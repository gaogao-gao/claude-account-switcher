package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

// Some sidebar settings live in the app's Local Storage, a LevelDB that all
// accounts share, under keys or entries per account: how the Claude Code
// sections are ordered and collapsed, custom groups, and the permission mode
// picked per folder. syncLocalStorage carries them over like mergeChatState
// does the chat settings. It runs while the app is closed, and writes, after
// a backup, only when the target lacks a change; see lsDB for how.

const lsOrigin = "https://claude.ai"

// errLSInUse is returned when another process, normally Claude, has the
// Local Storage database open.
var errLSInUse = errors.New("local storage is in use")

// lsSetting is one setting, found in each account's copy at path inside the
// JSON stored under key. "{scope}" stands for account/org, "{account}" for
// the account alone.
type lsSetting struct {
	name, key string
	path      []string
	// first merges the copies the first time the switcher sees them.
	first func(vals map[string]string, prefer, target string) string
}

var lsSettings = []lsSetting{
	{"codeSidebar", "dframe-store", []string{"state", "codeSidebarByScope", "{scope}"}, firstOf},
	{"customGroups", "dframe-store", []string{"state", "customGroupsByScope", "{scope}"}, firstOf},
	{"codeSections", "LSS-persisted.dframe-code-sections", []string{"value", "{scope}"}, firstOf},
	{"folderPermissionMode", "LSS-persisted.epitaxy-folder-permission-mode-v2.{account}", []string{"value"}, unionObjects},
	{"permissionModeAcks", "LSS-persisted.epitaxy-perm-mode-acks-v2.{account}", []string{"value"}, unionLists},
}

func (s lsSetting) keyFor(scope string) string {
	account, _, _ := strings.Cut(scope, "/")
	return strings.ReplaceAll(s.key, "{account}", account)
}

func (s lsSetting) pathFor(scope string) []string {
	p := append([]string{}, s.path...)
	for i := range p {
		if p[i] == "{scope}" {
			p[i] = scope
		}
	}
	return p
}

// syncLocalStorage brings target's copies of lsSettings up to date. scopes
// lists every account as account/org; prefer is the one the user just left.
// Returns how many settings it changed.
func syncLocalStorage(dir string, scopes []string, target, prefer, statePath, backupRoot string) (int, error) {
	if _, err := os.Stat(filepath.Join(dir, "CURRENT")); err != nil {
		return 0, nil
	}
	// The lock keeps goleveldb from opening the database on Windows, so it
	// is read first and checked under the lock to be what was read.
	read := lsFiles(dir)
	db, err := openLSDB(dir)
	if err != nil {
		return 0, fmt.Errorf("open local storage: %w", err)
	}
	docs := map[string]*lsDoc{}
	doc := func(key string) *lsDoc {
		if d, ok := docs[key]; ok {
			return d
		}
		d := readLSDoc(db, key)
		docs[key] = d
		return d
	}
	w := readThreeWay(statePath)
	type change struct {
		setting lsSetting
		value   string
	}
	var changes []change
	for _, s := range lsSettings {
		vals := map[string]string{}
		for _, scope := range scopes {
			if d := doc(s.keyFor(scope)); d.exists || scope == target {
				vals[scope] = d.get(s.pathFor(scope))
			}
		}
		truth := w.resolve(s.name, vals, []string{prefer}, func() string { return s.first(vals, prefer, target) })
		if vals[target] != truth {
			changes = append(changes, change{s, truth})
		}
	}
	if len(changes) == 0 {
		return 0, w.write(statePath)
	}

	batch := map[string][]byte{}
	written := map[string]bool{}
	for _, c := range changes {
		d := docs[c.setting.keyFor(target)]
		if !d.set(c.setting.pathFor(target), c.value) {
			log.Printf("local storage: no %s to put %s in", d.key, c.setting.name)
			continue
		}
		written[d.key] = true
	}
	for key := range written {
		raw, err := docs[key].encode()
		if err != nil {
			return 0, err
		}
		batch[string(lsKey(key))] = raw
	}
	if len(batch) == 0 {
		return 0, w.write(statePath)
	}
	// The app must not have the database open while a record is appended.
	unlock, err := lockLS(dir)
	if err != nil {
		return 0, fmt.Errorf("%w, the settings wait for the next sync: %v", errLSInUse, err)
	}
	defer unlock()
	if lsFiles(dir) != read {
		return 0, fmt.Errorf("%w: it changed while it was read, the settings wait for the next sync", errLSInUse)
	}
	if _, err := backupDir(dir, backupRoot); err != nil {
		return 0, fmt.Errorf("back up local storage: %w", err)
	}
	if err := db.put(batch); err != nil {
		return 0, fmt.Errorf("write local storage: %w", err)
	}
	n := 0
	for _, c := range changes {
		if written[c.setting.keyFor(target)] {
			w.saw(target, c.setting.name, c.value)
			n++
		}
	}
	return n, w.write(statePath)
}

// lsFiles lists the files of the LevelDB in dir but its LOCK, with their
// sizes and times, to tell whether anyone wrote it in between.
func lsFiles(dir string) string {
	entries, _ := os.ReadDir(dir)
	var b strings.Builder
	for _, e := range entries {
		if info, err := e.Info(); err == nil && e.Name() != "LOCK" {
			fmt.Fprintf(&b, "%s %d %d\n", e.Name(), info.Size(), info.ModTime().UnixNano())
		}
	}
	return b.String()
}

// lsDoc is the JSON a Local Storage key holds.
type lsDoc struct {
	key    string
	exists bool
	latin1 bool // how the app stored it; kept where the text allows
	root   any
}

