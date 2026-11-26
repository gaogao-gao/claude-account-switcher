package main

// lockSkill sets the user immutable flag on the placeholder and the folder,
// so nothing in it can be added, renamed or deleted.
func lockSkill(dir, mark string) error { return run("chflags", "uchg", mark, dir) }

// unlockSkill clears the flags lockSkill set.
func unlockSkill(dir, mark string) error { return run("chflags", "nouchg", dir, mark) }
