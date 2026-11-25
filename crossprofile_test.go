package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func countChats(t *testing.T, dir string) int {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "local_*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return len(paths)
}

// syncTwoWay syncs official/a/o and third/p/q into each other.
func syncTwoWay(t *testing.T, official, third, trash, state string) {
	t.Helper()
	if _, err := syncProfileChats(third, "p", "q", official, "", "a/o", trash, state); err != nil {
		t.Fatal(err)
	}
	if _, err := syncProfileChats(official, "a", "o", third, "", "p/q", trash, state); err != nil {
		t.Fatal(err)
	}
}

func TestSyncProfileChatsSmallProfileDeletesNothing(t *testing.T) {
	official, third, trash := t.TempDir(), t.TempDir(), t.TempDir()
	state := filepath.Join(t.TempDir(), "chat-state-3p.json")
	for i := range 200 {
		for _, acct := range []string{"a", "b"} {
			write(t, filepath.Join(official, acct, "o", fmt.Sprintf("local_%d.json", i)),
				fmt.Sprintf(`{"sessionId":"local_%d","cliSessionId":"c%d","title":"chat %d","lastActivityAt":%d}`, i, i, i, 100+i))
		}
	}
	write(t, filepath.Join(official, "a", "o", "deleted_old"), "50")
	for i := range 3 {
		write(t, filepath.Join(third, "p", "q", fmt.Sprintf("local_3p%d.json", i)),
			fmt.Sprintf(`{"sessionId":"local_3p%d","cliSessionId":"t%d","lastActivityAt":%d}`, i, i, 1000+i))
	}
	// The third-party profile's other organization holds tasks only.
	write(t, filepath.Join(third, "p", "tasks-only", "scheduled-tasks.json"), `{"scheduledTasks":[]}`)

	for range 2 {
		syncTwoWay(t, official, third, trash, state)
	}
	if n := countChats(t, filepath.Join(third, "p", "q")); n != 203 {
		t.Fatalf("third-party profile holds %d chats, want 203", n)
	}
	if n := countChats(t, filepath.Join(official, "a", "o")); n != 203 {
		t.Fatalf("account a holds %d chats, want 203", n)
	}
	if n := countChats(t, filepath.Join(official, "b", "o")); n != 200 {
		t.Fatalf("account b holds %d chats, want its 200 untouched", n)
	}
	if n := countChats(t, filepath.Join(third, "p", "tasks-only")); n != 0 {
		t.Fatal("chats went into an organization the app keeps no chats in")
	}
	if trashed, _ := filepath.Glob(filepath.Join(trash, "*", "*")); len(trashed) != 0 {
		t.Fatalf("%d chats removed, want none", len(trashed))
	}
	if n, err := syncProfileChats(third, "p", "q", official, "", "a/o", trash, state); err != nil || n != 0 {
		t.Fatalf("idle sync = %d, %v; want nothing written", n, err)
	}
}

func TestSyncProfileChatsCarriesChangesAcross(t *testing.T) {
	official, third, trash := t.TempDir(), t.TempDir(), t.TempDir()
	state := filepath.Join(t.TempDir(), "chat-state-3p.json")
	mine := filepath.Join(official, "a", "o", "local_x.json")
	theirs := filepath.Join(third, "p", "q", "local_x.json")
	write(t, mine, `{"sessionId":"local_x","cliSessionId":"c1","title":"old","lastActivityAt":100}`)
	write(t, filepath.Join(official, "a", "o", "local_gone.json"), `{"sessionId":"local_gone","cliSessionId":"g","lastActivityAt":100}`)
	write(t, filepath.Join(third, "p", "q", "local_y.json"), `{"sessionId":"local_y","cliSessionId":"c2","lastActivityAt":100}`)
	syncTwoWay(t, official, third, trash, state)

	// Worked on and renamed on third-party, another chat deleted there.
	write(t, theirs, `{"sessionId":"local_x","cliSessionId":"c1","title":"new","lastActivityAt":200}`)
	if err := os.Remove(filepath.Join(third, "p", "q", "local_gone.json")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(third, "p", "q", "deleted_gone"), "300")
	syncTwoWay(t, official, third, trash, state)
	if got := chatField(t, mine, "lastActivityAt"); got != "200" {
		t.Fatalf("account copy lastActivityAt = %s, want the newer 200", got)
	}
	if got := chatField(t, mine, "title"); got != `"new"` {
		t.Fatalf("account copy title = %s, want the new one", got)
	}
	if _, err := os.Stat(filepath.Join(official, "a", "o", "local_gone.json")); !os.IsNotExist(err) {
		t.Fatal("a chat deleted on third-party stayed in the account")
	}

	// Renamed in the account, with no new activity: the third-party copy follows.
	write(t, mine, `{"sessionId":"local_x","cliSessionId":"c1","title":"mine","lastActivityAt":200}`)
	syncTwoWay(t, official, third, trash, state)
	if got := chatField(t, theirs, "title"); got != `"mine"` {
		t.Fatalf("third-party title = %s, want the account's", got)
	}
}

