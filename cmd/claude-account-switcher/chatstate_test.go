package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func chatField(t *testing.T, path, key string) string {
	t.Helper()
	o, err := readObject(path)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := o.get(key)
	return string(v)
}

func TestMergeChatStateFollowsTheAccountThatChanged(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "chat-state.json")
	a := filepath.Join(root, "a", "oa", "local_x.json")
	b := filepath.Join(root, "b", "ob", "local_x.json")
	write(t, a, `{"sessionId":"local_x","cliSessionId":"c1","lastActivityAt":100,"isArchived":false,"isStarred":false}`)
	write(t, b, `{"sessionId":"local_x","cliSessionId":"c1","lastActivityAt":100,"isArchived":false,"isStarred":false}`)
	sync := func(account, org, prefer string) {
		t.Helper()
		if _, err := mergeChatState(root, account, org, prefer, state); err != nil {
			t.Fatal(err)
		}
	}
	sync("b", "ob", "a/oa")

	// Archived in a, with no new activity: b follows on the next switch.
	write(t, a, `{"sessionId":"local_x","cliSessionId":"c1","lastActivityAt":100,"isArchived":true,"isStarred":false}`)
	sync("b", "ob", "a/oa")
	if got := chatField(t, b, "isArchived"); got != "true" {
		t.Fatalf("b isArchived = %s, want true", got)
	}

	// Unarchived and starred in b: a follows when the user switches back.
	write(t, b, `{"sessionId":"local_x","cliSessionId":"c1","lastActivityAt":100,"isArchived":false,"isStarred":true}`)
	sync("a", "oa", "b/ob")
	if got := chatField(t, a, "isArchived"); got != "false" {
		t.Fatalf("a isArchived = %s, want false", got)
	}
	if got := chatField(t, a, "isStarred"); got != "true" {
		t.Fatalf("a isStarred = %s, want true", got)
	}

	// Syncing again without a change, from either side, changes nothing.
	for _, prefer := range []string{"a/oa", "b/ob"} {
		if n, err := mergeChatState(root, "a", "oa", prefer, state); err != nil || n != 0 {
			t.Fatalf("idle sync = %d, %v", n, err)
		}
	}
}

func TestMergeChatStateFirstSyncTakesWhatTheUserSet(t *testing.T) {
	// The left account never got the archive made in the target; that is
	// no reason to unarchive it.
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "chat-state.json")
	a := filepath.Join(root, "a", "oa", "local_x.json")
	b := filepath.Join(root, "b", "ob", "local_x.json")
	write(t, a, `{"cliSessionId":"c1","isArchived":false,"title":"New","titleSource":"auto","previousTitles":["Old"]}`)
	write(t, b, `{"cliSessionId":"c1","isArchived":true,"title":"Old","titleSource":"auto"}`)
	if _, err := mergeChatState(root, "b", "ob", "a/oa", state); err != nil {
		t.Fatal(err)
	}
	if got := chatField(t, b, "isArchived"); got != "true" {
		t.Fatalf("isArchived = %s, want true", got)
	}
	if got := chatField(t, b, "title"); got != `"New"` {
		t.Fatalf("title = %s, want the later title", got)
	}
	if got := strings.Join(strings.Fields(chatField(t, b, "previousTitles")), ""); got != `["Old"]` {
		t.Fatalf("previousTitles = %s", got)
	}
}

func TestMergeChatStateStaleAccountDoesNotWin(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "chat-state.json")
	entry := `{"cliSessionId":"c1","lastActivityAt":100,"isArchived":false}`
	for _, acct := range []string{"a", "b", "c"} {
		write(t, filepath.Join(root, acct, "o"+acct, "local_x.json"), entry)
	}
	if _, err := mergeChatState(root, "a", "oa", "a/oa", state); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "a", "oa", "local_x.json"), strings.Replace(entry, "false", "true", 1))
	if _, err := mergeChatState(root, "b", "ob", "a/oa", state); err != nil {
		t.Fatal(err)
	}
	// c still says false, as it did; that is not a change and must not undo the archive.
	if _, err := mergeChatState(root, "a", "oa", "b/ob", state); err != nil {
		t.Fatal(err)
	}
	for _, acct := range []string{"a", "b"} {
		if got := chatField(t, filepath.Join(root, acct, "o"+acct, "local_x.json"), "isArchived"); got != "true" {
			t.Fatalf("%s isArchived = %s, want true", acct, got)
		}
	}
	if _, err := mergeChatState(root, "c", "oc", "a/oa", state); err != nil {
		t.Fatal(err)
	}
	if got := chatField(t, filepath.Join(root, "c", "oc", "local_x.json"), "isArchived"); got != "true" {
		t.Fatalf("c isArchived = %s, want true", got)
	}
}

func TestMergeChatStateMatchesAliasesByConversation(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "chat-state.json")
	write(t, filepath.Join(root, "a", "oa", "local_one.json"), `{"sessionId":"local_one","cliSessionId":"c1","title":"Renamed","titleSource":"user","isStarred":true}`)
	b := filepath.Join(root, "b", "ob", "local_two.json")
	write(t, b, `{"sessionId":"local_two","cliSessionId":"c1","title":"Auto","titleSource":"auto","isStarred":false,"own":1}`)
	if _, err := mergeChatState(root, "b", "ob", "a/oa", state); err != nil {
		t.Fatal(err)
	}
	want := `{"sessionId":"local_two","cliSessionId":"c1","title":"Renamed","titleSource":"user","isStarred":true,"own":1}`
	o, _ := readObject(b)
	if got := compactObject(t, o); got != want {
		t.Fatalf("alias = %s\nwant %s", got, want)
	}
}

