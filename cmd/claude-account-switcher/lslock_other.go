//go:build !windows

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// lockLS takes the fcntl lock on the LOCK file of the LevelDB in dir, the
// lock Chromium holds while it has the database open.
func lockLS(dir string) (unlock func(), err error) {
	f, err := os.OpenFile(filepath.Join(dir, "LOCK"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	lk := unix.Flock_t{Type: unix.F_WRLCK, Whence: io.SeekStart}
	if err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, &lk); err != nil {
		f.Close()
		return nil, err
	}
	// Closing the file drops the lock.
	return func() { f.Close() }, nil
}

// profileInUse reports whether Chromium runs on the profile in dir: it keeps
// a SingletonLock link there while it does. Only a missing link counts as
// free.
func profileInUse(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, "SingletonLock"))
	return !errors.Is(err, os.ErrNotExist)
}
