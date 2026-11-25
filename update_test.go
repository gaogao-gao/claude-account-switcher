package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.3.0", "0.2.0", true},
		{"0.10.0", "0.9.9", true},
		{"1.0.0", "0.99.99", true},
		{"0.2.1", "0.2.0", true},
		{"0.2.0", "0.2.0", false},
		{"0.1.9", "0.2.0", false},
		{"", "0.2.0", false},
		{"0.3", "0.2.0", false},
		{"0.3.0-beta", "0.2.0", false},
	} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestUpdateNow(t *testing.T) {
	tag, calls := "v"+version, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}))
	defer srv.Close()
	defer func(old string) { latestAPI = old }(latestAPI)
	latestAPI = srv.URL

	s := &switcher{dir: t.TempDir(), onChange: func() {}}
	s.updateNow(context.Background())
	if want := fmt.Sprintf(tr.UpToDate, version); calls != 1 || s.snapshot().Status != want {
		t.Fatalf("%d checks, status %q, want %q", calls, s.snapshot().Status, want)
	}

	// Upstream's releases are not this fork's: never offered.
	for _, tag = range []string{"v99.0.0", "nightly"} {
		if v, err := latestRelease(context.Background()); err == nil {
			t.Fatalf("tag %s read as release %s", tag, v)
		}
	}
	tag = "v0.3.6-ext.99"
	if v, err := latestRelease(context.Background()); err != nil || !newerBuild(v, version) {
		t.Fatalf("latest release = %q, %v, want a newer build", v, err)
	}
}

func TestFetchRelease(t *testing.T) {
	bin := []byte("new switcher")
	sum := sha256.Sum256(bin)
	good := hex.EncodeToString(sum[:]) + "  " + releaseAsset() + "\n"
	sha := good
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/v9.9.9/" + releaseAsset():
			w.Write(bin)
		case "/download/v9.9.9/" + releaseAsset() + ".sha256":
			w.Write([]byte(sha))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer func(old string) { releasesURL = old }(releasesURL)
	releasesURL = srv.URL
	if releaseAsset() == "" {
		t.Skip("no release build for this platform")
	}

	got, err := fetchRelease(context.Background(), "9.9.9")
	if err != nil || string(got) != string(bin) {
		t.Fatalf("fetchRelease = %q, %v", got, err)
	}
	sha = strings.Repeat("0", 64) + "  " + releaseAsset() + "\n"
	if _, err := fetchRelease(context.Background(), "9.9.9"); err == nil {
		t.Fatal("accepted a download that does not match its checksum")
	}
	if _, err := fetchRelease(context.Background(), "9.9.8"); err == nil {
		t.Fatal("accepted a missing release")
	}
}

func TestReplaceExe(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "switcher.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExe(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Fatalf("exe = %q after the update", got)
	}
	if got, _ := os.ReadFile(exe + ".old"); string(got) != "old" {
		t.Fatalf("exe.old = %q", got)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal("the download was left next to exe")
	}

	restoreExe(exe)
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("exe = %q after restoring", got)
	}
}
