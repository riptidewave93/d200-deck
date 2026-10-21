package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// configDir holds deckhost's own settings, such as the user's Discord app.
func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "deckhost"), nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// writeJSON writes v to path, readable only by the user.
func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// hostSettings are deckhost's own settings, in settings.json.
type hostSettings struct {
	// CheckUpdates is whether deckhost looks for new releases; unset is on
	CheckUpdates *bool `json:"check_updates,omitempty"`
}

func settingsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// checkUpdates reads whether deckhost looks for new releases.
func checkUpdates() bool {
	var s hostSettings
	if path, err := settingsPath(); err == nil {
		readJSON(path, &s)
	}
	return s.CheckUpdates == nil || *s.CheckUpdates
}

// saveCheckUpdates saves whether deckhost looks for new releases.
func saveCheckUpdates(on bool) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	var s hostSettings
	readJSON(path, &s)
	s.CheckUpdates = &on
	return writeJSON(path, s)
}
