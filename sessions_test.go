package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncSessionsKeepsAccountLocalIdentity(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "local_source.json"), `{"sessionId":"local_source","cliSessionId":"shared","lastActivityAt":300,"isStarred":false,"isArchived":false}`)
	target := filepath.Join(root, "b", "ob", "local_target.json")
	want := `{"sessionId":"local_target","cliSessionId":"shared","lastActivityAt":100,"isStarred":true,"isArchived":true,"spaceId":"own-project","futureSetting":42}`
	write(t, target, want)
	for range 2 {
		n, err := syncSessions(root, "b", "ob", "")
		if err != nil || n != 0 {
			t.Fatalf("sync = %d, %v; want no imported aliases", n, err)
		}
		if read(t, target) != want {
			t.Fatal("another account overwrote the target's identity or settings")
		}
		if _, err := os.Stat(filepath.Join(root, "b", "ob", "local_source.json")); !os.IsNotExist(err) {
			t.Fatal("same CLI session was imported under a second local ID")
		}
	}
}

func TestSyncSessionsNewAccountGetsOneEntryPerConversation(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "local_old.json"), `{"sessionId":"local_old","cliSessionId":"shared","title":"Same title","lastActivityAt":100}`)
	write(t, filepath.Join(root, "b", "ob", "local_new.json"), `{"sessionId":"local_new","cliSessionId":"shared","title":"Same title","lastActivityAt":300,"future":true}`)
	write(t, filepath.Join(root, "a", "oa", "local_other.json"), `{"sessionId":"local_other","cliSessionId":"different","title":"Same title","lastActivityAt":200}`)
	for i := range 2 {
		n, err := syncSessions(root, "c", "oc", "")
		if err != nil || (i == 0 && n != 2) || (i == 1 && n != 0) {
			t.Fatalf("sync %d = %d, %v", i, n, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "c", "oc"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("new account entries = %d, %v; want two distinct conversations", len(entries), err)
	}
	if got := read(t, filepath.Join(root, "c", "oc", "local_new.json")); got != read(t, filepath.Join(root, "b", "ob", "local_new.json")) {
		t.Fatal("new account did not get the newest complete source entry")
	}
}

func TestSyncSessionsDoesNotRestoreDeletedAlias(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "local_first.json"), `{"cliSessionId":"shared","lastActivityAt":100}`)
	write(t, filepath.Join(root, "b", "ob", "local_alias.json"), `{"cliSessionId":"shared","lastActivityAt":200}`)
	write(t, filepath.Join(root, "a", "oa", "deleted_first"), "1")
	if n, err := syncSessions(root, "c", "oc", ""); err != nil || n != 0 {
		t.Fatalf("deleted conversation restored through its alias: %d, %v", n, err)
	}
}

func TestSyncSessionsSkipsStrayFile(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", ".DS_Store"), "Finder metadata")
	source := filepath.Join(root, "a", "oa", "local_valid.json")
	write(t, source, `{"cliSessionId":"valid","lastActivityAt":100}`)
	for i := range 2 {
		n, err := syncSessions(root, "b", "ob", "")
		if err != nil || (i == 0 && n != 1) || (i == 1 && n != 0) {
			t.Fatalf("sync %d with a stray file = %d, %v", i, n, err)
		}
	}
	if got := read(t, filepath.Join(root, "b", "ob", "local_valid.json")); got != read(t, source) {
		t.Fatal("stray file prevented a valid conversation from being imported")
	}
}

func TestSyncSessionsSkipsMalformedSourceIndex(t *testing.T) {
	for _, data := range []string{"", "{broken", "null", "[]"} {
		t.Run(data, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "a", "oa", "local_broken.json")
			write(t, path, data)
			write(t, filepath.Join(root, "a", "oa", "local_valid.json"), `{"cliSessionId":"valid","lastActivityAt":100}`)
			for i := range 2 {
				n, err := syncSessions(root, "b", "ob", "")
				if err != nil || (i == 0 && n != 1) || (i == 1 && n != 0) {
					t.Fatalf("sync %d with a malformed source = %d, %v", i, n, err)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "b", "ob", "local_broken.json")); !os.IsNotExist(err) {
				t.Fatal("malformed source was copied")
			}
			if read(t, path) != data {
				t.Fatal("malformed source was modified")
			}
		})
	}
}

