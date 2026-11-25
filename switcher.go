package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// problemSignedOut is Account.Problem for an account whose saved sign-in
// stopped working; the menu says it in the user's language.
const problemSignedOut = "signed-out"

// Account is a Claude account the switcher has seen signed in.
type Account struct {
	UUID    string `json:"uuid"`
	Org     string `json:"org"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Plan    string `json:"plan"`
	Usage   *Usage `json:"usage,omitempty"`
	Problem string `json:"problem,omitempty"`
}

// key names the account's chat folder, account/org; "" for nil.
func (a *Account) key() string {
	if a == nil {
		return ""
	}
	return a.UUID + "/" + a.Org
}

func (a *Account) label() string {
	name := a.Name
	if name == "" {
		name = a.Email
	}
	if a.Plan != "" {
		name += " (" + a.Plan + ")"
	}
	return name
}

// State is what the switcher remembers between runs.
type State struct {
	Accounts   []*Account `json:"accounts"`
	AutoSwitch bool       `json:"autoSwitch"`
	Threshold  float64    `json:"threshold"`
	// SpendWeekly moves to an account whose week is about to reset mostly unused.
	SpendWeekly bool      `json:"spendWeekly"`
	LastSwitch  time.Time `json:"lastSwitch"`
	HandSwitch  time.Time `json:"handSwitch"`
	// Why the last switch happened, and the account it left.
	LastCause switchCause `json:"lastCause,omitempty"`
	LastFrom  string      `json:"lastFrom,omitempty"`
	// Spend weekly moved to this account and keeps coming back to it until
	// its week resets, even once more than half of it is used.
	Spending   string    `json:"spending,omitempty"`
	SpendUntil time.Time `json:"spendUntil,omitempty"`
	AppExe     string    `json:"appExe,omitempty"`
	// LockSkills keeps the built-in skills named in LockedSkills locked in
	// every account; see skills.go.
	LockSkills   bool     `json:"lockSkills"`
	LockedSkills []string `json:"lockedSkills"`
	// SyncThirdParty syncs the chats with the app's third-party profile; see
	// crossprofile.go.
	SyncThirdParty bool `json:"syncThirdParty"`
}

// switchCause says why a switch happened.
type switchCause int

const (
	byHand switchCause = iota
	nearlyFull
	spendWeekly
)

const (
	defaultThreshold = 96
	// An auto switch only goes to an account with this much room below the threshold.
	autoHeadroom = 10
	// No second auto switch this soon after the last switch.
	autoCooldown = 10 * time.Minute
	// After an auto switch failed, none is tried for this long.
	autoFailPause = time.Minute
	// Spend weekly: an account whose week resets this soon with at most this
	// much of it used is worth moving to before the rest is lost.
	expiringWithin    = 24 * time.Hour
	expiringMaxWeekly = 50
	// A switch made by hand pauses spend weekly this long.
	handPause = 5 * time.Hour
	// Spend weekly is never urgent, so it waits until no chat has been
	// active this long instead of cutting off a reply.
	idleFor = 15 * time.Minute
	// The menu says why the last auto switch happened for this long.
	noteFor = 12 * time.Hour
	// Claude stays closed this long while it asks which stopped chats go on.
	resumeWait = 15 * time.Second

	// The usage endpoint rate-limits per account, and Claude polls it too.
	// The active account is checked more often once it gets close.
	activeEvery = 2 * time.Minute
	hotEvery    = time.Minute
	hotAt       = 85
	idleEvery   = 10 * time.Minute
	// Auto switch only trusts numbers this fresh.
	activeFresh = 5 * time.Minute
	idleFresh   = idleEvery + 5*time.Minute
	maxBackoff  = 30 * time.Minute
)

type switcher struct {
	dir      string
	mu       sync.Mutex
	st       State
	active   string
	busy     atomic.Bool
	status   string
	addingAt time.Time
	onChange func()
	// Held for a switch and for the background sync between profiles
	// (syncProfiles), which both write the chats.
	switching sync.Mutex
	// After a 429, an account is not asked again before its backoff ends.
	backoff map[string]time.Time
	strikes map[string]int
	// When the last auto switch failed.
	autoFailed time.Time
	// The app runs on its third-party profile; see onThirdParty.
	thirdParty bool
	// The menu entry of the third-party provider, "" for none.
	thirdPartyItem string
	// The chat lists syncProfiles last found in step.
	profilesSeen profilesPrint
}

func newSwitcher(dir string) *switcher {
	s := &switcher{dir: dir, st: State{Threshold: defaultThreshold, LockSkills: true, LockedSkills: slices.Clone(defaultLockedSkills), SyncThirdParty: true}, onChange: func() {},
		backoff: map[string]time.Time{}, strikes: map[string]int{}}
	if data, err := os.ReadFile(s.statePath()); err == nil {
		_ = json.Unmarshal(data, &s.st)
	}
	if s.st.Threshold <= 0 {
		s.st.Threshold = defaultThreshold
	}
	return s
}

func (s *switcher) statePath() string             { return filepath.Join(s.dir, "state.json") }
func (s *switcher) profileDir(uuid string) string { return filepath.Join(s.dir, "accounts", uuid) }
func (s *switcher) backupsDir() string            { return filepath.Join(s.dir, "backups") }
func (s *switcher) trashDir() string              { return filepath.Join(s.dir, "deleted-chats") }
func (s *switcher) switchMarker() string          { return filepath.Join(s.dir, "switch-in-progress") }

// app looks the data folder up on every use, because Claude can move it in an
// update while the switcher keeps running.
func (s *switcher) app() desktop { return claudeDesktop() }

// saveState writes state.json. Callers hold s.mu.
func (s *switcher) saveState() {
	data, _ := json.MarshalIndent(s.st, "", "  ")
	if err := writeFileAtomic(s.statePath(), data); err != nil {
		log.Printf("save state: %v", err)
	}
}

func (s *switcher) setStatus(msg string) {
	s.mu.Lock()
	s.status = msg
	s.mu.Unlock()
	if msg != "" {
		log.Print(msg)
	}
	s.onChange()
}

func (s *switcher) account(uuid string) *Account {
	for _, a := range s.st.Accounts {
		if a.UUID == uuid {
			return a
		}
	}
	return nil
}

// tokenFor returns an access token for an account: from the app's live config
// when it is signed in, otherwise from its saved profile.
func (s *switcher) tokenFor(uuid, active string) (string, error) {
	path := filepath.Join(s.profileDir(uuid), "config.json")
	if uuid == active {
		path = s.app().configPath()
	}
	cache, err := tokenCacheOf(path)
	if err != nil {
		return "", err
	}
	return accessToken(cache, uuid)
}

// refresh notices sign-ins, keeps usage current, syncs the chats with the
// other profile and runs the auto switch.
func (s *switcher) refresh(ctx context.Context) {
	what, do := s.check(ctx)
	s.syncProfiles(s.app(), thirdPartyApp())
	if do != nil {
		s.run(what, do)
	}
}

// check fetches the usage that is due and saves it. It never restarts Claude,
// so -status can use it; the switch or import it finds due is returned instead.
func (s *switcher) check(ctx context.Context) (string, func() error) {
	if s.busy.Load() {
		return "", nil
	}
	// No account is in use then: nothing is checked, marked signed out or
	// switched until the app is back on an account.
	third, item := onThirdParty()
	s.mu.Lock()
	changed := third != s.thirdParty
	shown := changed || item != s.thirdPartyItem
	s.thirdParty, s.thirdPartyItem = third, item
	if third {
		s.active = ""
	}
	s.mu.Unlock()
	if changed {
		log.Printf("Claude on its third-party profile: %v", third)
	}
	if shown {
		s.onChange()
	}
	if third {
		return "", nil
	}
	d := s.app()
	active, err := d.activeAccount()
	if err != nil {
		log.Printf("read app config: %v", err)
	}
	// The app renews its tokens, and a sign-out and in replaces them; the
	// profile only got them at the last switch away.
	if active != "" {
		if changed, err := refreshProfileAuth(d, s.profileDir(active), active); err != nil {
			log.Printf("save renewed sign-in: %v", err)
		} else if changed {
			log.Printf("saved the renewed sign-in of %s", active[:min(8, len(active))])
		}
	}
	_, exe := desktopProcesses()

	s.mu.Lock()
	s.active = active
	if exe != "" {
		s.st.AppExe = exe
	}
	type job struct {
		uuid string
		new  bool
	}
	now := time.Now()
	var jobs []job
	if active != "" && s.account(active) == nil && now.After(s.backoff[active]) {
		jobs = append(jobs, job{active, true})
	}
	for _, a := range s.st.Accounts {
		every := idleEvery
		if a.UUID == active {
			every = activeEvery
			if a.Usage != nil && a.Usage.current(now).peak() >= hotAt {
				every = hotEvery
			}
		}
		due := a.Usage == nil || now.Sub(a.Usage.CheckedAt) >= every-5*time.Second
		if due && now.After(s.backoff[a.UUID]) {
			jobs = append(jobs, job{a.UUID, false})
		}
	}
	s.mu.Unlock()

	added := false
	for _, j := range jobs {
		token, err := s.tokenFor(j.uuid, active)
		var p profile
		var u *Usage
		if err == nil && j.new {
			p, err = fetchProfile(ctx, token)
		}
		if err == nil {
			u, err = fetchUsage(ctx, token)
		}

		s.mu.Lock()
		var limited *rateLimited
		if errors.As(err, &limited) {
			s.strikes[j.uuid]++
			// The server's Retry-After is a floor, not a reason to stop backing off.
			wait := max(limited.after, min(activeEvery<<min(s.strikes[j.uuid]-1, 5), maxBackoff))
			s.backoff[j.uuid] = time.Now().Add(wait)
			log.Printf("usage %s: rate limited, next try in %s", j.uuid[:min(8, len(j.uuid))], wait)
		} else if err == nil {
			delete(s.strikes, j.uuid)
		}
		a := s.account(j.uuid)
		if j.new && err == nil {
			a = &Account{UUID: j.uuid, Org: p.Organization.UUID, Name: p.Account.DisplayName, Email: p.Account.Email, Plan: p.plan()}
			s.st.Accounts = append(s.st.Accounts, a)
			added = true
			log.Printf("added account %s", a.label())
		}
		if a != nil {
			switch {
			case err == nil:
				a.Usage, a.Problem = u, ""
			case errors.Is(err, errSignedOut):
				a.Problem = problemSignedOut
			case limited != nil:
				// Logged above; the last numbers stay until the next try.
			default:
				log.Printf("usage %s: %v", a.label(), err)
			}
		}
		s.mu.Unlock()
	}

	lastChat := newestChat(d.sessionsDir()).at
	s.mu.Lock()
	adding := !s.addingAt.IsZero()
	if added && adding {
		s.addingAt = time.Time{}
	} else if adding && time.Since(s.addingAt) > 15*time.Minute {
		s.addingAt, s.status = time.Time{}, ""
	}
	s.saveState()
	target, cause := s.autoTarget(desktopRunning(), lastChat, time.Now())
	s.mu.Unlock()
	s.onChange()

	switch {
	case added && adding:
		// A fresh sign-in: restart once so the chats of the other accounts show up.
		return "Importing chats", s.importChats
	case target != nil:
		return "Auto switch", func() error { return s.switchTo(target.UUID, cause) }
	}
	return "", nil
}

// autoTarget picks the account to switch to and says why: the active one is
// nearly full, or another one's week is about to reset mostly unused.
// lastChat is when a chat was last active. Callers hold s.mu.
func (s *switcher) autoTarget(running bool, lastChat, now time.Time) (*Account, switchCause) {
	cur := s.account(s.active)
	if !running || cur == nil || cur.Usage == nil || now.Sub(cur.Usage.CheckedAt) > activeFresh || now.Sub(s.st.LastSwitch) < autoCooldown || now.Sub(s.autoFailed) < autoFailPause {
		return nil, byHand
	}
	if s.st.AutoSwitch && cur.Usage.current(now).peak() >= s.st.Threshold {
		var best *Account
		for _, a := range s.st.Accounts {
			if s.usable(a, cur, now) && (best == nil || a.Usage.current(now).peak() < best.Usage.current(now).peak()) {
				best = a
			}
		}
		if best != nil {
			return best, nearlyFull
		}
	}
	// The active account keeps going while its own week is the one running out.
	if s.st.SpendWeekly && now.Sub(s.st.HandSwitch) >= handPause && now.Sub(lastChat) >= idleFor && !s.spendable(cur, now) {
		var best *Account
		for _, a := range s.st.Accounts {
			if s.usable(a, cur, now) && s.spendable(a, now) && (best == nil || a.Usage.WeeklyReset.Before(best.Usage.WeeklyReset)) {
				best = a
			}
		}
		if best != nil {
			return best, spendWeekly
		}
	}
	return nil, byHand
}

// usable says whether an auto switch may go to a: fresh numbers and room
// well below the threshold. Callers hold s.mu.
func (s *switcher) usable(a, cur *Account, now time.Time) bool {
	return a != cur && a.Usage != nil && a.Problem == "" && now.Sub(a.Usage.CheckedAt) <= idleFresh &&
		a.Usage.current(now).peak() < s.st.Threshold-autoHeadroom
}

// spendable says whether a's week should be used up first: it resets soon
// with much of it unused, or spend weekly already moved there and that week
// has not reset yet. a has usage. Callers hold s.mu.
func (s *switcher) spendable(a *Account, now time.Time) bool {
	if a.UUID == s.st.Spending && now.Before(s.st.SpendUntil) {
		return true
	}
	u := a.Usage.current(now)
	left := u.WeeklyReset.Sub(now)
	return !u.WeeklyReset.IsZero() && left > 0 && left <= expiringWithin && u.Weekly <= expiringMaxWeekly
}

// run does one app restart at a time and reports the outcome in the menu.
func (s *switcher) run(what string, f func() error) {
	if !s.busy.CompareAndSwap(false, true) {
		return
	}
	// A background sync between profiles finishes first.
	s.switching.Lock()
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("internal error: %v", r)
				log.Printf("panic in %s: %v\n%s", what, r, debug.Stack())
			}
		}()
		return f()
	}()
	s.switching.Unlock()
	s.busy.Store(false)
	if err != nil && what == "Auto switch" {
		// A failing auto switch would otherwise close Claude again every minute.
		s.mu.Lock()
		s.autoFailed = time.Now()
		s.mu.Unlock()
		log.Printf("auto switch failed, no auto switch for %s: %v", autoFailPause, err)
	}
	if err != nil {
		s.setStatus(fmt.Sprintf("%s failed: %v", what, err))
		return
	}
	s.mu.Lock()
	// Adding waits for the sign-in.
	if s.addingAt.IsZero() {
		s.status = ""
	}
	s.mu.Unlock()
	s.onChange()
	s.refresh(context.Background())
}

// restart closes the app, runs change on its files, asks which of the chats
// the quit stopped go on and starts the app again. If change fails, the app
// files are put back as they were.
func (s *switcher) restart(change func(active string) error) error {
	quitAt := time.Now()
	running := runningChats(projectsDir(), quitAt)
	s.mu.Lock()
	exe := s.st.AppExe
	s.mu.Unlock()
	if err := quitDesktop(); err != nil {
		// Part of the app may have gone; never leave it closed.
		if !desktopRunning() {
			s.startApp(exe)
		}
		return err
	}
	defer s.startApp(exe)
	bk, err := backup(s.app(), s.backupsDir())
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	// The marker names the backup while the sign-in may be half changed, so
	// recoverSwitch can undo a switch that a crash cut off on the next start.
	if err := os.WriteFile(s.switchMarker(), []byte(bk), 0o600); err != nil {
		return fmt.Errorf("mark switch: %w", err)
	}
	settled := false
	defer func() {
		if !settled {
			s.recoverSwitch() // change panicked
		}
	}()
	active, err := s.app().activeAccount()
	if err == nil {
		err = change(active)
	}
	if err != nil {
		s.recoverSwitch()
	} else if rerr := os.Remove(s.switchMarker()); rerr != nil {
		log.Printf("remove switch marker: %v", rerr)
	}
	settled = true
	if err == nil {
		// A new sign-in brings its own copy of the skills. In the background,
		// so Claude is not kept closed for it.
		go s.lockSkills()
	}
	s.askResume(quitAt, running)
	return err
}

// startApp starts the app from exe, saying so in the menu if it cannot.
func (s *switcher) startApp(exe string) {
	if err := startDesktop(exe); err != nil {
		s.setStatus(tr.CouldNotStart + err.Error())
	}
}

// recoverSwitch puts back the backup of a switch that did not finish, which
// the marker from restart names: one that failed, or one a crash stopped
// before the switcher started again. A running Claude would write over its
// files, so then the backup only goes in the log.
func (s *switcher) recoverSwitch() {
	data, err := os.ReadFile(s.switchMarker())
	if err != nil {
		return
	}
	bk := string(data)
	switch {
	case desktopRunning():
		log.Printf("a switch did not finish and Claude is running, its backup is %s", bk)
	case restoreBackup(s.app(), bk) != nil:
		log.Printf("a switch did not finish, restoring backup %s failed", bk)
		return
	default:
		log.Printf("a switch did not finish, restored backup %s", bk)
	}
	if err := os.Remove(s.switchMarker()); err != nil {
		log.Printf("remove switch marker: %v", err)
	}
}

// askResume lets the user pick which of the chats the quit stopped mid-turn
// the app continues on its start. Nobody answering means all of them, the
// same as a restart without the switcher.
func (s *switcher) askResume(quitAt time.Time, running map[string]bool) {
	root, account, org, label := s.startFolder()
	if account == "" {
		return
	}
	cut := takeCutChats(root, account, org, quitAt, running)
	if len(cut) == 0 {
		return
	}
	keep := chooseResume(label, cut, resumeWait)
	n := 0
	for i, c := range cut {
		if keep[i] {
			n++
			continue
		}
		if err := clearCutMark(c.path); err != nil {
			log.Printf("clear quit mark of %s: %v", c.path, err)
		}
	}
	if label == "" {
		label = "third-party"
	}
	log.Printf("%d of %d stopped chats continue in %s", n, len(cut), label)
}

// startFolder is the chat folder the app opens on its next start: the
// signed-in account's, or on the third-party profile the one with its newest
// chat, which has no label. account is "" while it is not known.
func (s *switcher) startFolder() (root, account, org, label string) {
	if on, _ := onThirdParty(); on {
		root = thirdPartyApp().sessionsDir()
		if newest := newestChat(root).newest; newest != "" {
			account, org = chatFolder(newest)
		}
		return root, account, org, ""
	}
	a := s.signedIn()
	if a == nil {
		return "", "", "", ""
	}
	return s.app().sessionsDir(), a.UUID, a.Org, a.label()
}

// signedIn is the known account the official profile is signed in to, nil
// if there is none.
func (s *switcher) signedIn() *Account {
	uuid, err := s.app().activeAccount()
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account(uuid)
}

// saveActive keeps the signed-in account's latest login before it is replaced.
func (s *switcher) saveActive(active string) error {
	if active == "" {
		return nil
	}
	return saveProfile(s.app(), s.profileDir(active))
}

// switchTo signs the app in as uuid, for the reason in cause.
func (s *switcher) switchTo(uuid string, cause switchCause) error {
	target, err := s.switchable(uuid)
	if err != nil {
		return err
	}
	s.mu.Lock()
	from := s.active
	s.mu.Unlock()
	switch cause {
	case nearlyFull:
		log.Printf("auto switch to %s: the active account is nearly full", target.label())
	case spendWeekly:
		log.Printf("auto switch to %s: its weekly usage resets soon", target.label())
	}
	s.setStatus(fmt.Sprintf(tr.Switching, target.label()))
	if err := s.restart(official(s.signInAs(target))); err != nil {
		return err
	}
	s.noteSwitch(target, cause, from)
	return nil
}

// signInAs is the change that signs the closed app in as target.
func (s *switcher) signInAs(target *Account) func(active string) error {
	return func(active string) error {
		if active == target.UUID {
			return nil
		}
		if err := s.saveActive(active); err != nil {
			return fmt.Errorf("save current sign-in: %w", err)
		}
		if err := restoreProfile(s.app(), s.profileDir(target.UUID)); err != nil {
			return fmt.Errorf("restore sign-in: %w", err)
		}
		s.importChatsInto(target, active)
		log.Printf("switched to %s", target.label())
		return nil
	}
}

// noteSwitch records a switch from account from to target, made for cause.
func (s *switcher) noteSwitch(target *Account, cause switchCause, from string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.LastSwitch = time.Now()
	s.st.LastCause, s.st.LastFrom = cause, from
	switch cause {
	case byHand:
		// The user's pick ends spending another account's week.
		s.st.HandSwitch = s.st.LastSwitch
		s.st.Spending, s.st.SpendUntil = "", time.Time{}
	case spendWeekly:
		if target.Usage != nil {
			s.st.Spending, s.st.SpendUntil = target.UUID, target.Usage.WeeklyReset
		}
	}
	s.active = target.UUID
	s.saveState()
}

// switchable returns the account uuid, or why the app cannot switch to it.
func (s *switcher) switchable(uuid string) (*Account, error) {
	s.mu.Lock()
	target := s.account(uuid)
	s.mu.Unlock()
	if target == nil {
		return nil, errors.New("unknown account")
	}
	// The signed-in account needs no saved sign-in; the app's config is
	// asked, since the switcher forgets it on the third-party profile.
	active, _ := s.app().activeAccount()
	if _, err := os.Stat(filepath.Join(s.profileDir(uuid), "config.json")); err != nil && uuid != active {
		return nil, fmt.Errorf("no saved sign-in for %s, use Add account", target.label())
	}
	return target, nil
}

// addAccount keeps the current sign-in and opens the app on its login screen.
func (s *switcher) addAccount() error {
	s.setStatus(tr.SignInOther)
	err := s.restart(official(func(active string) error {
		if err := s.saveActive(active); err != nil {
			return err
		}
		return clearSignIn(s.app())
	}))
	if err == nil {
		s.mu.Lock()
		s.addingAt = time.Now()
		s.mu.Unlock()
	}
	return err
}

// importChats makes the chats of all accounts visible in the signed-in one.
func (s *switcher) importChats() error {
	// Restarting Claude only to find nobody signed in would be for nothing.
	if s.signedIn() == nil {
		return errors.New(tr.NoSignedIn)
	}
	s.setStatus(tr.Importing)
	return s.restart(func(active string) error {
		s.mu.Lock()
		a := s.account(active)
		s.mu.Unlock()
		if a == nil {
			return errors.New(tr.NoSignedIn)
		}
		s.importChatsInto(a, a.UUID)
		return nil
	})
}

// importChatsInto brings the chats and projects of all accounts into a, and
// what the user set in any of them: archive, pin and title of each chat,
// scheduled tasks, sidebar sections and groups, and permission modes per
// folder. from is the account the user was working in. Then a and the
// third-party profile take each other's chats; see crossprofile.go. The
// sign-in is already in place, so a failure here only costs some of that and
// is logged; the switch goes on.
func (s *switcher) importChatsInto(a *Account, from string) {
	root := s.app().sessionsDir()
	n, err := syncSessions(root, a.UUID, a.Org, s.trashDir())
	if err != nil {
		log.Printf("chats only partly imported into %s: %v", a.label(), err)
	}
	pruneDirs(s.trashDir(), keepBackups, keepBackupsFor)
	log.Printf("%d chat changes in %s", n, a.label())
	target := a.key()
	s.mu.Lock()
	prefer := s.account(from).key()
	s.mu.Unlock()
	state := func(name string) string { return filepath.Join(s.dir, name) }
	if n, err := mergeChatState(root, a.UUID, a.Org, prefer, state("chat-state.json")); err != nil {
		log.Printf("chat settings not merged into %s: %v", a.label(), err)
	} else {
		log.Printf("%d chat settings updated in %s", n, a.label())
	}
	if n, err := syncTasks(root, a.UUID, a.Org, prefer, state("tasks-state.json")); err != nil {
		log.Printf("scheduled tasks not synced into %s: %v", a.label(), err)
	} else if n > 0 {
		log.Printf("%d scheduled tasks updated in %s", n, a.label())
	}
	scopes := []string{target}
	if dirs, err := filepath.Glob(filepath.Join(root, "*", "*")); err == nil {
		for _, d := range dirs {
			if info, err := os.Stat(d); err == nil && info.IsDir() {
				if sc := filepath.Base(filepath.Dir(d)) + "/" + filepath.Base(d); sc != target {
					scopes = append(scopes, sc)
				}
			}
		}
	}
	if n, err := syncLocalStorage(s.app().localStorageDir(), scopes, target, prefer, state("local-storage-state.json"), state("local-storage-backups")); err != nil {
		log.Printf("sidebar settings not synced into %s: %v", a.label(), err)
	} else {
		log.Printf("%d sidebar settings updated in %s", n, a.label())
	}
	s.importProjects(a)
	// Claude is closed: both profiles can take the other's chats.
	official, third := s.app(), thirdPartyApp()
	s.syncIntoAccount(official, third, a, prefer)
	s.syncIntoThirdParty(official, third, newestChat(third.sessionsDir()).newest, prefer)
}

// importProjects copies the projects of all accounts into a. A failure only
// costs the project grouping, so it is logged and the switch goes on.
func (s *switcher) importProjects(a *Account) {
	n, err := syncSpaces(s.app().spacesDir(), a.UUID, a.Org, filepath.Join(s.dir, "spaces-seen.json"))
	if err != nil {
		log.Printf("projects not imported into %s: %v", a.label(), err)
		return
	}
	log.Printf("%d projects imported into %s", n, a.label())
}

// setAutoSwitch turns the auto switch off, or on at threshold.
func (s *switcher) setAutoSwitch(on bool, threshold float64) {
	s.mu.Lock()
	s.st.AutoSwitch = on
	if on {
		s.st.Threshold = threshold
	}
	s.saveState()
	s.mu.Unlock()
	s.onChange()
}

func (s *switcher) setSpendWeekly(on bool) {
	s.mu.Lock()
	s.st.SpendWeekly = on
	s.saveState()
	s.mu.Unlock()
	s.onChange()
}

func (s *switcher) setSyncThirdParty(on bool) {
	s.mu.Lock()
	s.st.SyncThirdParty = on
	s.saveState()
	s.mu.Unlock()
	s.onChange()
}

// snapshot is a copy of what the menu shows.
type snapshot struct {
	Accounts    []Account
	Active      string
	AutoSwitch  bool
	SpendWeekly bool
	Threshold   float64
	Status      string
	Note        string
	Busy        bool
	ThirdParty  bool
	// ThirdPartyItem is the menu entry of the third-party provider, "" for none.
	ThirdPartyItem string
	LockSkills     bool
	SyncChats      bool
}

func (s *switcher) snapshot() snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := snapshot{Active: s.active, AutoSwitch: s.st.AutoSwitch, SpendWeekly: s.st.SpendWeekly, Threshold: s.st.Threshold,
		Status: s.status, Note: s.autoNote(time.Now()), Busy: s.busy.Load(), ThirdParty: s.thirdParty, LockSkills: s.st.LockSkills,
		SyncChats: s.st.SyncThirdParty, ThirdPartyItem: s.thirdPartyItem}
	for _, a := range s.st.Accounts {
		snap.Accounts = append(snap.Accounts, *a)
	}
	return snap
}

// autoNote says why the last switch happened when the switcher made it on
// its own, for a while afterwards. Callers hold s.mu.
func (s *switcher) autoNote(now time.Time) string {
	if now.Sub(s.st.LastSwitch) > noteFor {
		return ""
	}
	at := resetText(s.st.LastSwitch, now)
	switch s.st.LastCause {
	case nearlyFull:
		from := tr.LastAccount
		if a := s.account(s.st.LastFrom); a != nil {
			from = a.label()
		}
		return fmt.Sprintf(tr.NoteFull, at, from)
	case spendWeekly:
		return fmt.Sprintf(tr.NoteSpend, at, resetText(s.st.SpendUntil, now))
	}
	return ""
}

func (s *switcher) adding() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.addingAt.IsZero()
}