func readLSDoc(db *lsDB, key string) *lsDoc {
	d := &lsDoc{key: key}
	raw, ok := db.get(lsKey(key))
	if !ok || len(raw) == 0 {
		return d
	}
	text, latin1, ok := decodeLS(raw)
	if !ok {
		return d
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if dec.Decode(&d.root) != nil {
		return d
	}
	d.exists, d.latin1 = true, latin1
	return d
}

// get returns the canonical JSON at path, "" if there is none.
func (d *lsDoc) get(path []string) string {
	cur := d.root
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		if cur, ok = m[p]; !ok {
			return ""
		}
	}
	out, err := json.Marshal(cur)
	if err != nil {
		return ""
	}
	return compactJSON(out)
}

// set puts value ("" removes it) at path. A key the app never wrote is
// created only for the LSS- wrapper the app reads back without more state.
func (d *lsDoc) set(path []string, value string) bool {
	if !d.exists {
		if !strings.HasPrefix(d.key, "LSS-") || len(path) != 1 || path[0] != "value" {
			return false
		}
		d.root = map[string]any{"value": nil, "tabId": ""}
		d.exists, d.latin1 = true, true
	}
	var v any
	if value != "" {
		dec := json.NewDecoder(strings.NewReader(value))
		dec.UseNumber()
		if dec.Decode(&v) != nil {
			return false
		}
	}
	m, ok := d.root.(map[string]any)
	if !ok {
		return false
	}
	for _, p := range path[:len(path)-1] {
		next, ok := m[p].(map[string]any)
		if !ok {
			if value == "" {
				return true
			}
			next = map[string]any{}
			m[p] = next
		}
		m = next
	}
	last := path[len(path)-1]
	if value == "" {
		delete(m, last)
	} else {
		m[last] = v
	}
	if root := d.root.(map[string]any); strings.HasPrefix(d.key, "LSS-") {
		// The app tells its own writes apart by this.
		root["timestamp"] = json.Number(fmt.Sprint(time.Now().UnixMilli()))
	}
	return true
}

func (d *lsDoc) encode() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(d.root); err != nil {
		return nil, err
	}
	return encodeLS(strings.TrimSuffix(buf.String(), "\n"), d.latin1), nil
}

// Chromium keeps a Local Storage key as "_" + origin + NUL + the encoded
// name, and encodes names and values with a leading byte: 1 for Latin-1, 0
// for UTF-16LE.
func lsKey(name string) []byte {
	return append([]byte("_"+lsOrigin+"\x00"), encodeLS(name, true)...)
}

func encodeLS(s string, latin1 bool) []byte {
	if latin1 {
		out := make([]byte, 1, len(s)+1)
		out[0] = 1
		for _, r := range s {
			if r > 0xff {
				return encodeLS(s, false)
			}
			out = append(out, byte(r))
		}
		return out
	}
	units := utf16.Encode([]rune(s))
	out := make([]byte, 1, 1+2*len(units))
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

func decodeLS(raw []byte) (text string, latin1, ok bool) {
	switch raw[0] {
	case 1:
		runes := make([]rune, len(raw)-1)
		for i, b := range raw[1:] {
			runes[i] = rune(b)
		}
		return string(runes), true, true
	case 0:
		if (len(raw)-1)%2 != 0 {
			return "", false, false
		}
		units := make([]uint16, (len(raw)-1)/2)
		for i := range units {
			units[i] = uint16(raw[1+2*i]) | uint16(raw[2+2*i])<<8
		}
		return string(utf16.Decode(units)), false, true
	}
	return "", false, false
}

// firstOf takes the copy of the account the user left, else the target's,
// else any account's.
func firstOf(vals map[string]string, prefer, target string) string {
	for _, k := range append([]string{prefer, target}, sortedKeys(vals)...) {
		if v := vals[k]; v != "" {
			return v
		}
	}
	return ""
}

// unionObjects merges JSON objects; on a clash the account the user left wins.
func unionObjects(vals map[string]string, prefer, target string) string {
	merged := map[string]json.RawMessage{}
	order := sortedKeys(vals)
	order = append(order, target, prefer)
	for _, k := range order {
		var m map[string]json.RawMessage
		if json.Unmarshal([]byte(vals[k]), &m) == nil {
			for kk, v := range m {
				merged[kk] = v
			}
		}
	}
	if len(merged) == 0 {
		return firstOf(vals, prefer, target)
	}
	out, _ := json.Marshal(merged)
	return compactJSON(out)
}

// unionLists joins JSON arrays without repeats, the target's items first.
func unionLists(vals map[string]string, prefer, target string) string {
	var merged []json.RawMessage
	have := map[string]bool{}
	for _, k := range append([]string{target, prefer}, sortedKeys(vals)...) {
		var list []json.RawMessage
		if json.Unmarshal([]byte(vals[k]), &list) != nil {
			continue
		}
		for _, item := range list {
			if c := compactJSON(item); !have[c] {
				have[c] = true
				merged = append(merged, item)
			}
		}
	}
	if merged == nil {
		return firstOf(vals, prefer, target)
	}
	out, _ := json.Marshal(merged)
	return compactJSON(out)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// keepLSBackups is how many Local Storage backups stay around.
const keepLSBackups = 3

// backupDir copies the files of dir into a new folder under root, keeping
// the newest keepLSBackups.
func backupDir(dir, root string) (string, error) {
	dst := stampDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "LOCK" {
			continue
		}
		if err := copyFile(filepath.Join(dir, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%s is empty", dir)
	}
	// Each is a copy of the whole database, so only the newest few stay.
	pruneDirs(root, keepLSBackups, 0)
	return dst, nil
}
