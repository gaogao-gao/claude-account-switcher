package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSyncSpaces(t *testing.T) {
	root := t.TempDir()
	seen := filepath.Join(t.TempDir(), "spaces-seen.json")
	write(t, filepath.Join(root, "a", "oa", "spaces.json"), `{"spaces":[
		{"id":"web","name":"Web v2","folders":[{"path":"/src/web"}],"projects":[{"uuid":"pa"}],"migration":{"stage":"complete"},"links":[],"createdAt":1,"updatedAt":300,"future":true},
		{"id":"api","name":"API","folders":[],"projects":[{"uuid":"pb"}],"links":[],"createdAt":1,"updatedAt":100}]}`)
	write(t, filepath.Join(root, "b", "ob", "spaces.json"), `{"spaces":[
		{"id":"web","name":"Web","folders":[],"projects":[{"uuid":"own"}],"links":[],"createdAt":1,"updatedAt":200}],"version":2}`)

	n, err := syncSpaces(root, "b", "ob", seen)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("wrote %d projects, want 2 (newer web, new api)", n)
	}
	file, got := spacesByID(t, filepath.Join(root, "b", "ob", "spaces.json"))
	if string(file["version"]) != "2" {
		t.Fatal("other keys of the file were lost")
	}
	web := got["web"]
	if web["name"] != "Web v2" || web["future"] != true {
		t.Fatalf("web not refreshed: %v", web)
	}
	if p := web["projects"].([]any); len(p) != 1 || p[0].(map[string]any)["uuid"] != "own" {
		t.Fatalf("web lost its own org's projects: %v", web["projects"])
	}
	if _, ok := web["migration"]; ok {
		t.Fatal("web took the other org's migration")
	}
	if _, ok := got["api"]["projects"]; ok {
		t.Fatal("api kept the other org's projects")
	}
	if n, _ := syncSpaces(root, "b", "ob", seen); n != 0 {
		t.Fatalf("second sync wrote %d, want 0", n)
	}

	// A project deleted in b stays deleted.
	write(t, filepath.Join(root, "b", "ob", "spaces.json"), `{"spaces":[{"id":"web","name":"Web v2","updatedAt":300}]}`)
	if n, _ := syncSpaces(root, "b", "ob", seen); n != 0 {
		t.Fatalf("deleted project came back, wrote %d", n)
	}

	// The deletion in b reaches the other accounts, a new one included.
	if n, _ := syncSpaces(root, "c", "oc", seen); n != 1 {
		t.Fatalf("new account got %d projects, want web only", n)
	}
	if n, _ := syncSpaces(root, "a", "oa", seen); n != 1 {
		t.Fatalf("a removed %d, want the project deleted in b", n)
	}

	// An unreadable file is left alone.
	bad := filepath.Join(root, "d", "od", "spaces.json")
	write(t, bad, "{not json")
	if _, err := syncSpaces(root, "d", "od", seen); err == nil {
		t.Fatal("no error for an unreadable spaces file")
	}
	if read(t, bad) != "{not json" {
		t.Fatal("unreadable spaces file was overwritten")
	}
}

func spacesByID(t *testing.T, path string) (map[string]json.RawMessage, map[string]map[string]any) {
	t.Helper()
	var file map[string]json.RawMessage
	if err := json.Unmarshal([]byte(read(t, path)), &file); err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	if err := json.Unmarshal(file["spaces"], &list); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, sp := range list {
		out[sp["id"].(string)] = sp
	}
	return file, out
}

func TestSyncSpacesIgnoresWipedAccount(t *testing.T) {
	root, seen := t.TempDir(), filepath.Join(t.TempDir(), "seen.json")
	path := func(acct string) string { return filepath.Join(root, acct, "o"+acct, "spaces.json") }
	all := `{"spaces":[{"id":"p1","updatedAt":1},{"id":"p2","updatedAt":1},{"id":"p3","updatedAt":1}]}`
	write(t, path("a"), all)
	write(t, path("b"), all)
	for _, acct := range []string{"a", "b"} {
		if _, err := syncSpaces(root, acct, "o"+acct, seen); err != nil {
			t.Fatal(err)
		}
	}
	for _, wipe := range []string{`{"spaces":[]}`, `{}`} {
		write(t, path("b"), wipe)
		if n, err := syncSpaces(root, "a", "oa", seen); err != nil || n != 0 {
			t.Fatalf("a after b was wiped to %s: wrote %d, %v", wipe, n, err)
		}
		if n, err := syncSpaces(root, "b", "ob", seen); err != nil || n != 2 && n != 3 {
			t.Fatalf("b wiped to %s got %d projects back, %v", wipe, n, err)
		}
		_, got := spacesByID(t, path("b"))
		if len(got) != 3 {
			t.Fatalf("b holds %d projects after %s, want 3", len(got), wipe)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(seen), "spaces-backups", "b", "*", "spaces.json"))
	if len(backups) == 0 {
		t.Fatal("b's spaces.json was rewritten without a backup")
	}

	// Deleting most but not all projects is the user's doing and spreads.
	write(t, path("b"), `{"spaces":[{"id":"p1","updatedAt":1}]}`)
	for _, acct := range []string{"a", "b"} {
		if _, err := syncSpaces(root, acct, "o"+acct, seen); err != nil {
			t.Fatal(err)
		}
		if _, got := spacesByID(t, path(acct)); len(got) != 1 || got["p1"] == nil {
			t.Fatalf("%s holds %v after b deleted p2 and p3", acct, got)
		}
	}
}
