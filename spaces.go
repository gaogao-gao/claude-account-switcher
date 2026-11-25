package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Projects in the app (spaces internally) are kept per account in
// local-agent-mode-sessions/<account>/<org>/spaces.json. A chat in a project
// names it by spaceId, so a chat copied to another account only shows in its
// project once that account has the project too.
//
// projects and migration tie a space to claude.ai projects of the org that
// made it. The other org cannot open those, so a copy leaves them out and
// the app sets the space up for its own org.
var orgBoundSpaceKeys = []string{"projects", "migration"}

type space = map[string]json.RawMessage

// syncSpaces copies the projects of every other account into the target
// account's spaces.json. It adds missing projects and refreshes older ones.
// seenPath remembers which projects each account had, so a project the user
// deleted in one account is not brought back, and is deleted in the target
// too. Returns how many projects it wrote or removed.
func syncSpaces(root, account, org, seenPath string) (int, error) {
	targetPath := filepath.Join(root, account, org, "spaces.json")
	paths, err := filepath.Glob(filepath.Join(root, "*", "*", "spaces.json"))
	if err != nil {
		return 0, err
	}

	newest := map[string]space{}
	var order []string
	seen := readSeen(seenPath)
	presence := readThreeWay(presencePath(seenPath))
	// Which account has which project, for the deletions.
	has := map[string]map[string]bool{}
	for _, p := range paths {
		if samePath(p, targetPath) {
			continue
		}
		file, spaces, err := readSpaces(p)
		if err != nil {
			continue
		}
		other := map[string]bool{}
		key := filepath.Base(filepath.Dir(filepath.Dir(p))) + "/" + filepath.Base(filepath.Dir(p))
		has[key] = other
		for _, sp := range spaces {
			other[spaceID(sp)] = true
		}
		checkSpacesWiped(presence, key, file, other)
		for _, sp := range spaces {
			id := spaceID(sp)
			if cur, ok := newest[id]; !ok {
				order = append(order, id)
				newest[id] = sp
			} else if updatedAt(sp) > updatedAt(cur) {
				newest[id] = sp
			}
		}
	}

	key := account + "/" + org
	file, spaces, err := readSpaces(targetPath)
	if errors.Is(err, os.ErrNotExist) {
		// A file that went away gets its projects back.
		file = map[string]json.RawMessage{}
		presence.markWiped(key, "projects")
	} else if err != nil {
		// Leave a file the app cannot read either to the app's own recovery.
		return 0, err
	}
	if err == nil {
		mine := map[string]bool{}
		for _, sp := range spaces {
			mine[spaceID(sp)] = true
		}
		has[key] = mine
		checkSpacesWiped(presence, key, file, mine)
	}
	// A project the user deleted in some account goes from all of them. An
	// account counts as deleting one when it held the project at the last
	// sync and holds it no more.
	ids := map[string]bool{}
	for _, m := range has {
		for id := range m {
			ids[id] = true
		}
	}
	for id := range presence.Agreed {
		ids[id] = true
	}
	deleted := map[string]bool{}
	for id := range ids {
		cur := map[string]string{}
		for k, m := range has {
			cur[k] = map[bool]string{true: "1"}[m[id]]
		}
		if presence.resolve(id, cur, nil, func() string { return "1" }) == "" {
			deleted[id] = true
		}
	}
	presence.forget(func(id string) bool {
		if presence.Agreed[id] != "" {
			return true
		}
		for _, m := range has {
			if m[id] {
				return true
			}
		}
		return false
	})
	// A wiped target gets its projects back.
	had := map[string]bool{}
	for _, id := range seen[key] {
		had[id] = !presence.wiped[key]
	}
	index := map[string]int{}
	for i, sp := range spaces {
		index[spaceID(sp)] = i
	}

	written := 0
	if len(deleted) > 0 {
		kept := spaces[:0]
		for _, sp := range spaces {
			if deleted[spaceID(sp)] {
				written++
				continue
			}
			kept = append(kept, sp)
		}
		spaces = kept
		index = map[string]int{}
		for i, sp := range spaces {
			index[spaceID(sp)] = i
		}
	}
	for _, id := range order {
		if deleted[id] {
			continue
		}
		src := newest[id]
		i, ok := index[id]
		switch {
		case ok && updatedAt(src) > updatedAt(spaces[i]):
			spaces[i] = refreshSpace(spaces[i], src)
		case !ok && !had[id]:
			spaces = append(spaces, copySpace(src))
			index[id] = len(spaces) - 1
		default:
			continue
		}
		written++
	}

	if written > 0 {
		list, err := json.Marshal(spaces)
		if err != nil {
			return 0, err
		}
		file["spaces"] = list
		data, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			return 0, err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
			return 0, err
		}
		if err := backupSynced(targetPath, seenPath, "spaces", account); err != nil {
			return 0, err
		}
		if err := writeFileAtomic(targetPath, data); err != nil {
			return 0, err
		}
	}

	list := make([]string, 0, len(spaces))
	for _, sp := range spaces {
		list = append(list, spaceID(sp))
		presence.saw(key, spaceID(sp), "1")
	}
	for id := range deleted {
		presence.saw(key, id, "")
	}
	seen[key] = list
	if err := presence.write(presencePath(seenPath)); err != nil {
		return written, err
	}
	return written, writeSeen(seenPath, seen)
}

// checkSpacesWiped marks the project list of account key as reset when its
// spaces entry is gone or it looksWiped.
func checkSpacesWiped(presence *threeWay, key string, file map[string]json.RawMessage, ids map[string]bool) {
	if _, listed := file["spaces"]; !listed || presence.looksWiped(key, func(id string) bool { return ids[id] }) {
		presence.markWiped(key, "projects")
	}
}

// presencePath keeps, next to seenPath, which account had which project.
func presencePath(seenPath string) string {
	return strings.TrimSuffix(seenPath, filepath.Ext(seenPath)) + "-presence.json"
}

// readSpaces returns the whole file and its spaces that carry an id.
func readSpaces(path string) (map[string]json.RawMessage, []space, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, nil, err
	}
	var all, spaces []space
	if raw, ok := file["spaces"]; ok {
		if err := json.Unmarshal(raw, &all); err != nil {
			return nil, nil, err
		}
	}
	for _, sp := range all {
		if spaceID(sp) != "" {
			spaces = append(spaces, sp)
		}
	}
	return file, spaces, nil
}

func spaceID(sp space) string {
	var id string
	_ = json.Unmarshal(sp["id"], &id)
	return id
}

func updatedAt(sp space) float64 {
	var at float64
	_ = json.Unmarshal(sp["updatedAt"], &at)
	return at
}

// copySpace is src without the parts that belong to its org.
func copySpace(src space) space {
	out := space{}
	for k, v := range src {
		out[k] = v
	}
	for _, k := range orgBoundSpaceKeys {
		delete(out, k)
	}
	return out
}

// refreshSpace takes the newer content of src and keeps dst's own org parts.
func refreshSpace(dst, src space) space {
	out := copySpace(src)
	for _, k := range orgBoundSpaceKeys {
		if v, ok := dst[k]; ok {
			out[k] = v
		}
	}
	return out
}

func readSeen(path string) map[string][]string {
	seen := map[string][]string{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &seen)
	}
	return seen
}

func writeSeen(path string, seen map[string][]string) error {
	data, err := json.MarshalIndent(seen, "", "  ")
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
