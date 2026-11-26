package main

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// The app installs Anthropic's built-in skills for each account as a plugin
// in local-agent-mode-sessions/skills-plugin/<device>/<account>/skills/<name>,
// and no setting hides one. The one way that holds is to leave its folder
// empty but for a placeholder and lock both, so the app can neither refill,
// delete nor recreate it. The user unlocks them by turning the setting off;
// the app then fills the folders again itself.

// skillLockMark is the placeholder in a locked skill folder. A folder that
// holds it counts as locked, including ones the user locked by hand.
const skillLockMark = ".locked-by-owner"

const skillLockText = "Kept empty by Claude Account Switcher so the Claude app does not offer this built-in skill.\n" +
	"Turn off \"Keep extra built-in skills locked\" in its Settings menu to get it back.\n"

// defaultLockedSkills are the built-in skills kept locked unless state.json
// names others.
var defaultLockedSkills = []string{"setup-claude", "morning", "google-workspace", "consolidate-memory", "explain-usage"}

// skillDirs returns the folders of the skills named in names, for every
// device and account under root.
func skillDirs(root string, names []string) []string {
	want := map[string]bool{}
	for _, name := range names {
		want[name] = true
	}
	hits, _ := filepath.Glob(filepath.Join(root, "*", "*", "skills", "*"))
	var dirs []string
	for _, d := range hits {
		if info, err := os.Stat(d); err == nil && info.IsDir() && want[filepath.Base(d)] {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func skillLocked(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, skillLockMark))
	return err == nil
}

// lockSkillDirs locks the folders of the skills in names that are not locked
// yet: their files go to backupRoot/<account>/<name>, once, the folder is
// emptied, gets the placeholder, and lock closes both. A folder that fails
// is left for the next time. Returns how many it locked.
func lockSkillDirs(root, backupRoot string, names []string, lock func(dir, mark string) error) (int, error) {
	n := 0
	var errs []error
	for _, dir := range skillDirs(root, names) {
		if skillLocked(dir) {
			continue
		}
		account := filepath.Base(filepath.Dir(filepath.Dir(dir)))
		if err := lockSkillDir(dir, filepath.Join(backupRoot, account, filepath.Base(dir)), lock); err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

func lockSkillDir(dir, backup string, lock func(dir, mark string) error) error {
	if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
		// A copy cut off halfway must not pass for the backup.
		partial := backup + ".partial"
		if err := os.RemoveAll(partial); err != nil {
			return err
		}
		if err := copyTree(dir, partial); err != nil {
			return err
		}
		if err := os.MkdirAll(partial, 0o700); err != nil {
			return err
		}
		if err := os.Rename(partial, backup); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	mark := filepath.Join(dir, skillLockMark)
	if err := os.WriteFile(mark, []byte(skillLockText), 0o644); err != nil {
		return err
	}
	if err := lock(dir, mark); err != nil {
		// Without the placeholder the next run tries again.
		_ = os.Remove(mark)
		return err
	}
	return nil
}

// unlockSkillDirs undoes lockSkillDirs for the skills in names, leaving the
// folders empty for the app to fill. Returns how many it unlocked.
func unlockSkillDirs(root string, names []string, unlock func(dir, mark string) error) (int, error) {
	n := 0
	var errs []error
	for _, dir := range skillDirs(root, names) {
		if !skillLocked(dir) {
			continue
		}
		mark := filepath.Join(dir, skillLockMark)
		err := unlock(dir, mark)
		if err == nil {
			err = os.Remove(mark)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// copyTree copies the files under src to dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		return copyFile(path, filepath.Join(dst, rel))
	})
}

// skillsMu keeps a lock run at startup, after a switch and from the menu apart.
var skillsMu sync.Mutex

// lockSkills locks the unwanted built-in skills of every account when the
// setting is on.
func (s *switcher) lockSkills() {
	defer logPanic("lock skills")
	s.mu.Lock()
	on := s.st.LockSkills
	s.mu.Unlock()
	if on {
		s.applySkillLocks(true)
	}
}

// setLockSkills turns keeping the skills locked on or off, and locks or
// unlocks them right away.
func (s *switcher) setLockSkills(on bool) {
	defer logPanic("lock skills")
	s.mu.Lock()
	s.st.LockSkills = on
	s.saveState()
	s.mu.Unlock()
	s.onChange()
	s.applySkillLocks(on)
}

// applySkillLocks locks or unlocks the folders of the skills in the list.
// It only logs what fails: nothing else depends on it.
func (s *switcher) applySkillLocks(on bool) {
	s.mu.Lock()
	names := slices.Clone(s.st.LockedSkills)
	s.mu.Unlock()
	skillsMu.Lock()
	defer skillsMu.Unlock()
	verb, n, err := "locked", 0, error(nil)
	if !on {
		verb = "unlocked"
	}
	// The app's third-party profile keeps a copy of the skills of its own.
	for _, root := range []string{s.app().skillsDir(), thirdPartyApp().skillsDir()} {
		var k int
		var e error
		if on {
			k, e = lockSkillDirs(root, filepath.Join(s.dir, "skill-backups"), names, lockSkill)
		} else {
			k, e = unlockSkillDirs(root, names, unlockSkill)
		}
		n, err = n+k, errors.Join(err, e)
	}
	if n > 0 {
		log.Printf("%s %d built-in skill folders", verb, n)
	}
	if err != nil {
		log.Printf("not all built-in skill folders %s: %v", verb, err)
	}
}