func TestSyncSessionsPreservesMalformedTargetIndex(t *testing.T) {
	for _, data := range []string{"", "{broken", "null", "[]"} {
		t.Run(data, func(t *testing.T) {
			root := t.TempDir()
			write(t, filepath.Join(root, "a", "oa", "local_broken.json"), `{"cliSessionId":"same-name","lastActivityAt":200}`)
			write(t, filepath.Join(root, "a", "oa", "local_valid.json"), `{"cliSessionId":"valid","lastActivityAt":100}`)
			target := filepath.Join(root, "b", "ob", "local_broken.json")
			write(t, target, data)
			for i := range 2 {
				n, err := syncSessions(root, "b", "ob", "")
				if err != nil || (i == 0 && n != 1) || (i == 1 && n != 0) {
					t.Fatalf("sync %d with a malformed target = %d, %v", i, n, err)
				}
				if read(t, target) != data {
					t.Fatal("a source overwrote the malformed target index")
				}
			}
		})
	}
}

// useTranscripts points continues at a temporary projects folder.
func useTranscripts(t *testing.T, files map[string]string) {
	dir := t.TempDir()
	for cli, data := range files {
		write(t, filepath.Join(dir, "proj", cli+".jsonl"), data)
	}
	old := transcripts
	transcripts = func() string { return dir }
	t.Cleanup(func() { transcripts = old })
}

func TestSyncSessionsContinuedChatTakesNewest(t *testing.T) {
	useTranscripts(t, map[string]string{
		// The prompt left unanswered here was sent again in the other account.
		"second": `{"type":"user","uuid":"m1"}` + "\n" + `{"type":"assistant","uuid":"m2"}` + "\n" + `{"type":"user","uuid":"typo"}` + "\n",
		"first":  `{"type":"user","uuid":"m1"}` + "\n" + `{"type":"assistant","uuid":"m2"}` + "\n" + `{"type":"user","uuid":"m3"}` + "\n",
	})
	root := t.TempDir()
	newer := `{"cliSessionId":"first","lastActivityAt":200}`
	write(t, filepath.Join(root, "a", "oa", "local_same.json"), newer)
	write(t, filepath.Join(root, "a", "oa", "local_other.json"), `{"cliSessionId":"other","lastActivityAt":300}`)
	target := filepath.Join(root, "b", "ob", "local_same.json")
	write(t, target, `{"cliSessionId":"second","lastActivityAt":100}`)
	for i := range 2 {
		n, err := syncSessions(root, "b", "ob", "")
		if err != nil || (i == 0 && n != 2) || (i == 1 && n != 0) {
			t.Fatalf("sync %d with a continued chat = %d, %v", i, n, err)
		}
	}
	if read(t, target) != newer {
		t.Fatal("continued chat kept the older conversation")
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "b", "ob")); len(entries) != 2 {
		t.Fatalf("target has %d entries, want no branch for a plain continuation", len(entries))
	}
}

