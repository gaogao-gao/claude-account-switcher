package main

import (
	"bufio"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

// Only one switcher may run. It holds instancePort; a second start says hello
// with its version there. The running one quits when the newcomer is newer,
// so starting a new version replaces the old one, and otherwise stays.
const instancePort = "127.0.0.1:47823"

const helloPrefix = "claude-account-switcher "

// This fork's versions, and so its hellos, carry forkMark. Newer fork builds
// replace older ones; an upstream build never replaces a fork build.
const forkMark = "-ext."

// forkBase is the upstream version this build is based on.
var forkBase, _, _ = strings.Cut(version, forkMark)

// lockInstance takes the single-instance port, taking it over from an older
// switcher that is running. It fails when a switcher of this version or a
// newer one is running.
func lockInstance(addr string) (net.Listener, bool) {
	if l, err := net.Listen("tcp", addr); err == nil {
		return l, true
	}
	switch hello(addr, version) {
	case "quit":
		log.Print("an older switcher is running, it is quitting")
	default:
		// "stay" also comes from any upstream build, which cannot read this
		// fork's version. Versions before 0.3.3 do not answer at all. Ending
		// another process looks like malware to Windows Defender, so the user
		// quits it; quitting silently would look like a crash.
		log.Print("another switcher is running")
		showNotice(tr.AlreadyRunning)
		return nil, false
	}
	// The old switcher finishes a running switch before it quits, which can
	// take minutes with a stopped Claude and the resume dialog.
	for deadline := time.Now().Add(5 * time.Minute); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if l, err := net.Listen("tcp", addr); err == nil {
			return l, true
		}
	}
	log.Print("the running switcher did not quit")
	return nil, false
}

// hello tells the switcher holding addr this version and returns its answer,
// "quit" or "stay", or "" when it does not answer.
func hello(addr, v string) string {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return ""
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(helloPrefix + v + "\n")); err != nil {
		return ""
	}
	answer, _ := bufio.NewReader(conn).ReadString('\n')
	return strings.TrimSpace(answer)
}

// serveInstance answers hellos on the lock and calls quit, once, when a newer
// build of this fork says hello. A newer upstream version is told to stay
// out, and upstream is called with its version.
func serveInstance(l net.Listener, quit func(), upstream func(v string)) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		line, _ := bufio.NewReader(conn).ReadString('\n')
		v, ok := strings.CutPrefix(line, helloPrefix)
		v = strings.TrimSpace(v)
		if ok && newerBuild(v, version) {
			conn.Write([]byte("quit\n"))
			conn.Close()
			log.Printf("version %s started, quitting", v)
			quit()
			return
		}
		conn.Write([]byte("stay\n"))
		conn.Close()
		if ok && newerVersion(v, forkBase) {
			log.Printf("upstream version %s started, this fork stays", v)
			upstream(v)
		}
	}
}

// newerBuild says whether a build of this fork, version a, comes after b.
func newerBuild(a, b string) bool {
	abase, an, aok := splitFork(a)
	bbase, bn, bok := splitFork(b)
	switch {
	case !aok || !bok:
		return false
	case abase != bbase:
		return newerVersion(abase, bbase)
	}
	return an > bn
}

// splitFork splits a version of this fork, like 0.3.6-ext.1, into the
// upstream version it builds on and its own number.
func splitFork(v string) (base string, n int, ok bool) {
	base, num, found := strings.Cut(v, forkMark)
	if !found || parseVersion(base) == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(num)
	return base, n, err == nil && n >= 0
}
