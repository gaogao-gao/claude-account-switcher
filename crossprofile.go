package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// On third-party (see thirdparty.go) the app keeps its chats in a profile
// of its own, Claude-3p. The switcher syncs the chat entries, and nothing
// else, between it and the accounts, writing only where the app does not run.

// thirdPartyApp is the data folder of the app's third-party profile.
func thirdPartyApp() desktop { return desktop{dir: filepath.Dir(thirdPartyConfigPath())} }

// chatFolder is the account and organization folder of the chat entry at path.
func chatFolder(path string) (account, org string) {
	dir := filepath.Dir(path)
	return filepath.Base(filepath.Dir(dir)), filepath.Base(dir)
}

// syncProfileChats brings the chats of every account folder under root and
// other, the other profile's chat list, into root/account/org, and merges
// their sidebar settings. It keeps its record of those in statePath, apart
// from the one between accounts. The chats in the scratch workspaces of
// otherDir, the other profile's data folder, stay there: it moves the copies
// root holds into trash, and leaves them out of the sync. Returns the
// entries written or moved.
func syncProfileChats(root, account, org, other, otherDir, prefer, trash, statePath string) (int, error) {
	theirs := func(cwd string) bool { return scratchOf(cwd, otherDir) != "" }
	d, derr := dropChats(root, trash, theirs)
	if d > 0 {
		log.Printf("Moved %d scratch chats of %s out of %s", d, otherDir, root)
	}
	// A copy of root's own scratch chat in other, from an earlier sync, says
	// nothing about it either: root's own entry is the chat.
	scratch := func(cwd string) bool { return scratchOf(cwd, otherDir, filepath.Dir(root)) != "" }
	n, err := syncSessionsExcept(root, account, org, trash, scratch, other)
	m, merr := mergeChatStateExcept(root, account, org, prefer, statePath, scratch, other)
	return d + n + m, errors.Join(derr, err, merr)
}

// The app keeps a chat started without a folder in one of its own under the
// profile's scratch workspaces, and its sidebar groups only the chats there
// as such: elsewhere each shows as a project of its own. Claude Code finds
// the conversation by that folder, so it cannot move either; such a chat
// belongs to its profile alone.

// scratchOf is the profile data folder, among dirs, whose scratch workspaces
// hold folder cwd; "" when none does.
func scratchOf(cwd string, dirs ...string) string {
	if cwd == "" {
		return ""
	}
	cwd = filepath.Clean(cwd)
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		root := filepath.Join(dir, "scratch-workspaces")
		if len(cwd) > len(root) && cwd[len(root)] == filepath.Separator && samePath(cwd[:len(root)], root) {
			return dir
		}
	}
	return ""
}

// dropChats moves the chat entries under root whose folder drop names into
// trash. One that cannot be moved does not stop the others. Returns how many
// it moved.
func dropChats(root, trash string, drop func(cwd string) bool) (int, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*", "*", "local_*.json"))
	if err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, p := range paths {
		e, err := readSessionIndex(p)
		if err != nil || !drop(e.cwd) {
			continue
		}
		if err := removeSession(p, trash); err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// profileMu keeps the syncs between profiles of the periodic check and of a
// switch apart.
var profileMu sync.Mutex

// syncInto brings the chats of profile from into to's folder account/org,
// and its pinned chats into to's (see syncPins), when the setting is on and
// the app does not run on to. label names to in the log. Returns the entries
// and pins written.
func (s *switcher) syncInto(to, from desktop, account, org, prefer, label string) (int, error) {
	s.mu.Lock()
	on := s.st.SyncThirdParty
	s.mu.Unlock()
	if !on || profileInUse(to.dir) {
		return 0, nil
	}
	profileMu.Lock()
	defer profileMu.Unlock()
	n, err := syncProfileChats(to.sessionsDir(), account, org, from.sessionsDir(), from.dir, prefer, s.trashDir(), filepath.Join(s.dir, "chat-state-3p.json"))
	if err != nil {
		log.Printf("chats only partly synced into %s: %v", label, err)
	}
	if n > 0 {
		log.Printf("%d chats synced into %s", n, label)
	}
	p, perr := syncPins(to.appConfigPath(), from.appConfigPath(), scratchChats(from.sessionsDir(), from.dir))
	if perr != nil {
		log.Printf("pinned chats not synced into %s: %v", label, perr)
	}
	if p > 0 {
		log.Printf("%d pins synced into %s", p, label)
	}
	return n + p, errors.Join(err, perr)
}

// syncIntoAccount brings the chats of the third-party profile into account a.
func (s *switcher) syncIntoAccount(official, third desktop, a *Account, prefer string) (int, error) {
	return s.syncInto(official, third, a.UUID, a.Org, prefer, a.label())
}

// syncIntoThirdParty brings the accounts' chats into the third-party chat
// folder that holds entry newest; there is none while newest is "".
func (s *switcher) syncIntoThirdParty(official, third desktop, newest, prefer string) (int, error) {
	if newest == "" {
		return 0, nil
	}
	account, org := chatFolder(newest)
	return s.syncInto(third, official, account, org, prefer, "the third-party profile")
}

// profilesPrint is what decides whether syncProfiles has anything to do.
type profilesPrint struct {
	official, third         chatList
	officialLive, thirdLive bool
	account                 string
}

// syncProfiles brings each profile's chats into the other one, except into
// the one the app runs on, once anything changed since they were last found
// in step. A switch syncs both itself.
func (s *switcher) syncProfiles(official, third desktop) {
	// A switch that starts meanwhile waits for this sync; see run.
	if s.busy.Load() || !s.switching.TryLock() {
		return
	}
	defer s.switching.Unlock()
	uuid, _ := official.activeAccount()
	s.mu.Lock()
	on, adding, a, last, item := s.st.SyncThirdParty, !s.addingAt.IsZero(), s.account(uuid), s.profilesSeen, s.thirdPartyItem
	s.mu.Unlock()
	if !on || adding {
		return
	}
	// Without a third-party profile there is nothing to sync with.
	if _, err := os.Stat(third.sessionsDir()); item == "" && err != nil {
		return
	}
	seen := profilesPrint{newestChat(official.sessionsDir()), newestChat(third.sessionsDir()), profileInUse(official.dir), profileInUse(third.dir), uuid}
	if seen == last {
		return
	}
	n, err := s.syncBoth(official, third, a, seen.third.newest)
	// A sync that wrote nothing found the profiles in step as seen.
	if err == nil && n == 0 {
		s.mu.Lock()
		s.profilesSeen = seen
		s.mu.Unlock()
	}
}

// syncBoth brings the chats of account a, the official profile's, and of
// the third-party chat folder that holds entry newest into each other, except
// into the profile the app runs on. Returns the entries written.
func (s *switcher) syncBoth(official, third desktop, a *Account, newest string) (int, error) {
	// Each side prefers the other's values: the app runs there, if anywhere.
	n, err := s.syncIntoThirdParty(official, third, newest, a.key())
	if a != nil {
		prefer := ""
		if newest != "" {
			account, org := chatFolder(newest)
			prefer = account + "/" + org
		}
		m, merr := s.syncIntoAccount(official, third, a, prefer)
		n, err = n+m, errors.Join(err, merr)
	}
	return n, err
}