func TestSyncSessionsForkedChatKeepsBranch(t *testing.T) {
	useTranscripts(t, map[string]string{
		"older": `{"type":"user","uuid":"m1"}` + "\n" + `{"type":"assistant","uuid":"only-here"}` + "\n",
		"newer": `{"type":"user","uuid":"m1"}` + "\n" + `{"type":"assistant","uuid":"m9"}` + "\n",
	})
	root := t.TempDir()
	newer := `{"sessionId":"local_same","cliSessionId":"newer","title":"Plan","lastActivityAt":200}`
	write(t, filepath.Join(root, "a", "oa", "local_same.json"), newer)
	target := filepath.Join(root, "b", "ob", "local_same.json")
	write(t, target, `{"sessionId":"local_same","cliSessionId":"older","title":"Plan","lastActivityAt":100}`)
	if n, err := syncSessions(root, "b", "ob", ""); err != nil || n != 2 {
		t.Fatalf("sync with a forked chat = %d, %v; want the branch and the newer entry", n, err)
	}
	if read(t, target) != newer {
		t.Fatal("forked chat did not take the newer conversation")
	}
	branches, _ := filepath.Glob(filepath.Join(root, "b", "ob", "local_*.json"))
	var branch *object
	for _, p := range branches {
		if !samePath(p, target) {
			branch, _ = readObject(p)
			if id := stringField(branch, "sessionId"); id+".json" != filepath.Base(p) {
				t.Fatalf("branch sessionId %q does not match its file %s", id, filepath.Base(p))
			}
		}
	}
	if branch == nil || stringField(branch, "cliSessionId") != "older" || stringField(branch, "title") != "Plan (branch)" {
		t.Fatalf("older branch not kept: %d entries", len(branches))
	}
	// The branch reaches the other account, and both settle.
	if n, err := syncSessions(root, "a", "oa", ""); err != nil || n != 1 {
		t.Fatalf("branch import = %d, %v", n, err)
	}
	for _, acct := range []string{"a", "b"} {
		if n, err := syncSessions(root, acct, "o"+acct, ""); err != nil || n != 0 {
			t.Fatalf("resync %s = %d, %v; want settled", acct, n, err)
		}
	}
}

func TestSyncSessionsForkWithoutTranscriptsKeepsBranch(t *testing.T) {
	useTranscripts(t, nil)
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "local_same.json"), `{"cliSessionId":"newer","lastActivityAt":200}`)
	write(t, filepath.Join(root, "b", "ob", "local_same.json"), `{"cliSessionId":"older","lastActivityAt":100}`)
	if n, err := syncSessions(root, "b", "ob", ""); err != nil || n != 2 {
		t.Fatalf("sync = %d, %v; want a branch kept when the transcripts are missing", n, err)
	}
}

func TestSyncSessionsPriorConversationIsNoFork(t *testing.T) {
	useTranscripts(t, nil)
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "local_same.json"), `{"cliSessionId":"newer","priorCliSessionIds":["older"],"lastActivityAt":200}`)
	write(t, filepath.Join(root, "b", "ob", "local_same.json"), `{"cliSessionId":"older","lastActivityAt":100}`)
	if n, err := syncSessions(root, "b", "ob", ""); err != nil || n != 1 {
		t.Fatalf("sync = %d, %v; want the newer entry only, no branch", n, err)
	}
}

func TestSyncSessionsDivergedChatKeepsNewerTarget(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "local_same.json"), `{"cliSessionId":"first","lastActivityAt":100}`)
	target := filepath.Join(root, "b", "ob", "local_same.json")
	want := `{"cliSessionId":"second","lastActivityAt":200}`
	write(t, target, want)
	if n, err := syncSessions(root, "b", "ob", ""); err != nil || n != 0 {
		t.Fatalf("sync = %d, %v; want nothing written", n, err)
	}
	if read(t, target) != want {
		t.Fatal("an older conversation replaced the target's newer one")
	}
}

