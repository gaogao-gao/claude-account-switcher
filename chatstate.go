package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// What the user sets on a chat in the sidebar, such as archiving it, does not
// touch its lastActivityAt, so syncSessions, which goes by activity, cannot
// tell which account holds the newer setting. mergeChatState works on these
// fields alone and remembers what it saw in each account: an account whose
// value moved since the last sync is where the user changed it, and that
// value goes to the target. Fields that move together form one group.
//
// The pinned list itself lives in the app's Local Storage, which all
// accounts share; isStarred mirrors it in the entry, where the app reads it
// to spare a chat from auto archive.
type stateGroup struct {
	fields []string
	// weight ranks values the first time a conversation is seen with
	// different values: what the user set beats what never synced over.
	weight func(o *object) int
}

var chatStateGroups = []stateGroup{
	{[]string{"isArchived"}, func(o *object) int {
		// The app marks a chat the user unarchived so auto archive spares it.
		if boolWeight(o, "isArchived") == 0 && boolWeight(o, "autoArchiveExempt") == 1 {
			return 2
		}
		return boolWeight(o, "isArchived")
	}},
	{[]string{"isStarred"}, func(o *object) int { return boolWeight(o, "isStarred") }},
	{[]string{"title", "titleSource", "previousTitles"}, func(o *object) int {
		// Each new title pushes the old one onto previousTitles.
		var prev []json.RawMessage
		if raw, ok := o.get("previousTitles"); ok {
			_ = json.Unmarshal(raw, &prev)
		}
		w := len(prev)
		if stringField(o, "titleSource") == "user" {
			w += 1 << 16
		}
		return w
	}},
}

func boolWeight(o *object, key string) int {
	var b bool
	if raw, ok := o.get(key); ok && json.Unmarshal(raw, &b) == nil && b {
		return 1
	}
	return 0
}

// chatState is kept between syncs in the switcher's folder.
type chatState struct {
	// Agreed is the value of each group per conversation after the last sync.
	Agreed map[string]map[string]string `json:"agreed"`
	// Seen is what each entry held then, by account/org/file name.
	Seen map[string]seenEntry `json:"seen"`
}

type seenEntry struct {
	ID     string            `json:"id"`
	Groups map[string]string `json:"groups"`
}

func readChatState(path string) chatState {
	var st chatState
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	if st.Agreed == nil {
		st.Agreed = map[string]map[string]string{}
	}
	if st.Seen == nil {
		st.Seen = map[string]seenEntry{}
	}
	return st
}

