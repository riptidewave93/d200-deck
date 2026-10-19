package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/riptidewave93/d200-deck/internal/discord"
)

// configDir holds deckhost's own settings, such as the user's Discord app.
func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "deckhost"), nil
}

// discordLogin connects to Discord and logs in as the user's app, asking
// the user to allow it the first time. The token is kept for next time.
func discordLogin() (*discord.Conn, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	var app discord.App
	if err := readJSON(filepath.Join(dir, "discord.json"), &app); err != nil {
		return nil, fmt.Errorf("no Discord app set up: %w", err)
	}
	if app.ClientID == "" || app.ClientSecret == "" {
		return nil, errors.New("discord.json needs client_id and client_secret")
	}
	conn, err := discord.Dial(app.ClientID)
	if err != nil {
		return nil, err
	}
	tokenPath := filepath.Join(dir, "discord-token.json")
	var tok discord.Token
	if readJSON(tokenPath, &tok) == nil {
		if time.Until(tok.Expiry) < time.Hour {
			if t, err := app.Refresh(tok); err == nil {
				tok = t
				writeJSON(tokenPath, tok)
			}
		}
		if conn.Authenticate(tok) == nil {
			return conn, nil
		}
	}
	code, err := conn.Authorize(app)
	if err == nil {
		tok, err = app.Exchange(code)
	}
	if err == nil {
		err = conn.Authenticate(tok)
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := writeJSON(tokenPath, tok); err != nil {
		log.Printf("can't keep the Discord token: %v", err)
	}
	return conn, nil
}

// discordTest toggles mute and back, to check deckhost can control Discord.
func discordTest() error {
	conn, err := discordLogin()
	if err != nil {
		return err
	}
	defer conn.Close()
	v, err := conn.VoiceSettings()
	if err != nil {
		return err
	}
	fmt.Printf("muted %v, deafened %v, mode %s\n", v.Mute, v.Deaf, v.Mode.Type)
	if _, err := conn.Do("SUBSCRIBE", nil, "VOICE_SETTINGS_UPDATE"); err != nil {
		return fmt.Errorf("subscribing: %w", err)
	}
	go func() {
		for e := range conn.Events() {
			var v discord.VoiceSettings
			json.Unmarshal(e.Data, &v)
			fmt.Printf("  event %s: muted %v\n", e.Evt, v.Mute)
		}
	}()
	if v, err = conn.SetVoiceSettings(map[string]any{"mute": !v.Mute}); err != nil {
		return fmt.Errorf("changing mute: %w", err)
	}
	fmt.Printf("now muted %v; changing it back in 3 s\n", v.Mute)
	time.Sleep(3 * time.Second)
	if v, err = conn.SetVoiceSettings(map[string]any{"mute": !v.Mute}); err != nil {
		return err
	}
	fmt.Printf("muted %v again\n", v.Mute)
	time.Sleep(500 * time.Millisecond) // let the last event arrive
	return nil
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
