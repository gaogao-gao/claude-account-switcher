package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"

	"fyne.io/systray"
)

const maxAccounts = 8

var thresholds = []float64{90, 94, 96, 98}

type tray struct {
	s        *switcher
	status   *systray.MenuItem
	update   *systray.MenuItem
	accounts [maxAccounts]*systray.MenuItem
	third    *systray.MenuItem // the third-party provider; see thirdparty.go
	add      *systray.MenuItem
	// Auto-switch is off or on at one threshold: one of these is checked.
	autoOff    *systray.MenuItem
	autoAt     []*systray.MenuItem
	spend      *systray.MenuItem
	autostart  *systray.MenuItem
	lockSkills *systray.MenuItem
	syncThird  *systray.MenuItem
	importNow  *systray.MenuItem
	lastIcon   string
	lastTip    string
	updates    chan struct{}
}

func (t *tray) onReady() {
	t.setTooltip(tr.Title)
	t.setIcon(-1, defaultThreshold)

	t.status = systray.AddMenuItem("", "")
	t.status.Disable()
	t.status.Hide()
	t.update = systray.AddMenuItem("", "Download the new version and restart on it")
	t.update.Hide()
	for i := range t.accounts {
		t.accounts[i] = systray.AddMenuItemCheckbox("", tr.SwitchTip, false)
		t.accounts[i].Hide()
	}
	t.third = systray.AddMenuItemCheckbox("", tr.ThirdPartyTip, false)
	t.third.Hide()
	systray.AddSeparator()
	t.add = systray.AddMenuItem(tr.AddAccount, tr.AddAccountTip)
	auto := systray.AddMenuItem(tr.AutoSwitch, "")
	t.autoOff = auto.AddSubMenuItemCheckbox(tr.AutoOff, "", false)
	for _, v := range thresholds {
		t.autoAt = append(t.autoAt, auto.AddSubMenuItemCheckbox(fmt.Sprintf(tr.AutoAt, v), tr.AutoAtTip, false))
	}
	auto.AddSeparator()
	t.spend = auto.AddSubMenuItemCheckbox(tr.SpendWeekly, tr.SpendWeeklyTip, false)
	settings := systray.AddMenuItem(tr.Settings, "")
	// Rewriting an enabled entry keeps its path current and adds the Task
	// Manager record that entries from before v0.1.2 are missing.
	if autostartEnabled() {
		if err := setAutostart(true); err != nil {
			log.Printf("refresh start at login: %v", err)
		}
	}
	t.autostart = settings.AddSubMenuItemCheckbox(tr.StartAtLogin, "", autostartEnabled())
	t.lockSkills = settings.AddSubMenuItemCheckbox(tr.LockSkills, tr.LockSkillsTip, false)
	t.syncThird = settings.AddSubMenuItemCheckbox(tr.SyncChats, tr.SyncChatsTip, false)
	t.importNow = settings.AddSubMenuItem(tr.ImportChats, tr.ImportChatsTip)
	folder := settings.AddSubMenuItem(tr.OpenFolder, "")
	systray.AddSeparator()
	quit := systray.AddMenuItem(tr.Quit, "")

	for i, item := range t.accounts {
		go func() {
			for range item.ClickedCh {
				snap := t.s.snapshot()
				switch {
				case i >= len(snap.Accounts):
				case snap.Accounts[i].UUID != snap.Active:
					go t.s.run("Switch", func() error { return t.s.switchTo(snap.Accounts[i].UUID, byHand) })
				}
				t.s.onChange()
			}
		}()
	}
	for i, item := range t.autoAt {
		go func() {
			for range item.ClickedCh {
				t.s.setAutoSwitch(true, thresholds[i])
			}
		}()
	}
	go func() {
		for {
			select {
			case <-t.autoOff.ClickedCh:
				t.s.setAutoSwitch(false, 0)
			case <-t.spend.ClickedCh:
				t.s.setSpendWeekly(!t.spend.Checked())
			case <-t.third.ClickedCh:
				if !t.s.snapshot().ThirdParty {
					go t.s.run("Switch to third-party", t.s.switchToThirdParty)
				}
				t.s.onChange()
			case <-t.add.ClickedCh:
				go t.s.run("Add account", t.s.addAccount)
			case <-t.importNow.ClickedCh:
				go t.s.run("Import", t.s.importChats)
			case <-t.autostart.ClickedCh:
				if err := setAutostart(!t.autostart.Checked()); err != nil {
					t.s.setStatus("Start at login: " + err.Error())
				}
				setChecked(t.autostart, autostartEnabled())
			case <-t.lockSkills.ClickedCh:
				go t.s.setLockSkills(!t.lockSkills.Checked())
			case <-t.syncThird.ClickedCh:
				t.s.setSyncThirdParty(!t.syncThird.Checked())
			case <-t.update.ClickedCh:
				if v := t.s.snapshot().Update; v != "" {
					go t.s.installUpdate(context.Background(), v)
				}
			case <-folder.ClickedCh:
				shellOpen(t.s.dir)
			case <-quit.ClickedCh:
				// Quitting in the middle of a switch would leave Claude closed.
				go func() {
					if t.s.busy.Load() {
						t.s.setStatus(tr.QuittingAfter)
					}
					for !t.s.busy.CompareAndSwap(false, true) {
						time.Sleep(200 * time.Millisecond)
					}
					systray.Quit()
				}()
			}
		}
	}()

	// Menu updates arrive from worker goroutines; one goroutine applies them.
	t.s.onChange = func() {
		select {
		case t.updates <- struct{}{}:
		default:
		}
	}
	go func() {
		for range t.updates {
			func() {
				defer logPanic("render")
				t.render()
			}()
		}
	}()
	t.s.onChange()
	go t.loop()
	go t.updateLoop()
}