func TestSyncSessionsPropagatesDeletion(t *testing.T) {
	root := t.TempDir()
	trash := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "deleted_gone"), "500")
	target := filepath.Join(root, "b", "ob", "local_gone.json")
	write(t, target, `{"cliSessionId":"gone","lastActivityAt":400}`)
	alias := filepath.Join(root, "b", "ob", "local_alias.json")
	write(t, alias, `{"cliSessionId":"gone","lastActivityAt":450}`)
	kept := filepath.Join(root, "b", "ob", "local_kept.json")
	write(t, kept, `{"cliSessionId":"kept","lastActivityAt":100}`)
	for i := range 2 {
		n, err := syncSessions(root, "b", "ob", trash)
		if err != nil || (i == 0 && n != 2) || (i == 1 && n != 0) {
			t.Fatalf("sync %d with a deletion = %d, %v", i, n, err)
		}
	}
	for _, p := range []string{target, alias} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s survived its deletion in another account", filepath.Base(p))
		}
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatal("an unrelated chat was removed")
	}
	if got := read(t, filepath.Join(root, "b", "ob", "deleted_gone")); got != "500" {
		t.Fatalf("target marker = %q, want the deletion time", got)
	}
	backups, _ := filepath.Glob(filepath.Join(trash, "*", "b_local_*.json"))
	if len(backups) != 2 {
		t.Fatalf("trash holds %d entries, want both removed ones", len(backups))
	}
}

func TestKeepBranchTitleFollowsLanguage(t *testing.T) {
	defer func(old *texts) { tr = old }(tr)
	path := filepath.Join(t.TempDir(), "local_x.json")
	write(t, path, `{"sessionId":"local_x","cliSessionId":"c","title":"Plan"}`)
	for _, c := range []struct {
		lang *texts
		want string
	}{{&english, "Plan (branch)"}, {&chinese, "Plan (分支)"}} {
		tr = c.lang
		name, err := keepBranch(sessionIndex{path: path})
		if err != nil {
			t.Fatal(err)
		}
		o, err := readObject(filepath.Join(filepath.Dir(path), name))
		if err != nil {
			t.Fatal(err)
		}
		if got := stringField(o, "title"); got != c.want {
			t.Errorf("branch title = %q, want %q", got, c.want)
		}
	}
}

func TestRemoveSessionKeepsSameNameInTrash(t *testing.T) {
	root, trash := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "b", "ob", "local_x.json")
	for _, body := range []string{`{"n":1}`, `{"n":2}`, `{"n":3}`} {
		write(t, path, body)
		if err := removeSession(path, trash); err != nil {
			t.Fatal(err)
		}
	}
	moved, _ := filepath.Glob(filepath.Join(trash, "*", "b_local_x*.json"))
	if len(moved) != 3 {
		t.Fatalf("trash holds %d copies, want all three", len(moved))
	}
	seen := map[string]bool{}
	for _, p := range moved {
		seen[read(t, p)] = true
	}
	if len(seen) != 3 {
		t.Fatalf("a removed entry overwrote another in the trash: %v", seen)
	}
}

func TestSyncSessionsKeepsChatUsedAfterDeletion(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "deleted_busy"), "500")
	target := filepath.Join(root, "b", "ob", "local_busy.json")
	want := `{"cliSessionId":"busy","lastActivityAt":900}`
	write(t, target, want)
	if n, err := syncSessions(root, "b", "ob", ""); err != nil || n != 0 {
		t.Fatalf("sync = %d, %v; want nothing removed", n, err)
	}
	if read(t, target) != want {
		t.Fatal("a chat used after the deletion was removed")
	}
}

func TestSyncSessionsReportsWriteFailure(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "local_valid.json"), `{"cliSessionId":"valid","lastActivityAt":100}`)
	if err := os.MkdirAll(filepath.Join(root, "b", "ob", "local_valid.json.switcher-tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if n, err := syncSessions(root, "b", "ob", ""); err == nil || n != 0 {
		t.Fatalf("sync with a blocked destination = %d, %v; want a write error", n, err)
	}
	if _, err := os.Stat(filepath.Join(root, "b", "ob", "local_valid.json")); !os.IsNotExist(err) {
		t.Fatal("failed write created a target index")
	}
}

