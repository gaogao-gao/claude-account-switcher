package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
)

// Scheduled tasks are kept per account in
// claude-code-sessions/<account>/<org>/scheduled-tasks.json. syncTasks
// carries tasks the user added, changed or deleted in one account to the
// target, by task id. Only one account's app runs at a time, so a task runs
// in whichever account is signed in.
func syncTasks(root, account, org, prefer, statePath string) (int, error) {
	target := account + "/" + org
	paths, err := filepath.Glob(filepath.Join(root, "*", "*", "scheduled-tasks.json"))
	if err != nil {
		return 0, err
	}
	files := map[string]*object{}
	tasks := map[string]map[string]json.RawMessage{} // account/org → id → task
	var order []string                               // task ids, target's first
	seenID := map[string]bool{}
	addID := func(id string) {
		if !seenID[id] {
			seenID[id] = true
			order = append(order, id)
		}
	}
	targetPath := filepath.Join(root, account, org, "scheduled-tasks.json")
	w := readThreeWay(statePath)
	// An account whose file is missing or holds no task list says nothing
	// about its tasks; reading that as "all deleted" would spread. A target
	// in that state is left to the app.
	var loose []json.RawMessage // the target's entries without an id, kept as they are
	for _, p := range append([]string{targetPath}, paths...) {
		dir := filepath.Dir(p)
		key := filepath.Base(filepath.Dir(dir)) + "/" + filepath.Base(dir)
		if _, ok := files[key]; ok {
			continue
		}
		o, err := readObject(p)
		var list []json.RawMessage
		if err == nil {
			raw, ok := o.get("scheduledTasks")
			if !ok || json.Unmarshal(raw, &list) != nil {
				err = errors.New("no task list")
			}
		}
		if err != nil {
			if key == target {
				return 0, nil
			}
			continue
		}
		files[key] = o
		tasks[key] = map[string]json.RawMessage{}
		for _, t := range list {
			var head struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(t, &head) != nil || head.ID == "" {
				if key == target {
					loose = append(loose, t)
				}
				continue
			}
			tasks[key][head.ID] = t
			addID(head.ID)
		}
		if w.looksWiped(key, func(id string) bool { _, ok := tasks[key][id]; return ok }) {
			w.markWiped(key, "scheduled tasks")
		}
	}
	for id := range w.Agreed {
		addID(id)
	}

	changed := 0
	next := map[string]json.RawMessage{}
	for _, id := range order {
		vals := map[string]string{}
		for key, m := range tasks {
			vals[key] = compactJSON(m[id])
		}
		truth := w.resolve(id, vals, []string{prefer}, func() string {
			// A task one account has and the others never had is new.
			return firstOf(vals, prefer, target)
		})
		if truth != vals[target] {
			changed++
		}
		if truth != "" {
			next[id] = json.RawMessage(truth)
		}
	}
	w.forget(func(id string) bool { return w.Agreed[id] != "" || anyHas(tasks, id) })
	if changed == 0 {
		return 0, w.write(statePath)
	}

	list := make([]json.RawMessage, 0, len(next)+len(loose))
	list = append(list, loose...)
	for _, id := range order {
		if t, ok := next[id]; ok {
			list = append(list, t)
		}
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return 0, err
	}
	o := files[target]
	o.set("scheduledTasks", raw)
	if err := backupSynced(targetPath, statePath, "tasks", account); err != nil {
		return 0, err
	}
	if err := o.write(targetPath); err != nil {
		return 0, err
	}
	for id, t := range next {
		w.saw(target, id, compactJSON(t))
	}
	for _, id := range order {
		if _, ok := next[id]; !ok {
			w.saw(target, id, "")
		}
	}
	return changed, w.write(statePath)
}

func anyHas(tasks map[string]map[string]json.RawMessage, id string) bool {
	for _, m := range tasks {
		if _, ok := m[id]; ok {
			return true
		}
	}
	return false
}
