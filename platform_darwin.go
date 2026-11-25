package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func claudeDesktop() desktop {
	home, _ := os.UserHomeDir()
	return desktop{dir: filepath.Join(home, "Library", "Application Support", "Claude")}
}

// thirdPartyConfigPath is where the app keeps its deployment mode, see
// thirdparty.go. Unverified on a Mac: the folder name is the one Windows uses.
func thirdPartyConfigPath() string {
	return filepath.Join(filepath.Dir(claudeDesktop().dir), "Claude-3p", "claude_desktop_config.json")
}

// readClaudePolicy finds no policy: managed preferences on a Mac are not
// read, so only the local provider configs count.
func readClaudePolicy() (bool, string) { return false, "" }

// uiLanguage is the user's first preferred language, like "zh-Hans-CN".
func uiLanguage() string {
	if out, err := output("defaults", "read", "-g", "AppleLanguages"); err == nil {
		if tag := firstAppleLanguage(out); tag != "" {
			return tag
		}
	}
	return os.Getenv("LANG")
}

// The main process is named "Claude"; helpers are "Claude Helper (...)".
func desktopRunning() bool {
	return run("pgrep", "-x", "Claude") == nil
}

func desktopProcesses() ([]uint32, string) {
	if desktopRunning() {
		return []uint32{0}, ""
	}
	return nil, ""
}

// quitDesktop asks the app to quit, then ends it if it is still running.
// SIGTERM makes Chromium quit as it does from its menu. Asking through
// AppleScript needs the Automation consent, and its prompt held the switch
// up for as long as nobody answered it.
func quitDesktop() error {
	if !desktopRunning() {
		return nil
	}
	began := time.Now()
	_ = run("pkill", "-TERM", "-x", "Claude")
	// Quitting stops every running chat first, which can take a while.
	if waitGone(20 * time.Second) {
		log.Printf("Claude quit after %s", time.Since(began).Round(100*time.Millisecond))
		return nil
	}
	_ = run("pkill", "-9", "-x", "Claude")
	if waitGone(10 * time.Second) {
		log.Printf("Claude did not quit on request, ended it after %s", time.Since(began).Round(100*time.Millisecond))
		return nil
	}
	return errors.New("Claude did not quit")
}

func waitGone(timeout time.Duration) bool {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		if !desktopRunning() {
			time.Sleep(700 * time.Millisecond)
			return true
		}
	}
	return false
}

func startDesktop(string) error {
	return run("open", "-a", "Claude")
}

// shellOpen opens a folder, or a web page in the default browser.
func shellOpen(target string) { _ = exec.Command("open", target).Start() }

// launch starts the switcher at exe in its own session, so it keeps running
// after this process quits.
func launch(exe string) error {
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// showNotice shows text in an alert, which closes by itself after five
// minutes. The text comes in as an argument, so it needs no AppleScript quoting.
func showNotice(text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	_ = exec.CommandContext(ctx, "osascript",
		"-e", "on run argv",
		"-e", "display alert (item 1 of argv) message (item 2 of argv) giving up after 300",
		"-e", "end run", tr.Title, text).Run()
}

// launchAgentLabel matches CFBundleIdentifier in packaging/macos/Info.plist.
const launchAgentLabel = "io.github.gaogao-gao.claude-account-switcher"

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

// oldLaunchAgent returns the login item that earlier builds of this fork wrote
// under upstream's label, or "" when there is none for this executable. One
// that starts another program, such as an upstream build, is left alone.
func oldLaunchAgent() string {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, "Library", "LaunchAgents", "io.github.snlperstripes.claude-account-switcher.plist")
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "<string>"+html.EscapeString(exe)+"</string>") {
		return ""
	}
	return path
}

func autostartEnabled() bool {
	if _, err := os.Stat(launchAgentPath()); err == nil {
		return true
	}
	return oldLaunchAgent() != ""
}

func setAutostart(on bool) error {
	if old := oldLaunchAgent(); old != "" {
		if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !on {
		if err := os.Remove(launchAgentPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key><array><string>%s</string></array>
	<key>RunAtLoad</key><true/>
</dict>
</plist>
`, launchAgentLabel, html.EscapeString(exe))
	if err := os.MkdirAll(filepath.Dir(launchAgentPath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(launchAgentPath(), []byte(plist), 0o644)
}

// startedByClaude reports whether Claude started this process, for example
// from a Claude Code terminal, by Claude Code's variables or a Claude
// process among the ancestors.
func startedByClaude() bool {
	if claudeEnvSet() {
		return true
	}
	out, err := output("ps", "-axo", "pid=,ppid=,comm=")
	return err == nil && claudeAncestor(uint32(os.Getpid()), parsePS(out))
}

// relaunchOutsideDesktop starts the switcher again in its own session when
// Claude started it, so quitting Claude does not end it. The new process
// must not inherit Claude Code's variables, or it would relaunch again.
func relaunchOutsideDesktop(dataDir string) bool {
	if !startedByClaude() {
		return false
	}
	return relaunchOnce(dataDir, func(exe string) error {
		os.Unsetenv("CLAUDECODE")
		os.Unsetenv("CLAUDE_CODE_ENTRYPOINT")
		return launch(exe)
	})
}

// A helper command that hangs must not hold up a switch with Claude closed.
const commandTimeout = 15 * time.Second

func run(name string, args ...string) error {
	_, err := output(name, args...)
	return err
}

func output(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}
