package main

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestInstanceHello(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	quit := make(chan struct{})
	upstream := make(chan string, 4)
	go serveInstance(l, func() { close(quit) }, func(v string) { upstream <- v })
	addr := l.Addr().String()

	for _, v := range []string{version, "0.0.1", "nightly", "0.3.6-ext.0", "0.3.5-ext.9", "99.0.0"} {
		if got := hello(addr, v); got != "stay" {
			t.Fatalf("hello from %s = %q, want stay", v, got)
		}
	}
	if got := <-upstream; got != "99.0.0" || len(upstream) != 0 {
		t.Fatalf("upstream hellos reported: %q and %d more, want only 99.0.0", got, len(upstream))
	}
	base, n, _ := splitFork(version)
	if got := hello(addr, fmt.Sprintf("%s%s%d", base, forkMark, n+1)); got != "quit" {
		t.Fatalf("hello from a newer build of the fork = %q, want quit", got)
	}
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("did not quit for a newer version")
	}
}

func TestInstanceHelloUnanswered(t *testing.T) {
	// Versions before 0.3.3 hold the port and never answer.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if got := hello(l.Addr().String(), version); got != "" {
		t.Fatalf("hello = %q from a switcher that does not answer", got)
	}
}

func TestNewerBuild(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.3.6-ext.2", "0.3.6-ext.1", true},
		{"0.3.7-ext.1", "0.3.6-ext.9", true},
		{"0.3.6-ext.1", "0.3.6-ext.1", false},
		{"0.3.6-ext.1", "0.3.7-ext.0", false},
		{"0.3.7", "0.3.6-ext.1", false},
		{"0.3.6-ext.x", "0.3.6-ext.1", false},
	} {
		if got := newerBuild(c.a, c.b); got != c.want {
			t.Errorf("newerBuild(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
