package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The app lists Claude Code chats from claude-code-sessions/<account>/<org>/,
// one local_<id>.json per chat. The conversations themselves live in
// ~/.claude/projects and are shared, so making a chat visible in another
// account only needs its entry file. A deleted_<id> file marks a chat the
// user deleted; such chats are never copied back.

// syncSessions copies chat entries from every other account folder into the
// target account's folder. It adds missing chats and refreshes older copies;
// when two accounts carry the same local ID for diverged conversations, the
// more recently active one wins. A chat deleted in any account is removed
// from the target too, unless the target used it after the deletion; its
// entry is moved into trash (removed outright when trash is ""). Local IDs
// differ between accounts; cliSessionId identifies the actual conversation.
// The account folders under each root in more are sources as well, such as
// those of the app's other profile. Returns how many entries it wrote or
// removed.
func syncSessions(root, account, org, trash string, more ...string) (int, error) {
	return syncSessionsExcept(root, account, org, trash, nil, more...)
}

// syncSessionsExcept is syncSessions passing over, as if they were not
// there, the entries whose folder skip names; skip may be nil.
func syncSessionsExcept(root, account, org, trash string, skip func(cwd string) bool, more ...string) (int, error) {
	target := filepath.Join(root, account, org)
	dirs, err := globAll(filepath.Join("*", "*"), append([]string{root}, more...)...)
	if err != nil {
		return 0, err
	}

	// deleted maps a deleted local ID to its marker, which holds the time of
	// the deletion in milliseconds.
	deleted := map[string]deletion{}
	var all, sources []sessionIndex
	targets := map[string]sessionIndex{}
	protectedTargets := map[string]bool{}
	byIdentity := map[string]string{}
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err == nil && !info.IsDir() {
			continue // .DS_Store and the like
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			log.Printf("Skip session index folder %s: %v", dir, err)
			if samePath(dir, target) {
				// Without the target's indexes, importing could overwrite its entries.
				return 0, nil
			}
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			switch {
			case strings.HasPrefix(name, "deleted_"):
				id := strings.TrimPrefix(name, "deleted_")
				if d := readDeletion(filepath.Join(dir, name)); d.at >= deleted[id].at {
					deleted[id] = d
				}
			case strings.HasPrefix(name, "local_") && strings.HasSuffix(name, ".json"):
				entry, err := readSessionIndex(filepath.Join(dir, name))
				if err != nil {
					log.Printf("Skip session index %s: %v", filepath.Join(dir, name), err)
					if samePath(dir, target) {
						protectedTargets[name] = true
					}
					continue
				}
				if skip != nil && skip(entry.cwd) {
					continue
				}
				all = append(all, entry)
				if samePath(dir, target) {
					targets[name] = entry
					byIdentity[entry.identity()] = name
				} else {
					sources = append(sources, entry)
				}
			}
		}
	}
	// A deletion marker may name another local alias of the same conversation.
	deletedCLI := map[string]deletion{}
	for _, entry := range all {
		if d, ok := deleted[entry.localID()]; ok && entry.cli != "" && d.at >= deletedCLI[entry.cli].at {
			deletedCLI[entry.cli] = d
		}
	}
	// The app also names the deleted chat's conversations in markers of their own.
	gone := func(e sessionIndex) (deletion, bool) {
		if d, ok := deleted[e.localID()]; ok {
			return d, true
		}
		if e.cli == "" {
			return deletion{}, false
		}
		if d, ok := deleted[e.cli]; ok {
			return d, true
		}
		d, ok := deletedCLI[e.cli]
		return d, ok
	}

	if err := os.MkdirAll(target, 0o700); err != nil {
		return 0, err
	}
	written := 0
	// Deletions reach the target. A chat the target used after the deletion
	// stays, it holds work the deleting account never saw.
	for name, t := range targets {
		d, ok := gone(t)
		if !ok || t.activity > d.at {
			continue
		}
		if err := removeSession(t.path, trash); err != nil {
			return written, err
		}
		marker := filepath.Join(target, "deleted_"+t.localID())
		if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(marker, []byte(strconv.FormatInt(d.at, 10)), 0o600); err != nil {
				return written, err
			}
		}
		log.Printf("Removed deleted chat %s from %s", name, target)
		delete(targets, name)
		written++
	}
	// A new account imports the freshest copy once, even with many source aliases.
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].activity > sources[j].activity })

	// One entry that cannot be written does not stop the others; the first
	// error is returned once all are done.
	var firstErr error
	fail := func(err error) {
		log.Printf("sync: %v", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, s := range sources {
		if _, ok := gone(s); ok {
			continue
		}
		if protectedTargets[s.name] {
			log.Printf("Skip session index %s: target index could not be read", s.path)
			continue
		}
		if name, exists := byIdentity[s.identity()]; exists && name != s.name {
			// Do not replace account-local settings with another account's alias.
			continue
		}
		dst := filepath.Join(target, s.name)
		old, exists := targets[s.name]
		if exists && s.activity <= old.activity {
			continue
		}
		if exists && old.cli != "" && s.cli != "" && old.cli != s.cli {
			// The chat went on in two accounts under one local ID; the entry
			// follows the newer conversation. When the older one is not just
			// its start, it holds work of its own and stays as a branch, unless
			// the app lists it as a prior conversation (a resume or compaction
			// need not repeat the old answers).
			switch {
			case slices.Contains(s.prior, old.cli) || continues(s.cli, old.cli):
				log.Printf("Chat %s went on in another account, taking its newer conversation from %s", s.name, s.path)
			case hasOther(targets, old.cli, s.name):
				// A branch kept by an earlier sync already shows it.
			default:
				branch, err := keepBranch(old)
				if err != nil {
					fail(err)
					continue
				}
				log.Printf("Chat %s forked between accounts, kept the older branch as %s", s.name, branch)
				byIdentity[old.identity()] = branch
				targets[branch] = sessionIndex{path: filepath.Join(target, branch), name: branch, cli: old.cli, activity: old.activity}
				written++
			}
		}
		// The sidebar settings of a chat the target has are mergeChatState's
		// to decide; the source's may just be older ones.
		var keep *object
		if exists && old.identity() == s.identity() {
			if o, err := readObject(old.path); err == nil {
				keep = o
			}
		}
		if err := copySession(s.path, dst, keep); err != nil {
			fail(err)
			continue
		}
		written++
		targets[s.name] = s
		byIdentity[s.identity()] = s.name
	}
	return written, firstErr
}

// hasOther says whether targets holds conversation cli under a name other than name.
func hasOther(targets map[string]sessionIndex, cli, name string) bool {
	for n, t := range targets {
		if n != name && t.cli == cli {
			return true
		}
	}
	return false
}

// copySession copies a chat entry into another account. A quit mark left on
// the copy would continue the chat a second time there; takeCutChats moves
// the marks of the current switch to the target itself. A byte order mark
// is dropped, the app cannot read an entry that has one. With keep, the
// copy holds keep's sidebar settings instead of the source's.
func copySession(src, dst string, keep *object) error {
	o, err := readObject(src)
	if err != nil {
		return copyFile(src, dst)
	}
	head := make([]byte, 3)
	if f, err := os.Open(src); err == nil {
		_, _ = io.ReadFull(f, head)
		f.Close()
	}
	rewrite := bytes.Equal(head, []byte{0xef, 0xbb, 0xbf})
	if keep != nil {
		for _, k := range chatStateFields() {
			if v, ok := keep.get(k); ok {
				o.set(k, v)
			} else {
				o.del(k)
			}
		}
		rewrite = true
	}
	for _, k := range cutMarkKeys {
		if _, ok := o.get(k); ok {
			o.del(k)
			rewrite = true
		}
	}
	if !rewrite {
		return copyFile(src, dst)
	}
	return o.write(dst)
}

// transcripts is where continues looks for conversations; tests replace it.
var transcripts = projectsDir

// continues says whether conversation newer picks up where older ends: the
// app starts a new conversation file when it resumes a chat, carrying the
// recent messages over. It reads older's last answer and looks for it in
// newer; prompts left unanswered after it, often sent again in the other
// account, hold no work. Without both files it cannot tell, and says no.
func continues(newer, older string) bool {
	olderPath, newerPath := transcriptPath(older), transcriptPath(newer)
	if olderPath == "" || newerPath == "" {
		return false
	}
	// Transcripts reach a hundred MB; the last answer sits near the end.
	last := lastAnswer(readTail(olderPath, 4<<20))
	if last == "" {
		data, _ := os.ReadFile(olderPath)
		last = lastAnswer(data)
	}
	return last != "" && fileContains(newerPath, []byte(`"uuid":"`+last+`"`))
}

// transcriptPath finds conversation cli in any project folder, "" if none.
func transcriptPath(cli string) string {
	dirs, _ := os.ReadDir(transcripts())
	for _, d := range dirs {
		p := filepath.Join(transcripts(), d.Name(), cli+".jsonl")
		if d.IsDir() {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// fileContains streams path looking for needle, without holding it all.
func fileContains(path string, needle []byte) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 1<<20+len(needle))
	keep := 0
	for {
		n, err := f.Read(buf[keep:])
		total := keep + n
		if bytes.Contains(buf[:total], needle) {
			return true
		}
		if err != nil {
			return false
		}
		// Carry the tail over, in case needle spans two reads.
		keep = min(len(needle)-1, total)
		copy(buf, buf[total-keep:total])
	}
}

// lastAnswer is the uuid of a transcript's last message from Claude.
func lastAnswer(transcript []byte) string {
	lines := bytes.Split(transcript, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var e struct {
			Type        string `json:"type"`
			UUID        string `json:"uuid"`
			IsSidechain bool   `json:"isSidechain"`
		}
		if json.Unmarshal(lines[i], &e) != nil || e.IsSidechain || e.UUID == "" {
			continue
		}
		if e.Type == "assistant" {
			return e.UUID
		}
	}
	return ""
}

// keepBranch saves a chat entry under a new local ID, titled as a branch,
// next to the original. Returns the new file name.
func keepBranch(s sessionIndex) (string, error) {
	o, err := readObject(s.path)
	if err != nil {
		return "", err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}
	id[6], id[8] = id[6]&0x0f|0x40, id[8]&0x3f|0x80
	h := hex.EncodeToString(id)
	local := "local_" + h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	v, _ := json.Marshal(local)
	o.set("sessionId", v)
	title := stringField(o, "title")
	if title == "" {
		title = "Chat"
	}
	v, _ = json.Marshal(title + " " + tr.Branch)
	o.set("title", v)
	name := local + ".json"
	return name, o.write(filepath.Join(filepath.Dir(s.path), name))
}

type deletion struct {
	at int64 // milliseconds; 0 when the marker holds no time
}

// readDeletion reads the time the app wrote into a deleted_<id> marker. An
// unreadable marker still counts as a deletion, from the dawn of time, so it
// only removes chats nobody used since.
func readDeletion(path string) deletion {
	data, err := os.ReadFile(path)
	if err != nil {
		return deletion{}
	}
	at, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return deletion{at: at}
}

// removeSession moves a chat entry into trash, or deletes it when trash is "".
func removeSession(path, trash string) error {
	if trash == "" {
		return os.Remove(path)
	}
	dir := filepath.Join(trash, time.Now().Format("20060102"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := filepath.Base(filepath.Dir(filepath.Dir(path))) + "_" + filepath.Base(path)
	dst := filepath.Join(dir, name)
	// An entry of the same name moved there earlier that day stays.
	ext := filepath.Ext(name)
	for i := 2; ; i++ {
		if _, err := os.Lstat(dst); err != nil {
			break
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s_%d%s", strings.TrimSuffix(name, ext), i, ext))
	}
	if err := copyFile(path, dst); err != nil {
		return err
	}
	return os.Remove(path)
}

type sessionIndex struct {
	path, name, cli string
	cwd             string // the chat's folder
	activity        int64
	// The conversations the app ran this chat under before cli.
	prior []string
}

func readSessionIndex(path string) (sessionIndex, error) {
	data, err := readJSONFile(path)
	if err != nil {
		return sessionIndex{}, err
	}
	var entry *struct {
		CLISessionID   string   `json:"cliSessionId"`
		Cwd            string   `json:"cwd"`
		LastActivityAt int64    `json:"lastActivityAt"`
		Prior          []string `json:"priorCliSessionIds"`
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return sessionIndex{}, fmt.Errorf("read session index %s: %w", path, err)
	}
	if entry == nil {
		return sessionIndex{}, fmt.Errorf("session index %s is not an object", path)
	}
	return sessionIndex{path: path, name: filepath.Base(path), cli: entry.CLISessionID, cwd: entry.Cwd, activity: entry.LastActivityAt, prior: entry.Prior}, nil
}

func (s sessionIndex) identity() string {
	if s.cli != "" {
		return "cli:" + s.cli
	}
	// Legacy entries without a CLI ID remain distinct unless their filenames match.
	return "file:" + s.name
}

func (s sessionIndex) localID() string {
	return strings.TrimSuffix(strings.TrimPrefix(s.name, "local_"), ".json")
}

// samePath compares paths the way the file system does by default:
// ignoring case on Windows only. A Mac volume can be case-sensitive.
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// When the app quits it marks the chats it stopped mid-turn, and on its next
// start it sends them a message to go on. These are the keys of that mark.
var cutMarkKeys = []string{"interruptedByQuitAt", "interruptedUnseenResume", "interruptedAwaitingPerson"}

// cutChat is a chat whose turn the app stopped when it quit.
type cutChat struct {
	path, title, folder string
}

// takeCutChats marks the chats stopped mid-turn by the quit at since on
// their entry in the account's folder, where the app looks for the mark on
// its next start. Those are the chats the app marked itself and the running
// ones from runningChats: the app only marks when it quits through its own
// cleanup, which a quit from outside does not always reach. Marks left on
// other accounts' copies would continue the chat a second time after a later
// switch, so they are removed. Chats the account cannot see keep their mark.
func takeCutChats(root, account, org string, since time.Time, running map[string]bool) []cutChat {
	target := filepath.Join(root, account, org)
	paths, _ := filepath.Glob(filepath.Join(root, "*", "*", "local_*.json"))
	type entry struct {
		path string
		obj  *object
	}
	marked := map[string][]entry{}
	inTarget := map[string]entry{}
	for _, p := range paths {
		o, err := readObject(p)
		if err != nil {
			continue
		}
		cli := stringField(o, "cliSessionId")
		if cli == "" {
			continue
		}
		if samePath(filepath.Dir(p), target) {
			inTarget[cli] = entry{p, o}
		}
		var at int64
		if raw, ok := o.get("interruptedByQuitAt"); ok && json.Unmarshal(raw, &at) == nil && at >= since.UnixMilli() {
			marked[cli] = append(marked[cli], entry{p, o})
		}
	}
	for cli := range running {
		if _, ok := marked[cli]; !ok {
			marked[cli] = nil
		}
	}
	var cut []cutChat
	for cli, sources := range marked {
		dst, ok := inTarget[cli]
		if !ok {
			continue
		}
		switch {
		case len(sources) == 0:
			at, _ := json.Marshal(since.UnixMilli())
			dst.obj.set("interruptedByQuitAt", at)
			dst.obj.set("interruptedAwaitingPerson", json.RawMessage("false"))
		case !samePath(sources[0].path, dst.path):
			for _, k := range cutMarkKeys {
				if v, ok := sources[0].obj.get(k); ok {
					dst.obj.set(k, v)
				}
			}
		}
		if len(sources) == 0 || !samePath(sources[0].path, dst.path) {
			if err := dst.obj.write(dst.path); err != nil {
				log.Printf("move quit mark to %s: %v", dst.path, err)
				continue
			}
		}
		for _, src := range sources {
			if !samePath(src.path, dst.path) {
				if err := clearCutMark(src.path); err != nil {
					log.Printf("clear quit mark of %s: %v", src.path, err)
				}
			}
		}
		folder := stringField(dst.obj, "originCwd")
		if folder == "" {
			folder = stringField(dst.obj, "cwd")
		}
		if folder != "" {
			folder = filepath.Base(folder)
		}
		title := stringField(dst.obj, "title")
		if title == "" {
			title = folder
		}
		cut = append(cut, cutChat{path: dst.path, title: title, folder: folder})
	}
	sort.Slice(cut, func(i, j int) bool { return cut[i].title < cut[j].title })
	return cut
}

// A transcript written to within this long can belong to a running chat. A
// chat that sat this long on an open turn is waiting for someone anyway.
const runningWithin = 30 * time.Minute

// runningChats returns the CLI session IDs of the chats whose turn is open,
// from the transcripts the CLI keeps in projectsDir/<folder>/<id>.jsonl.
func runningChats(projectsDir string, now time.Time) map[string]bool {
	running := map[string]bool{}
	paths, _ := filepath.Glob(filepath.Join(projectsDir, "*", "*.jsonl"))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || now.Sub(info.ModTime()) > runningWithin {
			continue
		}
		if turnOpen(readTail(p, 256<<10)) {
			running[strings.TrimSuffix(filepath.Base(p), ".jsonl")] = true
		}
	}
	return running
}

func readTail(path string, n int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > n {
		_, _ = f.Seek(-n, io.SeekEnd)
	}
	data, _ := io.ReadAll(f)
	return data
}

// turnOpen reads a transcript's last message: the turn is open while Claude
// still has to answer a prompt or a tool result, or has called a tool.
func turnOpen(tail []byte) bool {
	lines := bytes.Split(tail, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		var e struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			IsMeta      bool   `json:"isMeta"`
			Message     struct {
				StopReason *string         `json:"stop_reason"`
				Content    json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[i], &e) != nil || e.IsSidechain || e.IsMeta {
			continue
		}
		// A local command such as /model and its output wait for nobody.
		if e.Type == "user" && (bytes.HasPrefix(e.Message.Content, []byte(`"<command-name>`)) || bytes.HasPrefix(e.Message.Content, []byte(`"<local-command`))) {
			continue
		}
		switch e.Type {
		case "assistant":
			if e.Message.StopReason == nil {
				return true
			}
			switch *e.Message.StopReason {
			case "end_turn", "stop_sequence", "max_tokens", "refusal":
				return false
			}
			return true
		case "user":
			return !bytes.Contains(e.Message.Content, []byte("[Request interrupted by user"))
		}
	}
	return false
}

// clearCutMark removes a chat's quit mark, so the app leaves the chat alone.
func clearCutMark(path string) error {
	o, err := readObject(path)
	if err != nil {
		return err
	}
	for _, k := range cutMarkKeys {
		o.del(k)
	}
	return o.write(path)
}

func stringField(o *object, key string) string {
	var s string
	if raw, ok := o.get(key); ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

// globAll matches pattern under each of roots.
func globAll(pattern string, roots ...string) ([]string, error) {
	var all []string
	for _, r := range roots {
		hits, err := filepath.Glob(filepath.Join(r, pattern))
		if err != nil {
			return nil, err
		}
		all = append(all, hits...)
	}
	return all, nil
}

// chatList sums up the chat entries under a claude-code-sessions folder.
type chatList struct {
	newest string    // the most recently written entry, "" when there is none
	at     time.Time // when it was written
	n      int
}

// newestChat finds the chat entry written last under root. The app rewrites
// the entry of a running Claude Code chat as it goes, so this tells whether
// someone is at work in Claude right now.
func newestChat(root string) chatList {
	var c chatList
	paths, _ := filepath.Glob(filepath.Join(root, "*", "*", "local_*.json"))
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.ModTime().After(c.at) {
			c.newest, c.at = p, info.ModTime()
		}
	}
	c.n = len(paths)
	return c
}

// projectsDir is where the Claude Code CLI keeps its transcripts.
func projectsDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}
