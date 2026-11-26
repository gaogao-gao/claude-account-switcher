package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/journal"
)

func taskIDs(t *testing.T, path string) string {
	t.Helper()
	var f struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"scheduledTasks"`
	}
	if err := json.Unmarshal([]byte(read(t, path)), &f); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, x := range f.Tasks {
		ids = append(ids, x.ID)
	}
	return strings.Join(ids, ",")
}

func TestSyncTasksAddsAndDeletes(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "tasks.json")
	a := filepath.Join(root, "a", "oa", "scheduled-tasks.json")
	b := filepath.Join(root, "b", "ob", "scheduled-tasks.json")
	write(t, a, `{"scheduledTasks":[{"id":"t1","cron":"x"}],"recordedSkips":{}}`)
	write(t, b, `{"scheduledTasks":[],"recordedSkips":{"own":1}}`)
	if _, err := syncTasks(root, "b", "ob", "a/oa", state); err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(t, b); got != "t1" {
		t.Fatalf("b tasks = %q, want t1", got)
	}
	if !strings.Contains(read(t, b), `"own"`) {
		t.Fatal("b lost its own fields")
	}
	// Deleted in b; a follows.
	write(t, b, `{"scheduledTasks":[],"recordedSkips":{}}`)
	if _, err := syncTasks(root, "a", "oa", "b/ob", state); err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(t, a); got != "" {
		t.Fatalf("a tasks = %q, want none", got)
	}
	if n, err := syncTasks(root, "b", "ob", "a/oa", state); err != nil || n != 0 {
		t.Fatalf("idle sync = %d, %v", n, err)
	}
}

// putLS writes key as the app does.
func putLS(t *testing.T, dir string, kv map[string]string) {
	t.Helper()
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for k, v := range kv {
		if err := db.Put(lsKey(k), encodeLS(v, true), nil); err != nil {
			t.Fatal(err)
		}
	}
}

func getLS(t *testing.T, dir, key string) string {
	t.Helper()
	db, err := openLSDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := db.get(lsKey(key))
	if !ok {
		return ""
	}
	s, _, _ := decodeLS(raw)
	return s
}

func TestSyncLocalStorage(t *testing.T) {
	dir, tmp := t.TempDir(), t.TempDir()
	state, backups := filepath.Join(tmp, "ls.json"), filepath.Join(tmp, "backups")
	putLS(t, dir, map[string]string{
		"dframe-store": `{"state":{"collapsed":false,"codeSidebarByScope":{"a/o":{"sections":[1]}},"customGroupsByScope":{}},"version":1}`,
		"LSS-persisted.epitaxy-folder-permission-mode-v2.a": `{"value":{"D:\\项目":"bypassPermissions"},"tabId":"","timestamp":1}`,
		"LSS-persisted.epitaxy-folder-permission-mode-v2.b": `{"value":{"D:\\x":"default"},"tabId":"","timestamp":1}`,
	})
	scopes := []string{"b/o", "a/o"}
	n, err := syncLocalStorage(dir, scopes, "b/o", "a/o", state, backups)
	if err != nil || n != 2 {
		t.Fatalf("sync = %d, %v; want sections and permission modes", n, err)
	}
	var store struct {
		State map[string]json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal([]byte(getLS(t, dir, "dframe-store")), &store); err != nil {
		t.Fatal(err)
	}
	if got := compactJSON(store.State["codeSidebarByScope"]); got != `{"a/o":{"sections":[1]},"b/o":{"sections":[1]}}` {
		t.Fatalf("codeSidebarByScope = %s", got)
	}
	if got := getLS(t, dir, "LSS-persisted.epitaxy-folder-permission-mode-v2.b"); !strings.Contains(got, `"D:\\项目":"bypassPermissions"`) || !strings.Contains(got, `"D:\\x":"default"`) {
		t.Fatalf("permission modes = %s", got)
	}
	if entries, _ := os.ReadDir(backups); len(entries) != 1 {
		t.Fatalf("%d backups, want one before the write", len(entries))
	}
	// Nothing new: no write, no backup.
	if n, err := syncLocalStorage(dir, scopes, "a/o", "b/o", state, backups); err != nil || n != 1 {
		// a gets the merged permission modes once.
		t.Fatalf("sync back = %d, %v", n, err)
	}
	if n, err := syncLocalStorage(dir, scopes, "b/o", "a/o", state, backups); err != nil || n != 0 {
		t.Fatalf("idle sync = %d, %v", n, err)
	}
	if entries, _ := os.ReadDir(backups); len(entries) > 2 {
		t.Fatalf("%d backups after an idle sync", len(entries))
	}
}

func TestEncodeLSRoundTrip(t *testing.T) {
	for _, s := range []string{"plain", "D:\\项目", "é"} {
		got, _, ok := decodeLS(encodeLS(s, true))
		if !ok || got != s {
			t.Fatalf("round trip of %q = %q", s, got)
		}
	}
}

func TestSyncSpacesPropagatesDeletion(t *testing.T) {
	root, seen := t.TempDir(), filepath.Join(t.TempDir(), "seen.json")
	a := filepath.Join(root, "a", "oa", "spaces.json")
	b := filepath.Join(root, "b", "ob", "spaces.json")
	write(t, a, `{"spaces":[{"id":"p1","updatedAt":1}]}`)
	write(t, b, `{"spaces":[]}`)
	if _, err := syncSpaces(root, "b", "ob", seen); err != nil {
		t.Fatal(err)
	}
	if _, err := syncSpaces(root, "a", "oa", seen); err != nil {
		t.Fatal(err)
	}
	// Deleted in b; a follows.
	write(t, b, `{"spaces":[]}`)
	if _, err := syncSpaces(root, "a", "oa", seen); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, a), "p1") {
		t.Fatal("project deleted in b stayed in a")
	}
}

func TestPruneDirsKeepsNewest(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"20260101", "20260102", "20260103", "20260104"} {
		if err := os.MkdirAll(filepath.Join(root, n), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	pruneDirs(root, 3, 0)
	entries, _ := os.ReadDir(root)
	if len(entries) != 3 || entries[0].Name() != "20260102" {
		t.Fatalf("left %v", entries)
	}
}

func TestAppendLogRecordAcrossBlocks(t *testing.T) {
	for _, size := range []int64{0, logBlock - 3, logBlock - 10, 5000} {
		prefix := make([]byte, size)
		payload := bytes.Repeat([]byte("x"), 2*logBlock+123)
		// The prefix stands for earlier records; only the new one is checked.
		data := appendLogRecord(nil, size, payload)
		r := journal.NewReader(bytes.NewReader(append(prefixRecords(t, prefix), data...)), nil, true, true)
		var got []byte
		for {
			rec, err := r.Next()
			if err != nil {
				break
			}
			got, _ = io.ReadAll(rec)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("size %d: read back %d bytes, want %d", size, len(got), len(payload))
		}
	}
}

// prefixRecords is a valid log of exactly len(b) bytes, made of padding
// records, so a record appended after it starts at that offset.
func prefixRecords(t *testing.T, b []byte) []byte {
	t.Helper()
	var out []byte
	for len(out) < len(b) {
		off := len(out) % logBlock
		room := min(logBlock-off, len(b)-len(out))
		if room < logHeader || logBlock-off < logHeader {
			out = append(out, make([]byte, room)...)
			continue
		}
		out = appendLogRecord(out, int64(len(out)), make([]byte, room-logHeader))
	}
	return out
}

func TestSyncSpacesDeletionSurvivesStaleAccount(t *testing.T) {
	root, seen := t.TempDir(), filepath.Join(t.TempDir(), "seen.json")
	path := func(acct string) string { return filepath.Join(root, acct, "o"+acct, "spaces.json") }
	for _, acct := range []string{"a", "b", "c"} {
		write(t, path(acct), `{"spaces":[{"id":"p1","updatedAt":1}]}`)
	}
	sync := func(acct string) {
		t.Helper()
		if _, err := syncSpaces(root, acct, "o"+acct, seen); err != nil {
			t.Fatal(err)
		}
	}
	for _, acct := range []string{"a", "b", "c"} {
		sync(acct)
	}
	write(t, path("a"), `{"spaces":[]}`)
	for _, acct := range []string{"b", "a", "b", "a", "c"} {
		sync(acct)
		if strings.Contains(read(t, path(acct)), "p1") {
			t.Fatalf("project deleted in a is back in %s", acct)
		}
	}
}

func TestSyncTasksIgnoresMissingFile(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "tasks.json")
	a := filepath.Join(root, "a", "oa", "scheduled-tasks.json")
	b := filepath.Join(root, "b", "ob", "scheduled-tasks.json")
	c := filepath.Join(root, "c", "oc", "scheduled-tasks.json")
	for _, p := range []string{a, b, c} {
		write(t, p, `{"scheduledTasks":[{"id":"u"},{"note":"no id"}]}`)
	}
	for _, acct := range []string{"a", "b", "c"} {
		if _, err := syncTasks(root, acct, "o"+acct, "", state); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(c); err != nil {
		t.Fatal(err)
	}
	for _, acct := range []string{"c", "b", "a"} {
		if _, err := syncTasks(root, acct, "o"+acct, "c/oc", state); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{a, b} {
		if got := taskIDs(t, p); got != "u," {
			t.Fatalf("%s tasks = %q; a missing file deleted them", p, got)
		}
	}
}

func TestLSDBRefusesToWriteAfterTornTail(t *testing.T) {
	dir := t.TempDir()
	putLS(t, dir, map[string]string{"k": "v"})
	db, err := openLSDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(db.log, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	torn := appendLogRecord(nil, int64(db.logEnd), []byte("0123456789abcdefghij"))
	f.Write(torn[:len(torn)-5])
	f.Close()
	db, err = openLSDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.put(map[string][]byte{string(lsKey("x")): encodeLS("y", true)}); err == nil {
		t.Fatal("wrote after a partial record")
	}
}

func TestLSDBPutReadsBack(t *testing.T) {
	dir := t.TempDir()
	putLS(t, dir, map[string]string{"k": "v"})
	for i := range 3 {
		db, err := openLSDB(dir)
		if err != nil {
			t.Fatal(err)
		}
		v := strings.Repeat("z", 40000*i+1)
		if err := db.put(map[string][]byte{string(lsKey("k")): encodeLS(v, true)}); err != nil {
			t.Fatal(err)
		}
		if got := getLS(t, dir, "k"); got != v {
			t.Fatalf("round %d: read back %d bytes", i, len(got))
		}
	}
	// goleveldb, a second reader, agrees.
	gdb, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gdb.Close()
	raw, _ := gdb.Get(lsKey("k"), nil)
	if s, _, _ := decodeLS(raw); len(s) != 80001 {
		t.Fatalf("goleveldb reads %d bytes", len(s))
	}
}

func TestSyncTasksIgnoresWipedAccount(t *testing.T) {
	root, state := t.TempDir(), filepath.Join(t.TempDir(), "tasks.json")
	a := filepath.Join(root, "a", "oa", "scheduled-tasks.json")
	b := filepath.Join(root, "b", "ob", "scheduled-tasks.json")
	for _, p := range []string{a, b} {
		write(t, p, `{"scheduledTasks":[{"id":"t1"},{"id":"t2"},{"id":"t3"}]}`)
	}
	for _, acct := range []string{"a", "b"} {
		if _, err := syncTasks(root, acct, "o"+acct, "", state); err != nil {
			t.Fatal(err)
		}
	}
	write(t, b, `{"scheduledTasks":[]}`)
	if _, err := syncTasks(root, "a", "oa", "b/ob", state); err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(t, a); got != "t1,t2,t3" {
		t.Fatalf("a tasks = %q; b's wiped list deleted them", got)
	}
	if _, err := syncTasks(root, "b", "ob", "a/oa", state); err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(t, b); got != "t1,t2,t3" {
		t.Fatalf("b tasks = %q, want them back", got)
	}
	// Deleting most but not all tasks still spreads.
	write(t, b, `{"scheduledTasks":[{"id":"t1"}]}`)
	if _, err := syncTasks(root, "a", "oa", "b/ob", state); err != nil {
		t.Fatal(err)
	}
	if got := taskIDs(t, a); got != "t1" {
		t.Fatalf("a tasks = %q, want t2 and t3 deleted", got)
	}
	if backups, _ := filepath.Glob(filepath.Join(filepath.Dir(state), "tasks-backups", "a", "*", "scheduled-tasks.json")); len(backups) == 0 {
		t.Fatal("a's tasks were rewritten without a backup")
	}
}

func TestPruneDirsKeepsLastWeek(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	names := []string{"20200101-000000", "20200102", "20200103-000000", "20200104-000000",
		now.Add(-6 * 24 * time.Hour).Format("20060102-150405"), now.Format("20060102-150405"), now.Format("20060102-150405") + "-2"}
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	pruneDirs(root, 3, keepBackupsFor)
	entries, _ := os.ReadDir(root)
	if len(entries) != 3 || entries[0].Name() != names[4] {
		t.Fatalf("left %v, want the last week only", entries)
	}
	if d := stampDir(root); filepath.Base(d) == names[5] {
		t.Fatal("stampDir reused a folder of this second")
	}
}

func TestRefreshProfileAuth(t *testing.T) {
	d := desktop{dir: t.TempDir()}
	profile := t.TempDir()
	write(t, d.configPath(), `{"oauth:tokenCacheV2":"new","lastKnownAccountUuid":"u1","theme":"dark"}`)
	if changed, err := refreshProfileAuth(d, profile, "u1"); err != nil || changed {
		t.Fatalf("created a profile: %v, %v", changed, err)
	}
	write(t, filepath.Join(profile, "config.json"), `{"oauth:tokenCache":"old1","oauth:tokenCacheV2":"old","lastKnownAccountUuid":"u1"}`)
	if changed, err := refreshProfileAuth(d, profile, "u2"); err != nil || changed {
		t.Fatalf("wrote another account's sign-in: %v, %v", changed, err)
	}
	if changed, err := refreshProfileAuth(d, profile, "u1"); err != nil || !changed {
		t.Fatalf("renewed sign-in not saved: %v, %v", changed, err)
	}
	got := read(t, filepath.Join(profile, "config.json"))
	if !strings.Contains(got, `"new"`) || strings.Contains(got, "old1") || strings.Contains(got, "theme") {
		t.Fatalf("profile = %s", got)
	}
	if changed, _ := refreshProfileAuth(d, profile, "u1"); changed {
		t.Fatal("rewrote an unchanged profile")
	}
}

func TestSyncLocalStorageWaitsForLock(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fcntl locks do not keep out the process that holds them")
	}
	dir, tmp := t.TempDir(), t.TempDir()
	putLS(t, dir, map[string]string{"dframe-store": `{"state":{"codeSidebarByScope":{"a/o":{"sections":[1]}}}}`})
	unlock, err := lockLS(dir)
	if err != nil {
		t.Fatal(err)
	}
	// goleveldb may already fail to open it; either way nothing is written.
	n, _ := syncLocalStorage(dir, []string{"b/o", "a/o"}, "b/o", "a/o", filepath.Join(tmp, "ls.json"), filepath.Join(tmp, "backups"))
	unlock()
	if n != 0 {
		t.Fatalf("sync with the database in use changed %d settings", n)
	}
	if strings.Contains(getLS(t, dir, "dframe-store"), "b/o") {
		t.Fatal("wrote while the database was in use")
	}
}