// Keep the identity assertion explicit: titles and local IDs are not conversation IDs.
func TestSessionIdentityWithoutCLI(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"one", "two"} {
		data, _ := json.Marshal(map[string]any{"sessionId": "local_" + id, "title": "Same title", "lastActivityAt": 1})
		write(t, filepath.Join(root, "a", "oa", "local_"+id+".json"), string(data))
	}
	if n, err := syncSessions(root, "b", "ob", ""); err != nil || n != 2 {
		t.Fatalf("legacy entries without CLI IDs were merged: %d, %v", n, err)
	}
}

// SESSION_INDEX_SAMPLE must name an offline copy of claude-code-sessions.
// Work only on a temporary copy; never modify the provided sample.
func TestSyncSessionsRealSample(t *testing.T) {
	sample := os.Getenv("SESSION_INDEX_SAMPLE")
	if sample == "" {
		t.Skip("SESSION_INDEX_SAMPLE not set")
	}
	root := filepath.Join(t.TempDir(), "sessions")
	if err := os.CopyFS(root, os.DirFS(sample)); err != nil {
		t.Fatal(err)
	}
	dirs, err := filepath.Glob(filepath.Join(root, "*", "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no sample account folders: %v", err)
	}
	before := map[string]bool{}
	originalIdentities := map[string]map[string]bool{}
	for _, dir := range dirs {
		originalIdentities[dir] = map[string]bool{}
		paths, _ := filepath.Glob(filepath.Join(dir, "local_*.json"))
		for _, path := range paths {
			before[path] = true
			entry, err := readSessionIndex(path)
			if err != nil {
				t.Fatal(err)
			}
			originalIdentities[dir][entry.identity()] = true
		}
	}
	for round := range 2 {
		for _, dir := range dirs {
			n, err := syncSessions(root, filepath.Base(filepath.Dir(dir)), filepath.Base(dir), "")
			if err != nil {
				t.Fatal(err)
			}
			if round == 1 && n != 0 {
				t.Fatal("second round trip was not idempotent")
			}
		}
	}
	for _, dir := range dirs {
		paths, _ := filepath.Glob(filepath.Join(dir, "local_*.json"))
		seen := map[string]bool{}
		for _, path := range paths {
			entry, err := readSessionIndex(path)
			if err != nil {
				t.Fatal(err)
			}
			if seen[entry.identity()] || (!before[path] && originalIdentities[dir][entry.identity()]) {
				t.Fatal("round trip introduced another local alias")
			}
			seen[entry.identity()] = true
			delete(before, path)
		}
	}
	if len(before) != 0 {
		t.Fatal("round trip removed a target account's entry")
	}
}

func TestTakeCutChatsMovesMarkToTargetAccount(t *testing.T) {
	root := t.TempDir()
	since := time.UnixMilli(1000)
	src := filepath.Join(root, "a", "oa", "local_one.json")
	write(t, src, `{"cliSessionId":"run","title":"Running","interruptedByQuitAt":1500,"interruptedAwaitingPerson":false,"isArchived":false}`)
	dst := filepath.Join(root, "b", "ob", "local_one.json")
	write(t, dst, `{"cliSessionId":"run","title":"Running","isArchived":false}`)
	write(t, filepath.Join(root, "a", "oa", "local_old.json"), `{"cliSessionId":"old","interruptedByQuitAt":500}`)
	write(t, filepath.Join(root, "b", "ob", "local_old.json"), `{"cliSessionId":"old"}`)
	write(t, filepath.Join(root, "a", "oa", "local_hidden.json"), `{"cliSessionId":"hidden","interruptedByQuitAt":1500}`)

	cut := takeCutChats(root, "b", "ob", since, nil)
	if len(cut) != 1 || cut[0].title != "Running" || !samePath(cut[0].path, dst) {
		t.Fatalf("cut = %+v, want only the chat stopped by this quit, at its target entry", cut)
	}
	o, err := readObject(dst)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := o.get("interruptedByQuitAt"); string(v) != "1500" {
		t.Fatalf("target mark = %s, want 1500", v)
	}
	if v, _ := o.get("interruptedAwaitingPerson"); string(v) != "false" {
		t.Fatalf("target awaiting = %s, want false", v)
	}
	if o, _ := readObject(src); o != nil {
		if _, ok := o.get("interruptedByQuitAt"); ok {
			t.Fatal("source kept its mark, a later switch back would continue the chat again")
		}
	}
	if o, _ := readObject(filepath.Join(root, "a", "oa", "local_hidden.json")); o != nil {
		if _, ok := o.get("interruptedByQuitAt"); !ok {
			t.Fatal("removed the mark of a chat the target account cannot see")
		}
	}
	if err := clearCutMark(dst); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); got != "{\n\t\"cliSessionId\": \"run\",\n\t\"title\": \"Running\",\n\t\"isArchived\": false\n}" {
		t.Fatalf("cleared entry = %q", got)
	}
}

