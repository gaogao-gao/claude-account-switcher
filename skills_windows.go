package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

// lockSkill denies the user deleting or writing the placeholder, and adding
// to, deleting from or deleting the folder. The app runs as the user, so it
// is denied too. The user keeps the right to change the permissions back.
func lockSkill(dir, mark string) error {
	sid, err := userSID()
	if err != nil {
		return err
	}
	if err := icacls(mark, "/deny", sid+":(DE,WD,AD,WEA,WA)"); err != nil {
		return err
	}
	return icacls(dir, "/deny", sid+":(DE,DC,WD,AD,WEA,WA)")
}

// unlockSkill removes the user's deny entries from the folder and the placeholder.
func unlockSkill(dir, mark string) error {
	sid, err := userSID()
	if err != nil {
		return err
	}
	if err := icacls(dir, "/remove:d", sid); err != nil {
		return err
	}
	if _, err := os.Stat(mark); err != nil {
		return nil
	}
	return icacls(mark, "/remove:d", sid)
}

// userSID names the user for icacls by SID, which holds for any user name.
var userSID = sync.OnceValues(func() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return "*" + u.User.Sid.String(), nil
})

func icacls(args ...string) error {
	out, err := output("icacls", args...)
	if err != nil {
		return fmt.Errorf("icacls %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return nil
}