// loop runs refresh every few seconds while a new sign-in is expected,
// otherwise every minute; refresh decides which accounts are due.
func (t *tray) loop() {
	var last time.Time
	for ; ; time.Sleep(5 * time.Second) {
		if !t.s.adding() && time.Since(last) < hotEvery {
			continue
		}
		last = time.Now()
		func() {
			defer logPanic("refresh")
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			t.s.refresh(ctx)
		}()
	}
}

// updateLoop looks for a new release a minute after start, then every hour;
// checkUpdate decides whether a day has passed since the last answer.
func (t *tray) updateLoop() {
	time.Sleep(time.Minute)
	for {
		t.s.checkUpdate(context.Background())
		time.Sleep(time.Hour)
	}
}

func (t *tray) render() {
	snap := t.s.snapshot()
	now := time.Now()

	switch {
	case snap.Status != "":
		t.status.SetTitle(snap.Status)
		t.status.Show()
	case snap.Note != "":
		t.status.SetTitle(snap.Note)
		t.status.Show()
	default:
		t.status.Hide()
	}
	if snap.Update != "" {
		t.update.SetTitle("Version " + snap.Update + " is out, click to update")
		t.update.Show()
	} else {
		t.update.Hide()
	}
	if len(snap.Accounts) == 0 && snap.Status == "" && !snap.ThirdParty {
		t.status.SetTitle(tr.SignIn)
		t.status.Show()
	}
	// Switching, adding and importing all restart Claude: not while a
	// restart runs.
	canRestart := !snap.Busy

	activePct := -1.0
	tip := tr.Title
	for i, item := range t.accounts {
		if i >= len(snap.Accounts) {
			item.Hide()
			continue
		}
		a := snap.Accounts[i]
		detail := usageText(a, snap.Threshold, now)
		item.SetTitle(a.label() + menuTab + detail)
		setChecked(item, a.UUID == snap.Active)
		setEnabled(item, canRestart)
		item.Show()
		if a.UUID == snap.Active {
			tip = a.label() + "\n" + detail
			if a.Usage != nil {
				activePct = a.Usage.current(now).Session
			}
		}
	}
	if label := snap.ThirdPartyItem; label != "" {
		t.third.SetTitle(label)
		setChecked(t.third, snap.ThirdParty)
		setEnabled(t.third, canRestart)
		t.third.Show()
		if snap.ThirdParty {
			tip = tr.Title + "\n" + label
		}
	} else {
		t.third.Hide()
	}
	t.setTooltip(tip)
	t.setIcon(activePct, snap.Threshold)

	setChecked(t.autoOff, !snap.AutoSwitch)
	for i, item := range t.autoAt {
		setChecked(item, snap.AutoSwitch && thresholds[i] == snap.Threshold)
	}
	setChecked(t.spend, snap.SpendWeekly)
	setChecked(t.lockSkills, snap.LockSkills)
	setChecked(t.syncThird, snap.SyncChats)
	setEnabled(t.add, canRestart)
	// Importing goes into the signed-in account.
	setEnabled(t.importNow, canRestart && !snap.ThirdParty)
}

// menuTab right-aligns the usage on Windows, where a tab in a menu title
// starts a right-aligned column.
var menuTab = map[bool]string{true: "\t", false: "    "}[runtime.GOOS == "windows"]

func usageText(a Account, threshold float64, now time.Time) string {
	switch a.Problem {
	case "":
	case problemSignedOut, "signed out, add it again": // the latter from before 0.3.6-ext.1
		return tr.SignedOut
	default:
		return a.Problem
	}
	if a.Usage == nil {
		return tr.UsageUnknown
	}
	u := a.Usage.current(now)
	parts := []string{fmt.Sprintf("5h %.0f%%", u.Session), fmt.Sprintf("%s %.0f%%", tr.Week, u.Weekly)}
	if u.Session >= threshold && !u.SessionReset.IsZero() {
		parts[0] += tr.Until + resetText(u.SessionReset, now)
	}
	// The weekly reset is always shown: it decides how much of the week is left to use.
	switch {
	case u.WeeklyReset.IsZero() || !u.WeeklyReset.After(now):
	case u.Weekly >= threshold:
		parts[1] += tr.Until + resetText(u.WeeklyReset, now)
	default:
		parts[1] += tr.Resets + resetText(u.WeeklyReset, now)
	}
	return strings.Join(parts, " · ")
}

func resetText(at, now time.Time) string {
	at, now = at.Local(), now.Local()
	switch {
	case at.YearDay() == now.YearDay() && at.Year() == now.Year():
		return at.Format("15:04")
	case at.Sub(now) < 6*24*time.Hour:
		return at.Format("Mon 15:04")
	}
	// A weekday alone would read as today for a reset a week out.
	return at.Format("Mon 2 Jan 15:04")
}

// setTooltip only passes a new text on: on Windows each call can wait on the
// shell, and its timeouts filled the log.
func (t *tray) setTooltip(tip string) {
	if tip == t.lastTip {
		return
	}
	t.lastTip = tip
	systray.SetTooltip(tip)
}

func (t *tray) setIcon(pct, threshold float64) {
	key := fmt.Sprintf("%.0f/%.0f", pct, threshold)
	if key == t.lastIcon {
		return
	}
	t.lastIcon = key
	systray.SetIcon(trayIcon(pct, threshold))
}

func setChecked(item *systray.MenuItem, on bool) {
	if on != item.Checked() {
		if on {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
}

func setEnabled(item *systray.MenuItem, on bool) {
	if on == item.Disabled() {
		if on {
			item.Enable()
		} else {
			item.Disable()
		}
	}
}
