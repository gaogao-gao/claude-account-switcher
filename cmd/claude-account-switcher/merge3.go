package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"sort"
)

// threeWay settles a setting that each account keeps a copy of, by what each
// account held at the last sync: an account whose copy moved since is where
// the user changed it. Values are canonical JSON, "" where an account has
// none, so a deletion moves a value too.
type threeWay struct {
	// Agreed is each unit's value after the last sync.
	Agreed map[string]string `json:"agreed"`
	// Seen is what each account held then, by account and unit.
	Seen map[string]map[string]string `json:"seen"`
	// wiped are the accounts whose list looks reset in this sync, see
	// markWiped. Not saved.
	wiped map[string]bool
}

func readThreeWay(path string) *threeWay {
	w := &threeWay{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, w)
	}
	if w.Agreed == nil {
		w.Agreed = map[string]string{}
	}
	if w.Seen == nil {
		w.Seen = map[string]map[string]string{}
	}
	return w
}

func (w *threeWay) write(path string) error {
	data, err := json.Marshal(w)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// resolve returns the value unit takes now. cur holds every account's value;
// order names the accounts whose change counts first, the one the user just
// left leading. first gives the value of a unit the switcher has no record of.
// Every account's value is recorded as seen; the caller records the target's
// again once it wrote the result there.
func (w *threeWay) resolve(unit string, cur map[string]string, order []string, first func() string) string {
	keys := append([]string{}, order...)
	rest := make([]string, 0, len(cur))
	for k := range cur {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	keys = append(keys, rest...)

	truth, known := w.Agreed[unit]
	for _, k := range keys {
		v, has := cur[k]
		if !has || v == "" && w.wiped[k] {
			continue
		}
		if was, ok := w.Seen[k][unit]; ok && v != was {
			truth, known = v, true
			break
		}
	}
	if !known {
		truth = first()
	}
	w.Agreed[unit] = truth
	for k, v := range cur {
		if v != "" || !w.wiped[k] {
			w.saw(k, unit, v)
		}
	}
	return truth
}

func (w *threeWay) saw(account, unit, value string) {
	if w.Seen[account] == nil {
		w.Seen[account] = map[string]string{}
	}
	w.Seen[account][unit] = value
}

// looksWiped says whether an account's list looks reset rather than edited:
// it lost every one of the two or more units it held at the last sync. An
// app that started over on an empty file loses everything at once, and that
// must not spread; deletions short of that are the user's and do. has says
// whether the account holds a unit now.
func (w *threeWay) looksWiped(account string, has func(unit string) bool) bool {
	held := 0
	for u, v := range w.Seen[account] {
		if v == "" {
			continue
		}
		if has(u) {
			return false
		}
		held++
	}
	return held >= 2
}

// markWiped makes resolve ignore what account lacks in this sync: its list
// was reset, not edited. what names the list for the log.
func (w *threeWay) markWiped(account, what string) {
	if w.wiped == nil {
		w.wiped = map[string]bool{}
	}
	if !w.wiped[account] && len(w.Seen[account]) > 0 {
		log.Printf("%s of %s look reset, their absence is not taken as deleting them", what, account)
	}
	w.wiped[account] = true
}

// forget drops units no account has any more.
func (w *threeWay) forget(keep func(unit string) bool) {
	for u := range w.Agreed {
		if !keep(u) {
			delete(w.Agreed, u)
		}
	}
	for _, m := range w.Seen {
		for u := range m {
			if !keep(u) {
				delete(m, u)
			}
		}
	}
}

// compactJSON is raw in canonical form, "" for nothing or null.
func compactJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil || v == nil {
		return ""
	}
	out, _ := json.Marshal(v)
	return string(out)
}
