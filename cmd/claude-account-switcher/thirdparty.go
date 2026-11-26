package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"strings"
)

// CC Switch or an enterprise policy can give the desktop app a third-party
// inference provider. The app then runs on a data folder of its own,
// Claude-3p, unless deploymentMode in Claude-3p/claude_desktop_config.json is
// "1p". The menu offers that provider as one more entry after the accounts:
// picking it, or an account while the app is on it, quits the app, sets
// deploymentMode and starts the app again. That key is the only thing the
// switcher writes in that file (see setDeploymentMode); the provider settings
// belong to whatever set them up.

// thirdPartyFallback names the third-party entry when its config has no name.
const thirdPartyFallback = "API"

// onThirdParty reports whether the app runs on its third-party profile or,
// while it is not running, will start on it. item is the menu entry of the
// provider, "" while there is none to show.
func onThirdParty() (on bool, item string) {
	cfg, name := appliedConfig(configLibrary())
	available := thirdPartyAvailable(cfg)
	on = liveThirdParty(profileInUse(claudeDesktop().dir), profileInUse(thirdPartyApp().dir),
		deploymentIsThirdParty(thirdPartyConfigPath()), available)
	if on || available {
		item = thirdPartyLabel(name)
	}
	return on, item
}

// liveThirdParty decides onThirdParty: the profile the app holds open, if it
// holds one, else the mode it is set to, which only counts with a provider
// to start on.
func liveThirdParty(officialLive, thirdLive, setThird, available bool) bool {
	switch {
	case officialLive:
		return false
	case thirdLive:
		return true
	}
	return setThird && available
}

// deploymentIsThirdParty reads the app's claude_desktop_config.json at path.
// Anything unreadable counts as the official mode, which the app falls back
// to as well.
func deploymentIsThirdParty(path string) bool {
	cfg, err := readObject(path)
	if err != nil {
		return false
	}
	mode := stringField(cfg, "deploymentMode")
	return mode != "" && mode != "1p"
}

// setDeploymentMode sets deploymentMode in the app's
// claude_desktop_config.json at path to mode and leaves the rest of the file
// as it is. A missing file is created with that key alone; a file that
// cannot be read is not written.
func setDeploymentMode(path, mode string) error {
	cfg, err := readObject(path)
	if errors.Is(err, fs.ErrNotExist) {
		cfg, err = &object{vals: map[string]json.RawMessage{}}, nil
	}
	if err != nil {
		return err
	}
	v, _ := json.Marshal(mode)
	cfg.set("deploymentMode", v)
	return cfg.write(path)
}

// claudePolicy reports whether a Claude policy is set, which makes the app
// ignore its local provider configs, and the policy's inferenceProvider.
// A variable, so tests can set one.
var claudePolicy = readClaudePolicy

// configLibrary is where the app keeps its local provider configs.
func configLibrary() string {
	return filepath.Join(filepath.Dir(thirdPartyConfigPath()), "configLibrary")
}

// thirdPartyAvailable reports whether the app has a third-party provider to
// start on: the policy's if a policy is set, else that of applied, the
// applied local config (nil for none).
func thirdPartyAvailable(applied *object) bool {
	if set, provider := claudePolicy(); set {
		return provider != ""
	}
	return applied != nil && stringField(applied, "inferenceProvider") != ""
}

// thirdPartyName is name, the applied config's as CC Switch shows it, or
// thirdPartyFallback if it has none.
func thirdPartyName(name string) string {
	if name == "" {
		return thirdPartyFallback
	}
	return name
}

// thirdPartyLabel is the menu entry of the provider config named name.
func thirdPartyLabel(name string) string {
	return fmt.Sprintf(tr.ThirdPartyItem, thirdPartyName(name))
}

// appliedConfig reads the config that _meta.json in library names as
// applied, and its name; nil if there is none.
func appliedConfig(library string) (cfg *object, name string) {
	meta, err := readObject(filepath.Join(library, "_meta.json"))
	if err != nil {
		return nil, ""
	}
	id := stringField(meta, "appliedId")
	if id == "" || strings.ContainsAny(id, `/\`) || id == ".." {
		return nil, ""
	}
	var entries []struct{ ID, Name string }
	if raw, ok := meta.get("entries"); ok {
		_ = json.Unmarshal(raw, &entries)
	}
	for _, e := range entries {
		if e.ID == id {
			name = strings.TrimSpace(e.Name)
		}
	}
	cfg, err = readObject(filepath.Join(library, id+".json"))
	if err != nil {
		return nil, ""
	}
	return cfg, name
}

// switchToThirdParty restarts the app on its third-party provider. Closed,
// the chats of both profiles go into each other first.
func (s *switcher) switchToThirdParty() error {
	_, name := appliedConfig(configLibrary())
	name = thirdPartyName(name)
	s.setStatus(fmt.Sprintf(tr.SwitchingThird, name))
	return s.restart(func(active string) error {
		s.mu.Lock()
		a := s.account(active)
		s.mu.Unlock()
		official, third := s.app(), thirdPartyApp()
		if _, err := s.syncBoth(official, third, a, newestChat(third.sessionsDir()).newest); err != nil {
			log.Printf("switch to third-party: chats only partly synced: %v", err)
		}
		if err := setDeploymentMode(thirdPartyConfigPath(), "3p"); err != nil {
			return fmt.Errorf("set third-party: %w", err)
		}
		log.Printf("switched to third-party (%s)", name)
		return nil
	})
}

// official runs change and then, if the app is set to its third-party
// profile, sets it back to the official one, so the closed app starts on the
// accounts. If change fails, the mode stays.
func official(change func(active string) error) func(active string) error {
	return func(active string) error {
		if err := change(active); err != nil {
			return err
		}
		path := thirdPartyConfigPath()
		if !deploymentIsThirdParty(path) {
			return nil
		}
		if err := setDeploymentMode(path, "1p"); err != nil {
			return fmt.Errorf("set official mode: %w", err)
		}
		log.Print("set Claude back to official mode")
		return nil
	}
}
