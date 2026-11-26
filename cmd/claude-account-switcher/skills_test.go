package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestLockSkillDirs(t *testing.T) {
	root, backups := t.TempDir(), t.TempDir()
	skill := func(acct, name string) string { return filepath.Join(root, "dev", acct, "skills", name) }
	write(t, filepath.Join(skill("a", "morning"), "SKILL.md"), "morning")
	write(t, filepath.Join(skill("a", "morning"), "refs", "x.md"), "x")
	write(t, filepath.Join(skill("a", "docx"), "SKILL.md"), "docx")
	write(t, filepath.Join(skill("b", "setup-claude"), skillLockMark), "by hand")
	write(t, filepath.Join(root, "dev", "a", "skills", "stray.txt"), "")

	var locked []string
	lock := func(dir, mark string) error {
		if filepath.Dir(mark) != dir {
			t.Fatalf("placeholder %s is not in %s", mark, dir)
		}
		locked = append(locked, dir)
		return nil
	}
	n, err := lockSkillDirs(root, backups, defaultLockedSkills, lock)
	if err != nil || n != 1 || len(locked) != 1 || locked[0] != skill("a", "morning") {
		t.Fatalf("locked %d %v, %v; want only a's morning", n, locked, err)
	}
	entries, _ := os.ReadDir(skill("a", "morning"))
	if len(entries) != 1 || entries[0].Name() != skillLockMark {
		t.Fatalf("morning holds %v, want only the placeholder", entries)
	}
	if read(t, filepath.Join(backups, "a", "morning", "refs", "x.md")) != "x" {
		t.Fatal("morning was not backed up")
	}
	if _, err := os.Stat(filepath.Join(backups, "a", "morning.partial")); !os.IsNotExist(err) {
		t.Fatal("the partial backup was left behind")
	}
	if read(t, filepath.Join(skill("a", "docx"), "SKILL.md")) != "docx" {
		t.Fatal("a skill not on the list was touched")
	}
	if read(t, filepath.Join(skill("b", "setup-claude"), skillLockMark)) != "by hand" {
		t.Fatal("a folder locked by hand was locked again")
	}
	if n, _ := lockSkillDirs(root, backups, defaultLockedSkills, lock); n != 0 || len(locked) != 1 {
		t.Fatalf("second run locked %d more", n)
	}

	var unlocked []string
	n, err = unlockSkillDirs(root, defaultLockedSkills, func(dir, _ string) error {
		unlocked = append(unlocked, dir)
		return nil
	})
	sort.Strings(unlocked)
	if err != nil || n != 2 || unlocked[0] != skill("a", "morning") || unlocked[1] != skill("b", "setup-claude") {
		t.Fatalf("unlocked %d %v, %v", n, unlocked, err)
	}
	if skillLocked(skill("a", "morning")) || skillLocked(skill("b", "setup-claude")) {
		t.Fatal("placeholders left behind")
	}

	// A lock that fails leaves the folder to try again.
	write(t, filepath.Join(skill("c", "morning"), "SKILL.md"), "")
	if _, err := lockSkillDirs(root, backups, []string{"morning"}, func(string, string) error { return os.ErrPermission }); err == nil {
		t.Fatal("no error from a failed lock")
	}
	if skillLocked(skill("c", "morning")) {
		t.Fatal("a failed lock counts as locked")
	}
}