func TestReadObjectSkipsByteOrderMark(t *testing.T) {
	p := filepath.Join(t.TempDir(), "local_x.json")
	write(t, p, "\xef\xbb\xbf"+`{"cliSessionId":"c1","lastActivityAt":5}`)
	if _, err := readObject(p); err != nil {
		t.Fatal(err)
	}
	if e, err := readSessionIndex(p); err != nil || e.cli != "c1" || e.activity != 5 {
		t.Fatalf("readSessionIndex = %+v, %v", e, err)
	}
}

func compactObject(t *testing.T, o *object) string {
	t.Helper()
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"` + k + `":`)
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.String()
}

func TestSyncSessionsHonorsConversationTombstone(t *testing.T) {
	// The app marks a deleted chat's conversation too; another account may
	// show that conversation under a local ID of its own.
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "oa", "deleted_c1"), "500")
	alias := filepath.Join(root, "b", "ob", "local_alias.json")
	write(t, alias, `{"cliSessionId":"c1","lastActivityAt":100}`)
	if _, err := syncSessions(root, "b", "ob", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(alias); !os.IsNotExist(err) {
		t.Fatal("chat deleted in another account stayed")
	}
}

func TestCopySessionDropsByteOrderMark(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src.json"), filepath.Join(dir, "dst.json")
	write(t, src, "\xef\xbb\xbf"+`{"cliSessionId":"c1"}`)
	if err := copySession(src, dst, nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dst); strings.HasPrefix(got, "\xef\xbb\xbf") || !strings.Contains(got, `"c1"`) {
		t.Fatalf("copy = %q", got)
	}
}

func TestMergeChatStateRewritesEntryWithByteOrderMark(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "chat-state.json")
	p := filepath.Join(root, "b", "ob", "local_x.json")
	write(t, p, "\xef\xbb\xbf"+`{"cliSessionId":"c1","isArchived":false}`)
	if n, err := mergeChatState(root, "b", "ob", "", state); err != nil || n != 1 {
		t.Fatalf("merge = %d, %v", n, err)
	}
	if got := read(t, p); strings.HasPrefix(got, "\xef\xbb\xbf") || !strings.Contains(got, `"c1"`) {
		t.Fatalf("entry = %q", got)
	}
}

func TestRefreshKeepsTargetSettingsForMerge(t *testing.T) {
	// A stale account with newer activity refreshes the entry; the archive
	// the user made since must survive it.
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "chat-state.json")
	stale := filepath.Join(root, "a", "oa", "local_x.json")
	left := filepath.Join(root, "b", "ob", "local_x.json")
	target := filepath.Join(root, "c", "oc", "local_x.json")
	write(t, stale, `{"cliSessionId":"c1","lastActivityAt":300,"isArchived":false}`)
	write(t, left, `{"cliSessionId":"c1","lastActivityAt":300,"isArchived":false}`)
	write(t, target, `{"cliSessionId":"c1","lastActivityAt":300,"isArchived":false}`)
	if _, err := mergeChatState(root, "c", "oc", "b/ob", state); err != nil {
		t.Fatal(err)
	}
	write(t, left, `{"cliSessionId":"c1","lastActivityAt":300,"isArchived":true}`)
	write(t, target, `{"cliSessionId":"c1","lastActivityAt":100,"isArchived":false}`)
	if _, err := syncSessions(root, "c", "oc", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeChatState(root, "c", "oc", "b/ob", state); err != nil {
		t.Fatal(err)
	}
	if got := chatField(t, target, "isArchived"); got != "true" {
		t.Fatalf("isArchived = %s, want the archive made in b", got)
	}
}

func TestMergeChatStateTwoEntriesOfOneConversation(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "chat-state.json")
	x := filepath.Join(root, "c", "oc", "local_x.json")
	y := filepath.Join(root, "c", "oc", "local_y.json")
	a := filepath.Join(root, "a", "oa", "local_x.json")
	for _, p := range []string{x, y, a} {
		write(t, p, `{"cliSessionId":"c1","isArchived":false}`)
	}
	if _, err := mergeChatState(root, "a", "oa", "c/oc", state); err != nil {
		t.Fatal(err)
	}
	write(t, x, `{"cliSessionId":"c1","isArchived":true}`)
	if _, err := mergeChatState(root, "a", "oa", "c/oc", state); err != nil {
		t.Fatal(err)
	}
	write(t, a, `{"cliSessionId":"c1","isArchived":false}`)
	for range 2 {
		if _, err := mergeChatState(root, "a", "oa", "c/oc", state); err != nil {
			t.Fatal(err)
		}
		if got := chatField(t, a, "isArchived"); got != "false" {
			t.Fatalf("isArchived = %s; an old change in c came back", got)
		}
	}
}

func TestMergeChatStateFirstSyncKeepsAnUnarchive(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "chat-state.json")
	write(t, filepath.Join(root, "a", "oa", "local_x.json"), `{"cliSessionId":"c1","isArchived":false,"autoArchiveExempt":true}`)
	c := filepath.Join(root, "c", "oc", "local_x.json")
	write(t, c, `{"cliSessionId":"c1","isArchived":true}`)
	if _, err := mergeChatState(root, "c", "oc", "", state); err != nil {
		t.Fatal(err)
	}
	if got := chatField(t, c, "isArchived"); got != "false" {
		t.Fatalf("isArchived = %s, want the user's unarchive", got)
	}
}
