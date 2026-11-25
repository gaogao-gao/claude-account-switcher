package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// A profile is a saved sign-in: the account's config.json entries and the
// app's cookie store. Tokens stay encrypted by the app's own key, the
// switcher never writes them in plain text.

// saveProfile copies the current sign-in into dir. The app must be closed.
func saveProfile(d desktop, dir string) error {
	cfg, err := readObject(d.configPath())
	if err != nil {
		return err
	}
	saved := &object{vals: map[string]json.RawMessage{}}
	for _, k := range authKeys {
		if v, ok := cfg.get(k); ok {
			saved.set(k, v)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := saved.write(filepath.Join(dir, "config.json")); err != nil {
		return err
	}
	for _, f := range d.cookieFiles() {
		if err := copyFile(f, filepath.Join(dir, filepath.Base(f))); err != nil {
			return err
		}
	}
	return nil
}

// refreshProfileAuth copies the app's live sign-in into the saved profile of
// account in dir when the app renewed or replaced it, so a sign-out and
// sign-in inside the app does not leave the profile with dead tokens. The
// cookies stay as they are, the running app holds them. Only a profile that
// exists is updated, and only from a config.json signed in as account.
func refreshProfileAuth(d desktop, dir, account string) (bool, error) {
	cfg, err := readObject(d.configPath())
	if err != nil {
		return false, err
	}
	if _, ok := cfg.get("oauth:tokenCacheV2"); !ok || stringField(cfg, "lastKnownAccountUuid") != account {
		return false, nil
	}
	path := filepath.Join(dir, "config.json")
	saved, err := readObject(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	changed := false
	for _, k := range authKeys {
		v, ok := cfg.get(k)
		old, had := saved.get(k)
		switch {
		case ok && (!had || compactJSON(v) != compactJSON(old)):
			saved.set(k, v)
			changed = true
		case !ok && had:
			saved.del(k)
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	return true, saved.write(path)
}

// restoreProfile puts a saved sign-in back. The app must be closed.
func restoreProfile(d desktop, dir string) error {
	saved, err := readObject(filepath.Join(dir, "config.json"))
	if err != nil {
		return err
	}
	cfg, err := readObject(d.configPath())
	if err != nil {
		return err
	}
	for _, k := range authKeys {
		if v, ok := saved.get(k); ok {
			cfg.set(k, v)
		} else {
			cfg.del(k)
		}
	}
	for _, f := range d.cookieFiles() {
		if err := copyFile(filepath.Join(dir, filepath.Base(f)), f); err != nil {
			return err
		}
	}
	return cfg.write(d.configPath())
}

// clearSignIn removes the current sign-in locally so the app shows its login
// screen. It does not sign out, which would revoke the saved tokens.
func clearSignIn(d desktop) error {
	cfg, err := readObject(d.configPath())
	if err != nil {
		return err
	}
	for _, k := range authKeys {
		cfg.del(k)
	}
	for _, f := range d.cookieFiles() {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return cfg.write(d.configPath())
}

// Backups and deleted chats stay for keepBackupsFor, and the newest
// keepBackups of them stay whatever their age.
const (
	keepBackups    = 3
	keepBackupsFor = 7 * 24 * time.Hour
)

// backup copies config.json and the cookie store before a switch touches them.
func backup(d desktop, root string) (string, error) {
	dir := stampDir(root)
	files := append([]string{d.configPath()}, d.cookieFiles()...)
	for _, f := range files {
		if err := copyFile(f, filepath.Join(dir, filepath.Base(f))); err != nil {
			return "", err
		}
	}
	pruneDirs(root, keepBackups, keepBackupsFor)
	return dir, nil
}

// restoreBackup undoes a switch that failed halfway.
func restoreBackup(d desktop, dir string) error {
	files := append([]string{d.configPath()}, d.cookieFiles()...)
	for _, f := range files {
		if err := copyFile(filepath.Join(dir, filepath.Base(f)), f); err != nil {
			return err
		}
	}
	return nil
}

// backupFile copies path into a new folder under root before it is
// rewritten. A missing file needs no backup.
func backupFile(path, root string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := copyFile(path, filepath.Join(stampDir(root), filepath.Base(path))); err != nil {
		return err
	}
	pruneDirs(root, keepBackups, keepBackupsFor)
	return nil
}

// backupSynced backs up path before a sync rewrites it, into
// <name>-backups/<account> next to the sync's state file.
func backupSynced(path, statePath, name, account string) error {
	if err := backupFile(path, filepath.Join(filepath.Dir(statePath), name+"-backups", account)); err != nil {
		return fmt.Errorf("back up %s: %w", path, err)
	}
	return nil
}

// stampDir names a new folder under root after the current time. A second
// one within the same second gets a suffix rather than the first one's files.
func stampDir(root string) string {
	base := filepath.Join(root, time.Now().Format("20060102-150405"))
	dir := base
	for i := 2; ; i++ {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return dir
		}
		dir = fmt.Sprintf("%s-%d", base, i)
	}
}

// pruneDirs removes the folders in root, oldest first by name, that are
// older than keepFor, always keeping the newest keep.
func pruneDirs(root string, keep int, keepFor time.Duration) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	cutoff := time.Now().Add(-keepFor)
	for _, name := range names[:max(0, len(names)-keep)] {
		if at, ok := dirTime(name); ok && at.After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, name))
	}
}

// dirTime reads the time a folder from stampDir, or a day folder of deleted
// chats, was made from its name.
func dirTime(name string) (time.Time, bool) {
	for _, layout := range []string{"20060102-150405", "20060102"} {
		if len(name) >= len(layout) {
			if at, err := time.ParseInLocation(layout, name[:len(layout)], time.Local); err == nil {
				return at, true
			}
		}
	}
	return time.Time{}, false
}
