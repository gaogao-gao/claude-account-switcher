package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
)

// The app keeps the Claude Code chats pinned in its sidebar per profile, in
// claude_desktop_config.json under preferences.epitaxyPrefs, and takes them
// from there when it starts with no pins of its own, as on a profile nothing
// was pinned in yet; once it has pins it keeps its own list and writes it
// there at start. All accounts share the official profile's file, so only
// the third-party profile needs them carried over. syncPins does that with
// the chat sync, adding only, to that one key of the profile the app does
// not run on.

const pinsKey = "starred-local-code-sessions"

// pinList is the pinned chats in an app config, with what is needed to write
// them back in the same form.
type pinList struct {
	cfg, prefs, ep *object
	ids            []string
	// encoded says the list is stored as a JSON string holding the array.
	encoded bool
}

// childObject is the object under key in o, an empty one when there is none.
func childObject(o *object, key, name string) (*object, error) {
	raw, ok := o.get(key)
	if !ok || compactJSON(raw) == "" {
		return &object{vals: map[string]json.RawMessage{}}, nil
	}
	return parseObject(raw, name+": "+key)
}

// readPins reads the pinned chats of the app config at path. A missing file
// has none; one that cannot be read is an error, so it is not written.
func readPins(path string) (*pinList, error) {
	cfg, err := readObject(path)
	if errors.Is(err, fs.ErrNotExist) {
		cfg, err = &object{vals: map[string]json.RawMessage{}}, nil
	}
	if err != nil {
		return nil, err
	}
	p := &pinList{cfg: cfg}
	if p.prefs, err = childObject(cfg, "preferences", path); err != nil {
		return nil, err
	}
	if p.ep, err = childObject(p.prefs, "epitaxyPrefs", path); err != nil {
		return nil, err
	}
	raw, ok := p.ep.get(pinsKey)
	if !ok || compactJSON(raw) == "" {
		return p, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		p.encoded, raw = true, json.RawMessage(s)
	}
	if err := json.Unmarshal(raw, &p.ids); err != nil {
		return nil, err
	}
	return p, nil
}

// write saves ids as the pinned chats at path, leaving everything else as
// it was.
func (p *pinList) write(path string, ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	v, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if p.encoded {
		v, _ = json.Marshal(string(v))
	}
	p.ep.set(pinsKey, v)
	p.prefs.set("epitaxyPrefs", p.ep.raw())
	p.cfg.set("preferences", p.prefs.raw())
	return p.cfg.write(path)
}

// scratchChats is the ids of the chats in root, a chat list, that are
// scratch chats of profile dir.
func scratchChats(root, dir string) map[string]bool {
	ids := map[string]bool{}
	paths, _ := filepath.Glob(filepath.Join(root, "*", "*", "local_*.json"))
	for _, path := range paths {
		if e, err := readSessionIndex(path); err == nil && scratchOf(e.cwd, dir) != "" {
			ids[strings.TrimSuffix(e.name, ".json")] = true
		}
	}
	return ids
}

// syncPins adds the pinned chats of the app config at source that the one
// at target lacks to the end of target's, which alone it writes. It never
// unpins: once the app has pins of its own in a profile it writes them over
// this list at start, so a missing pin says nothing. The chats in theirs
// belong to source alone and are left out. Returns the pins added.
func syncPins(target, source string, theirs map[string]bool) (int, error) {
	t, err := readPins(target)
	if err != nil {
		return 0, err
	}
	s, err := readPins(source)
	if err != nil {
		return 0, err
	}
	has := map[string]bool{}
	for _, id := range t.ids {
		has[id] = true
	}
	ids := t.ids
	for _, id := range s.ids {
		if !has[id] && !theirs[id] {
			has[id] = true
			ids = append(ids, id)
		}
	}
	n := len(ids) - len(t.ids)
	if n == 0 {
		return 0, nil
	}
	return n, t.write(target, ids)
}
