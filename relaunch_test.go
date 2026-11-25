package main

import "testing"

func TestClaudeAncestor(t *testing.T) {
	procs := parsePS(`    1     0 /sbin/launchd
  512     1 /Applications/Claude.app/Contents/MacOS/Claude
  530   512 /Applications/Claude.app/Contents/Frameworks/Claude Helper (Renderer).app/Contents/MacOS/Claude Helper (Renderer)
  531   530 /bin/zsh
  532   531 ./claude-account-switcher
  600     1 /System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal
  601   600 -zsh
  602   601 ./claude-account-switcher
`)
	if p := procs[530]; p.parent != 512 || p.name != "/Applications/Claude.app/Contents/Frameworks/Claude Helper (Renderer).app/Contents/MacOS/Claude Helper (Renderer)" {
		t.Fatalf("parsed %+v", p)
	}
	if !claudeAncestor(532, procs) {
		t.Fatal("missed Claude above a terminal in the app")
	}
	if claudeAncestor(602, procs) {
		t.Fatal("took a Terminal start for Claude's")
	}
	win := map[uint32]proc{
		10: {1, "explorer.exe"}, 1: {0, "claude.exe"}, 11: {10, "switcher.exe"},
		20: {21, "Claude.exe"}, 21: {0, "x"}, 22: {20, "cmd.exe"}, 23: {22, "switcher.exe"},
	}
	if claudeAncestor(11, win) {
		t.Fatal("looked past explorer.exe")
	}
	if !claudeAncestor(23, win) {
		t.Fatal("missed claude.exe")
	}
}
