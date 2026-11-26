package main

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// lockLS opens the LOCK file of the LevelDB in dir without sharing it. That
// fails while Claude has the database open, and keeps Claude from opening it
// until unlock.
func lockLS(dir string) (unlock func(), err error) {
	p, err := windows.UTF16PtrFromString(filepath.Join(dir, "LOCK"))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return func() { windows.CloseHandle(h) }, nil
}

// profileInUse reports whether Chromium runs on the profile in dir: it
// holds the lockfile there open while it does.
func profileInUse(dir string) bool { return heldOpen(filepath.Join(dir, "lockfile")) }

// heldOpen reports whether a process holds the file at path open. A missing
// file is not held.
func heldOpen(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	// No sharing: this fails while anyone else still has the file open.
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(err, windows.ERROR_PATH_NOT_FOUND)
	}
	windows.CloseHandle(h)
	return false
}