func TestSyncProfilesSkipsWhenNothingChanged(t *testing.T) {
	official, third := desktop{dir: t.TempDir()}, desktop{dir: t.TempDir()}
	write(t, official.configPath(), `{"lastKnownAccountUuid":"a","oauth:tokenCacheV2":"x"}`)
	mine := filepath.Join(official.sessionsDir(), "a", "o", "local_x.json")
	theirs := filepath.Join(third.sessionsDir(), "p", "q", "local_x.json")
	write(t, mine, `{"sessionId":"local_x","cliSessionId":"c1","title":"old","lastActivityAt":100}`)
	write(t, filepath.Join(third.sessionsDir(), "p", "q", "local_y.json"), `{"sessionId":"local_y","cliSessionId":"c2","lastActivityAt":100}`)
	s := &switcher{dir: t.TempDir(), st: State{SyncThirdParty: true, Accounts: []*Account{{UUID: "a", Org: "o"}}}}
	s.syncProfiles(official, third)
	if countChats(t, filepath.Dir(theirs)) != 2 || countChats(t, filepath.Dir(mine)) != 2 {
		t.Fatal("the profiles did not take each other's chats")
	}
	s.syncProfiles(official, third)
	if s.profilesSeen.third.n != 2 {
		t.Fatal("a sync that wrote nothing did not note the profiles as in step")
	}

	// A change the chat lists do not show is not looked for.
	info, err := os.Stat(theirs)
	if err != nil {
		t.Fatal(err)
	}
	write(t, theirs, `{"sessionId":"local_x","cliSessionId":"c1","title":"new","lastActivityAt":100}`)
	if err := os.Chtimes(theirs, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	s.syncProfiles(official, third)
	if got := chatField(t, mine, "title"); got != `"old"` {
		t.Fatalf("synced with no change in the chat lists, title = %s", got)
	}
	if err := os.Chtimes(theirs, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	s.syncProfiles(official, third)
	if got := chatField(t, mine, "title"); got != `"new"` {
		t.Fatalf("title = %s after a change, want the new one", got)
	}
}

func TestProfileInUse(t *testing.T) {
	dir := t.TempDir()
	if profileInUse(dir) {
		t.Fatal("a profile without a lock counts as in use")
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("host-123", filepath.Join(dir, "SingletonLock")); err != nil {
			t.Fatal(err)
		}
		if !profileInUse(dir) {
			t.Fatal("a profile with a SingletonLock counts as free")
		}
		return
	}
	lock := filepath.Join(dir, "lockfile")
	write(t, lock, "")
	if profileInUse(dir) {
		t.Fatal("a lockfile nobody holds counts as in use")
	}
	f, err := os.Open(lock)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !profileInUse(dir) {
		t.Fatal("a lockfile held open counts as free")
	}
}

func TestSyncProfileChatsKeepsScratchChatsHome(t *testing.T) {
	officialDir, thirdDir, trash := t.TempDir(), t.TempDir(), t.TempDir()
	official, third := filepath.Join(officialDir, "claude-code-sessions"), filepath.Join(thirdDir, "claude-code-sessions")
	state := filepath.Join(t.TempDir(), "chat-state-3p.json")
	entry := func(id, cwd string) string {
		return fmt.Sprintf(`{"sessionId":"local_%s","cliSessionId":"c%s","cwd":%q,"lastActivityAt":100}`, id, id, cwd)
	}
	scratch := func(dir, name string) string {
		return filepath.Join(dir, "scratch-workspaces", "a", "o", name)
	}
	// Official: a scratch chat, also in account b, and a project chat.
	write(t, filepath.Join(official, "a", "o", "local_os.json"), entry("os", scratch(officialDir, "scratch-2026-01-01-aaaaaa")))
	write(t, filepath.Join(official, "b", "o", "local_os.json"), entry("os", scratch(officialDir, "scratch-2026-01-01-aaaaaa")))
	write(t, filepath.Join(official, "a", "o", "local_op.json"), entry("op", filepath.Join(t.TempDir(), "project")))
	// Third-party: its own scratch chat, a project chat and a copy of the
	// official scratch chat an earlier sync made.
	write(t, filepath.Join(third, "p", "q", "local_ts.json"), entry("ts", scratch(thirdDir, "scratch-2026-01-02-bbbbbb")))
	write(t, filepath.Join(third, "p", "q", "local_tp.json"), entry("tp", filepath.Join(t.TempDir(), "other")))
	copied := filepath.Join(third, "p", "q", "local_os.json")
	write(t, copied, entry("os", scratch(officialDir, "scratch-2026-01-01-aaaaaa")))
	// A scratch folder elsewhere, or a name that only starts the same, is no scratch chat.
	write(t, filepath.Join(official, "a", "o", "local_ox.json"), entry("ox", filepath.Join(officialDir, "scratch-workspaces-old", "x")))

	sync := func() {
		t.Helper()
		if _, err := syncProfileChats(third, "p", "q", official, officialDir, "a/o", trash, state); err != nil {
			t.Fatal(err)
		}
		if _, err := syncProfileChats(official, "a", "o", third, thirdDir, "p/q", trash, state); err != nil {
			t.Fatal(err)
		}
	}
	sync()
	if _, err := os.Stat(copied); !os.IsNotExist(err) {
		t.Fatal("the copy of an official scratch chat stayed on third-party")
	}
	if trashed, _ := filepath.Glob(filepath.Join(trash, "*", "*")); len(trashed) != 1 {
		t.Fatalf("%d chats in trash, want the one copy", len(trashed))
	}
	for _, name := range []string{"local_op.json", "local_tp.json", "local_ox.json"} {
		for _, dir := range []string{filepath.Join(official, "a", "o"), filepath.Join(third, "p", "q")} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Fatalf("project chat %s missing from %s", name, dir)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(official, "a", "o", "local_ts.json")); !os.IsNotExist(err) {
		t.Fatal("a third-party scratch chat went to the official profile")
	}
	// Later rounds read the copy's absence as no deletion.
	for range 2 {
		sync()
	}
	for _, dir := range []string{filepath.Join(official, "a", "o"), filepath.Join(official, "b", "o")} {
		if _, err := os.Stat(filepath.Join(dir, "local_os.json")); err != nil {
			t.Fatalf("official scratch chat gone from %s", dir)
		}
	}
	if n := countChats(t, filepath.Join(official, "a", "o")); n != 4 {
		t.Fatalf("account a holds %d chats, want 4", n)
	}
	if n := countChats(t, filepath.Join(third, "p", "q")); n != 4 {
		t.Fatalf("third-party profile holds %d chats, want 4", n)
	}
	if trashed, _ := filepath.Glob(filepath.Join(trash, "*", "*")); len(trashed) != 1 {
		t.Fatalf("%d chats in trash after more rounds, want 1", len(trashed))
	}
}

func TestScratchOf(t *testing.T) {
	base := filepath.FromSlash("/data")
	if runtime.GOOS == "windows" {
		base = `C:\data`
	}
	official, third := filepath.Join(base, "Claude"), filepath.Join(base, "Claude-3p")
	cases := map[string]string{
		filepath.Join(official, "scratch-workspaces", "a", "o", "scratch-x"): official,
		filepath.Join(third, "scratch-workspaces", "a", "o", "scratch-x"):    third,
		filepath.Join(official, "scratch-workspaces"):                        "",
		filepath.Join(official, "scratch-workspacesX", "a"):                  "",
		filepath.Join(base, "work", "project"):                               "",
		"":                                                                   "",
	}
	if runtime.GOOS == "windows" {
		cases[strings.ToLower(filepath.Join(official, "Scratch-Workspaces", "a", "o", "scratch-x"))] = official
	}
	for cwd, want := range cases {
		if got := scratchOf(cwd, official, third); got != want {
			t.Errorf("scratchOf(%q) = %q, want %q", cwd, got, want)
		}
	}
}

func TestSyncProfileChatsIgnoresChangedScratchCopy(t *testing.T) {
	officialDir, thirdDir, trash := t.TempDir(), t.TempDir(), t.TempDir()
	official, third := filepath.Join(officialDir, "claude-code-sessions"), filepath.Join(thirdDir, "claude-code-sessions")
	state := filepath.Join(t.TempDir(), "chat-state-3p.json")
	cwd := filepath.Join(officialDir, "scratch-workspaces", "a", "o", "scratch-2026-01-01-aaaaaa")
	entry := func(archived bool) string {
		return fmt.Sprintf(`{"sessionId":"local_os","cliSessionId":"cos","cwd":%q,"lastActivityAt":100,"isArchived":%v}`, cwd, archived)
	}
	orig, copied := filepath.Join(official, "a", "o", "local_os.json"), filepath.Join(third, "p", "q", "local_os.json")
	// The copy an earlier sync left on third-party, recorded alike on both sides.
	write(t, orig, entry(false))
	write(t, copied, entry(false))
	if _, err := syncProfileChats(official, "a", "o", third, thirdDir, "p/q", trash, state); err != nil {
		t.Fatal(err)
	}
	// Archived on third-party before a sync there could take it away.
	write(t, copied, entry(true))
	if _, err := syncProfileChats(official, "a", "o", third, thirdDir, "p/q", trash, state); err != nil {
		t.Fatal(err)
	}
	var got struct{ IsArchived bool }
	if err := json.Unmarshal([]byte(read(t, orig)), &got); err != nil || got.IsArchived {
		t.Fatalf("archiving the third-party copy archived the official scratch chat (%v)", err)
	}
}
