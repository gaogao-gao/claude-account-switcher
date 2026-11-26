package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Claude ends the processes it started when it quits, which would take a
// switcher started from a Claude Code terminal down in the middle of a
// switch. Such a switcher starts itself again outside Claude's processes.

// claudeEnvSet reports whether the variables Claude Code sets are there. They
// survive a parent that already exited.
func claudeEnvSet() bool {
	return os.Getenv("CLAUDECODE") != "" || os.Getenv("CLAUDE_CODE_ENTRYPOINT") != ""
}

// proc is a process's parent and executable name or path.
type proc struct {
	parent uint32
	name   string
}

// claudeAncestor reports whether a Claude process is among pid's ancestors.
func claudeAncestor(pid uint32, procs map[uint32]proc) bool {
	for range 32 {
		p, ok := procs[pid]
		if !ok || p.parent == 0 || p.parent == pid {
			return false
		}
		pid = p.parent
		parent, ok := procs[pid]
		if !ok {
			return false
		}
		switch strings.ToLower(filepath.Base(parent.name)) {
		case "claude.exe", "claude":
			return true
		case "explorer.exe":
			// Started from the Windows shell. Its own parent has long
			// exited, and a reused process ID up there must not count.
			return false
		}
	}
	return false
}

// parsePS reads the output of ps -axo pid=,ppid=,comm=.
func parsePS(out string) map[uint32]proc {
	procs := map[uint32]proc{}
	for _, line := range strings.Split(out, "\n") {
		// The name is the rest of the line and can hold spaces, as in
		// "Claude Helper (Renderer)".
		pidText, rest, _ := strings.Cut(strings.TrimSpace(line), " ")
		ppidText, name, _ := strings.Cut(strings.TrimSpace(rest), " ")
		pid, err1 := strconv.ParseUint(pidText, 10, 32)
		ppid, err2 := strconv.ParseUint(ppidText, 10, 32)
		if name = strings.TrimSpace(name); err1 == nil && err2 == nil && name != "" {
			procs[uint32(pid)] = proc{uint32(ppid), name}
		}
	}
	return procs
}

// relaunchOnce starts the switcher again with relaunch and reports whether it
// did. A marker file stops a relaunch loop should a check ever misjudge.
func relaunchOnce(dataDir string, relaunch func(exe string) error) bool {
	marker := filepath.Join(dataDir, "relaunched")
	if info, err := os.Stat(marker); err == nil && time.Since(info.ModTime()) < 30*time.Second {
		return false
	}
	exe, err := os.Executable()
	if err != nil || os.WriteFile(marker, nil, 0o600) != nil {
		return false
	}
	return relaunch(exe) == nil
}