func TestTakeCutChatsSameAccount(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a", "oa", "local_one.json")
	write(t, p, `{"cliSessionId":"run","cwd":"/proj/app","interruptedByQuitAt":1500}`)
	cut := takeCutChats(root, "a", "oa", time.UnixMilli(1000), nil)
	if len(cut) != 1 || cut[0].title != "app" {
		t.Fatalf("cut = %+v, want the chat named after its folder", cut)
	}
	if o, _ := readObject(p); o != nil {
		if _, ok := o.get("interruptedByQuitAt"); !ok {
			t.Fatal("restart in the same account removed the mark")
		}
	}
}

func TestSyncSessionsDropsQuitMarkOnCopy(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "local_cut.json"), `{"cliSessionId":"cut","lastActivityAt":200,"interruptedByQuitAt":150,"interruptedAwaitingPerson":false,"title":"T"}`)
	if n, err := syncSessions(root, "b", "ob", ""); err != nil || n != 1 {
		t.Fatalf("sync = %d, %v", n, err)
	}
	o, err := readObject(filepath.Join(root, "b", "ob", "local_cut.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range cutMarkKeys {
		if _, ok := o.get(k); ok {
			t.Fatalf("copy kept %s, the chat would be continued again in the other account", k)
		}
	}
	if stringField(o, "title") != "T" {
		t.Fatal("copy lost its other fields")
	}
}

func TestSyncSessionsForkKeepsOneBranch(t *testing.T) {
	useTranscripts(t, nil)
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "local_same.json"), `{"cliSessionId":"newer","lastActivityAt":200}`)
	write(t, filepath.Join(root, "b", "ob", "local_same.json"), `{"cliSessionId":"older","lastActivityAt":100}`)
	write(t, filepath.Join(root, "b", "ob", "local_kept.json"), `{"cliSessionId":"older","lastActivityAt":100}`)
	if n, err := syncSessions(root, "b", "ob", ""); err != nil || n != 1 {
		t.Fatalf("sync = %d, %v; want no second branch of a conversation already shown", n, err)
	}
}

func TestTurnOpenIgnoresLocalCommands(t *testing.T) {
	tail := []byte(`{"type":"assistant","message":{"stop_reason":"end_turn","content":[]}}` + "\n" +
		`{"type":"user","message":{"content":"<command-name>/model</command-name>"}}` + "\n" +
		`{"type":"user","message":{"content":"<local-command-stdout>Set model</local-command-stdout>"}}` + "\n" +
		`{"type":"user","isMeta":true,"message":{"content":"caveat"}}` + "\n")
	if turnOpen(tail) {
		t.Fatal("a finished chat followed by a local command counts as running")
	}
}

func TestFileContainsAcrossReads(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.jsonl")
	data := bytes.Repeat([]byte("x"), 1<<20-5)
	data = append(data, []byte(`"uuid":"needle"`)...)
	write(t, p, string(data))
	if !fileContains(p, []byte(`"uuid":"needle"`)) {
		t.Fatal("needle spanning two reads not found")
	}
	if fileContains(p, []byte(`"uuid":"other"`)) {
		t.Fatal("found a needle that is not there")
	}
}
