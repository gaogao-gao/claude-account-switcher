//go:build !windows && !darwin

package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// There is no official Claude desktop app for this platform. These stubs keep
// the project building so the shared code can be tested here.

var errUnsupported = errors.New("the Claude desktop app is not supported on this platform")

func claudeDesktop() desktop {
	home, _ := os.UserHomeDir()
	return desktop{dir: filepath.Join(home, ".config", "Claude")}
}

func thirdPartyConfigPath() string {
	return filepath.Join(filepath.Dir(claudeDesktop().dir), "Claude-3p", "claude_desktop_config.json")
}

func desktopRunning() bool                 { return false }
func desktopProcesses() ([]uint32, string) { return nil, "" }
func quitDesktop() error                   { return nil }
func startDesktop(string) error            { return errUnsupported }
func shellOpen(string)                     {}
func autostartEnabled() bool               { return false }
func setAutostart(bool) error              { return errUnsupported }
func decryptValue([]byte) ([]byte, error)  { return nil, errUnsupported }

func uiLanguage() string                 { return os.Getenv("LANG") }
func lockSkill(string, string) error     { return errUnsupported }
func unlockSkill(string, string) error   { return errUnsupported }
func relaunchOutsideDesktop(string) bool { return false }
func launch(string) error                { return errUnsupported }
func showNotice(string)                  {}
func readClaudePolicy() (bool, string)   { return false, "" }

func nativeResume(titles []string, _ time.Duration) []bool {
	keep := make([]bool, len(titles))
	for i := range keep {
		keep[i] = true
	}
	return keep
}

func showWebDialog(string, int) ([]bool, error) { return nil, errUnsupported }