func writeChatState(path string, st chatState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// stateEntry is one chat entry as mergeChatState sees it.
type stateEntry struct {
	key  string // account/org
	name string // file name
	path string
	obj  *object
	mod  int64 // file time, to settle a change made in two accounts at once
	bom  bool  // the app skips an entry that starts with a byte order mark
}

// groupValue is a group's fields in one canonical string. A field set to
// false or null counts as absent, as the app treats both alike.
func groupValue(o *object, group []string) string {
	m := map[string]json.RawMessage{}
	for _, k := range group {
		v, ok := o.get(k)
		if !ok {
			continue
		}
		var c bytes.Buffer
		if json.Compact(&c, v) != nil {
			continue
		}
		if s := c.String(); s == "false" || s == "null" {
			continue
		}
		m[k] = c.Bytes()
	}
	data, _ := json.Marshal(m)
	return string(data)
}

// setGroupValue writes value into o's fields of group.
func setGroupValue(o *object, group []string, value string) {
	var m map[string]json.RawMessage
	_ = json.Unmarshal([]byte(value), &m)
	for _, k := range group {
		if v, ok := m[k]; ok {
			o.set(k, v)
			continue
		}
		cur, ok := o.get(k)
		if !ok {
			continue
		}
		if t := bytes.TrimSpace(cur); bytes.Equal(t, []byte("true")) {
			o.set(k, json.RawMessage("false"))
		} else if !bytes.Equal(t, []byte("false")) && !bytes.Equal(t, []byte("null")) {
			o.del(k)
		}
	}
}

// mergeChatState brings the sidebar settings of every chat the target
// account has up to date with the account where the user last changed them.
// It runs after syncSessions, which leaves these fields of an entry it
// refreshes as they were. prefer names the account folder (account/org)
// whose values count the first time a conversation is seen with different
// values: the account the user was just working in. The accounts under each
// root in more take part as well. Returns the entries written.
func mergeChatState(root, account, org, prefer, statePath string, more ...string) (int, error) {
	return mergeChatStateExcept(root, account, org, prefer, statePath, nil, more...)
}

// mergeChatStateExcept is mergeChatState passing over, as if they were not
// there, the entries whose folder skip names; skip may be nil.
func mergeChatStateExcept(root, account, org, prefer, statePath string, skip func(cwd string) bool, more ...string) (int, error) {
	target := account + "/" + org
	paths, err := globAll(filepath.Join("*", "*", "local_*.json"), append([]string{root}, more...)...)
	if err != nil {
		return 0, err
	}
	if len(paths) == 0 {
		// A data folder that moved or is not there yet; keep what we know.
		return 0, nil
	}
	byID := map[string][]stateEntry{}
	present := map[string]bool{}
	// Entries skip passes over keep their records, for a sync that does not skip them.
	passed := map[string]bool{}
	unreadable := false
	for _, p := range paths {
		dir := filepath.Dir(p)
		e := stateEntry{
			key:  filepath.Base(filepath.Dir(dir)) + "/" + filepath.Base(dir),
			name: filepath.Base(p),
			path: p,
		}
		present[e.seenKey()] = true
		o, err := readObject(p)
		if err == nil && skip != nil && skip(stringField(o, "cwd")) {
			passed[chatIdentity(o, e.name)] = true
			continue
		}
		if err != nil {
			unreadable = true
			continue
		}
		e.obj = o
		if info, err := os.Stat(p); err == nil {
			e.mod = info.ModTime().UnixNano()
		}
		if f, err := os.Open(p); err == nil {
			head := make([]byte, 3)
			_, _ = io.ReadFull(f, head)
			f.Close()
			e.bom = bytes.Equal(head, []byte{0xef, 0xbb, 0xbf})
		}
		id := chatIdentity(o, e.name)
		byID[id] = append(byID[id], e)
	}

	st := readChatState(statePath)
	written := 0
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		entries := byID[id]
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].mod > entries[j].mod })
		if st.Agreed[id] == nil {
			st.Agreed[id] = map[string]string{}
		}
		dirty := map[string]bool{}
		for _, e := range entries {
			if e.key == target && e.bom {
				dirty[e.path] = true
			}
		}
		for _, sg := range chatStateGroups {
			g := strings.Join(sg.fields, "+")
			truth, known := st.Agreed[id][g]
			// The newest entry that moved since the last sync carries the user's change.
			changed := false
			for _, e := range entries {
				was, ok := st.Seen[e.seenKey()]
				if !ok || was.ID != id {
					continue
				}
				if v, ok := was.Groups[g]; ok && groupValue(e.obj, sg.fields) != v {
					truth, known, changed = groupValue(e.obj, sg.fields), true, true
					break
				}
			}
			if !changed && !known {
				truth = firstSeenValue(entries, sg, prefer, target)
			}
			st.Agreed[id][g] = truth
			for _, e := range entries {
				if e.key == target && groupValue(e.obj, sg.fields) != truth {
					setGroupValue(e.obj, sg.fields, truth)
					dirty[e.path] = true
				}
			}
		}
		for i, e := range entries {
			if dirty[e.path] {
				if err := e.obj.write(e.path); err != nil {
					log.Printf("chat state of %s: %v", e.path, err)
					// Remember what the file still holds, not what it failed to get.
					if o, err := readObject(e.path); err == nil {
						entries[i].obj, e.obj = o, o
					}
				} else {
					written++
				}
			}
			groups := map[string]string{}
			for _, sg := range chatStateGroups {
				groups[strings.Join(sg.fields, "+")] = groupValue(e.obj, sg.fields)
			}
			st.Seen[e.seenKey()] = seenEntry{ID: id, Groups: groups}
		}
	}
	// Forget entries and conversations gone from every account. While some
	// entry cannot be read, the conversation it holds is unknown, so keep all.
	if !unreadable {
		for id := range st.Agreed {
			if _, ok := byID[id]; !ok && !passed[id] {
				delete(st.Agreed, id)
			}
		}
	}
	for k := range st.Seen {
		if !present[k] {
			delete(st.Seen, k)
		}
	}
	return written, writeChatState(statePath, st)
}

func (e stateEntry) seenKey() string { return e.key + "/" + e.name }

// firstSeenValue picks a group's value for a conversation the switcher has no
// record of. Copies differ then because they never synced, so the value the
// user set wins, such as archived over never archived; among equals the
// preferred account's, then the target's, then the newest entry's.
func firstSeenValue(entries []stateEntry, sg stateGroup, prefer, target string) string {
	order := make([]stateEntry, 0, 2*len(entries))
	for _, key := range []string{prefer, target} {
		for _, e := range entries {
			if e.key == key {
				order = append(order, e)
			}
		}
	}
	order = append(order, entries...)
	best := order[0]
	for _, e := range order[1:] {
		if sg.weight(e.obj) > sg.weight(best.obj) {
			best = e
		}
	}
	return groupValue(best.obj, sg.fields)
}

// chatStateFields are all fields mergeChatState decides.
func chatStateFields() []string {
	var fields []string
	for _, sg := range chatStateGroups {
		fields = append(fields, sg.fields...)
	}
	return fields
}

// chatIdentity is the conversation an entry shows, the same across accounts.
func chatIdentity(o *object, name string) string {
	if cli := stringField(o, "cliSessionId"); cli != "" {
		return "cli:" + cli
	}
	return "file:" + name
}
