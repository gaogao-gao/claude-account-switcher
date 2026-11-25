package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDeploymentIsThirdParty(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		config string
		want   bool
	}{
		{"", false},
		{`{not json`, false},
		{`{"preferences":{}}`, false},
		{`{"deploymentMode":"1p"}`, false},
		{`{"deploymentMode":null}`, false},
		{`{"deploymentMode":3}`, false},
		{`{"coworkUserFilesPath":"x","deploymentMode":"3p"}`, true},
	} {
		path := filepath.Join(dir, "claude_desktop_config.json")
		if c.config != "" {
			write(t, path, c.config)
		}
		if got := deploymentIsThirdParty(path); got != c.want {
			t.Errorf("%q: third party = %v, want %v", c.config, got, c.want)
		}
	}
}

func TestSetDeploymentMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude_desktop_config.json")
	read := func() string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
			t.Error("written with a BOM")
		}
		var got bytes.Buffer
		if err := json.Compact(&got, data); err != nil {
			t.Fatal(err)
		}
		return got.String()
	}

	write(t, path, `{not json`)
	if err := setDeploymentMode(path, "3p"); err == nil {
		t.Error("an unreadable config was written")
	}
	os.Remove(path)
	if err := setDeploymentMode(path, "3p"); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != `{"deploymentMode":"3p"}` {
		t.Errorf("new config = %s", got)
	}

	write(t, path, "\xef\xbb\xbf"+`{"coworkUserFilesPath":"x","deploymentMode":"3p","preferences":{"a":[1,2]}}`)
	if err := setDeploymentMode(path, "1p"); err != nil {
		t.Fatal(err)
	}
	// Only deploymentMode changed, keys in their order.
	if got, want := read(), `{"coworkUserFilesPath":"x","deploymentMode":"1p","preferences":{"a":[1,2]}}`; got != want {
		t.Errorf("config = %s, want %s", got, want)
	}
	if deploymentIsThirdParty(path) {
		t.Error("still set to third-party")
	}
	if err := setDeploymentMode(path, "3p"); err != nil {
		t.Fatal(err)
	}
	if !deploymentIsThirdParty(path) {
		t.Error("not set to third-party")
	}
	if _, err := os.Stat(path + ".switcher-tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left: %v", err)
	}
}

// library writes a config library to a new folder: _meta.json, and the
// applied config unless it is "".
func library(t *testing.T, meta, applied string) string {
	dir := t.TempDir()
	if meta != "" {
		write(t, filepath.Join(dir, "_meta.json"), meta)
	}
	if applied != "" {
		write(t, filepath.Join(dir, "c1.json"), applied)
	}
	return dir
}

func setPolicy(t *testing.T, set bool, provider string) {
	old := claudePolicy
	claudePolicy = func() (bool, string) { return set, provider }
	t.Cleanup(func() { claudePolicy = old })
}

func TestThirdPartyAvailable(t *testing.T) {
	meta := `{"appliedId":"c1","entries":[{"id":"c0","name":"Untitled"},{"id":"c1","name":"CC Switch"}]}`
	provider := `{"inferenceProvider":"gateway","inferenceGatewayBaseUrl":"https://example.com"}`
	for _, c := range []struct {
		name          string
		meta, applied string
		want          bool
	}{
		{"no library", "", "", false},
		{"no applied config", meta, "", false},
		{"applied", meta, provider, true},
		{"no provider", meta, `{"inferenceModels":[]}`, false},
		{"empty provider", meta, `{"inferenceProvider":""}`, false},
		{"null provider", meta, `{"inferenceProvider":null}`, false},
		{"nothing applied", `{"entries":[]}`, provider, false},
		{"applied outside", `{"appliedId":"../c1"}`, provider, false},
	} {
		setPolicy(t, false, "")
		cfg, _ := appliedConfig(library(t, c.meta, c.applied))
		if got := thirdPartyAvailable(cfg); got != c.want {
			t.Errorf("%s: available = %v, want %v", c.name, got, c.want)
		}
		// A policy replaces the local configs.
		setPolicy(t, true, "")
		if thirdPartyAvailable(cfg) {
			t.Errorf("%s: available under a policy without a provider", c.name)
		}
		setPolicy(t, true, "gateway")
		if !thirdPartyAvailable(cfg) {
			t.Errorf("%s: not available under a policy with a provider", c.name)
		}
	}
}

func TestThirdPartyName(t *testing.T) {
	defer func(old *texts) { tr = old }(tr)
	provider := `{"inferenceProvider":"gateway"}`
	for _, c := range []struct {
		meta, want string
	}{
		{`{"appliedId":"c1","entries":[{"id":"c0","name":"Untitled"},{"id":"c1","name":"CC Switch"}]}`, "CC Switch"},
		{`{"appliedId":"c1","entries":[{"id":"c1","name":"  "}]}`, "API"},
		{`{"appliedId":"c1","entries":[{"id":"c0","name":"Untitled"}]}`, "API"},
		{`{"appliedId":"c1"}`, "API"},
		{"", "API"},
	} {
		_, name := appliedConfig(library(t, c.meta, provider))
		if got := thirdPartyName(name); got != c.want {
			t.Errorf("%s: name = %q, want %q", c.meta, got, c.want)
		}
	}
	tr = &english
	if got := thirdPartyLabel("CC Switch"); got != "Third-party (CC Switch)" {
		t.Errorf("label = %q", got)
	}
	tr = &chinese
	if got := thirdPartyLabel(""); got != "第三方（API）" {
		t.Errorf("label = %q", got)
	}
}

func TestLiveThirdParty(t *testing.T) {
	for _, c := range []struct {
		officialLive, thirdLive, setThird, available, want bool
	}{
		// The profile the app holds open decides.
		{true, false, true, true, false},
		{true, true, true, true, false},
		{false, true, false, false, true},
		{false, true, true, true, true},
		// Closed: the mode it is set to, with a provider to start on.
		{false, false, true, true, true},
		{false, false, true, false, false},
		{false, false, false, true, false},
		{false, false, false, false, false},
	} {
		if got := liveThirdParty(c.officialLive, c.thirdLive, c.setThird, c.available); got != c.want {
			t.Errorf("%+v: on third-party = %v", c, got)
		}
	}
}
