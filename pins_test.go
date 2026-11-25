package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

// pinConfig is an app config holding pins, with other keys around them.
func pinConfig(pins string) string {
	return `{"isDxtAutoUpdatesEnabled":true,"preferences":{"sidebarMode":"code","epitaxyPrefs":{` +
		`"starred-local-code-sessions":` + pins + `,"starred-cowork-spaces":["space"],"starred-session-groups":["g"],` +
		`"starred-local-routines":["r"],"dframe-local-slice":{"x":1}},"menuBarEnabled":false}}`
}

func pinsOf(t *testing.T, path string) []string {
	t.Helper()
	p, err := readPins(path)
	if err != nil {
		t.Fatal(err)
	}
	return p.ids
}

func TestSyncPins(t *testing.T) {
	dir := t.TempDir()
	official, third := filepath.Join(dir, "official.json"), filepath.Join(dir, "3p.json")
	write(t, official, pinConfig(`["local_a","local_b"]`))
	write(t, third, pinConfig(`["local_c"]`))

	// Each side gets the other's pins after its own.
	if n, err := syncPins(third, official, nil); err != nil || n != 2 {
		t.Fatalf("sync added %d, %v; want 2", n, err)
	}
	if n, err := syncPins(official, third, nil); err != nil || n != 1 {
		t.Fatalf("sync added %d, %v; want 1", n, err)
	}
	if got, want := pinsOf(t, third), []string{"local_c", "local_a", "local_b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("third-party pins %v, want %v", got, want)
	}
	if got, want := pinsOf(t, official), []string{"local_a", "local_b", "local_c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("official pins %v, want %v", got, want)
	}

	// A new pin carries over; a pin the other side lacks is never removed.
	write(t, official, pinConfig(`["local_b","local_d"]`))
	if n, err := syncPins(third, official, nil); err != nil || n != 1 {
		t.Fatalf("sync added %d, %v; want 1", n, err)
	}
	if got, want := pinsOf(t, third), []string{"local_c", "local_a", "local_b", "local_d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("third-party pins %v, want %v", got, want)
	}

	// Nothing else in the file changes.
	want := map[string]any{}
	_ = json.Unmarshal([]byte(pinConfig(`["local_c","local_a","local_b","local_d"]`)), &want)
	got := map[string]any{}
	if err := json.Unmarshal([]byte(read(t, third)), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("third-party config %v, want %v", got, want)
	}
}

func TestSyncPinsFromEmptySideRemovesNothing(t *testing.T) {
	dir := t.TempDir()
	official := filepath.Join(dir, "official.json")
	write(t, official, pinConfig(`["local_a","local_b"]`))
	before := read(t, official)
	// The third-party profile has no pins, or no config at all.
	for _, third := range []string{filepath.Join(dir, "3p.json"), filepath.Join(dir, "missing.json")} {
		if third == filepath.Join(dir, "3p.json") {
			write(t, third, `{"deploymentMode":"3p"}`)
		}
		if n, err := syncPins(official, third, nil); err != nil || n != 0 {
			t.Fatalf("sync added %d, %v; want 0", n, err)
		}
		if read(t, official) != before {
			t.Fatal("official config was written")
		}
	}
}

func TestSyncPinsKeepsStringForm(t *testing.T) {
	dir := t.TempDir()
	official, third := filepath.Join(dir, "official.json"), filepath.Join(dir, "3p.json")
	write(t, official, pinConfig(`["local_a"]`))
	write(t, third, pinConfig(`"[\"local_c\"]"`))
	if _, err := syncPins(third, official, nil); err != nil {
		t.Fatal(err)
	}
	p, err := readPins(third)
	if err != nil {
		t.Fatal(err)
	}
	if !p.encoded || !reflect.DeepEqual(p.ids, []string{"local_c", "local_a"}) {
		t.Fatalf("third-party pins %v (string form %v), want [local_c local_a] as a string", p.ids, p.encoded)
	}
	if _, err := syncPins(official, third, nil); err != nil {
		t.Fatal(err)
	}
	if p, _ := readPins(official); p.encoded || !reflect.DeepEqual(p.ids, []string{"local_a", "local_c"}) {
		t.Fatalf("official pins %v (string form %v), want [local_a local_c] as an array", p.ids, p.encoded)
	}
}

func TestSyncPinsLeavesScratchChatsOut(t *testing.T) {
	dir := t.TempDir()
	officialDir := filepath.Join(dir, "Claude")
	official := desktop{dir: officialDir}
	third := filepath.Join(dir, "3p.json")
	entry := func(id, cwd string) string {
		data, _ := json.Marshal(map[string]any{"sessionId": id, "cliSessionId": "c" + id, "cwd": cwd})
		return string(data)
	}
	write(t, filepath.Join(official.sessionsDir(), "a", "o", "local_s.json"),
		entry("local_s", filepath.Join(officialDir, "scratch-workspaces", "a", "o", "scratch-1")))
	write(t, filepath.Join(official.sessionsDir(), "a", "o", "local_p.json"), entry("local_p", filepath.Join(dir, "project")))
	write(t, official.appConfigPath(), pinConfig(`["local_s","local_p"]`))
	write(t, third, pinConfig(`[]`))

	if _, err := syncPins(third, official.appConfigPath(), scratchChats(official.sessionsDir(), officialDir)); err != nil {
		t.Fatal(err)
	}
	if got, want := pinsOf(t, third), []string{"local_p"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("third-party pins %v, want %v", got, want)
	}
}
