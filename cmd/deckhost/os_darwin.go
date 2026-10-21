package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Finder starts an app without arguments: in the app bundle, that's the
// tray, which then starts at login too.
func init() {
	if len(os.Args) == 1 && strings.Contains(os.Args[0], ".app/Contents/MacOS/") {
		os.Args = append(os.Args, "tray")
		if err := startAtLogin(); err != nil {
			log.Printf("starting at login: %v", err)
		}
	}
}

// launchAgent is the label of the launchd job that starts the tray at login,
// and the app's bundle ID, which names the job in Login Items.
const launchAgent = "io.github.riptidewave93.deckhost"

// startAtLogin adds a launchd job that starts this app's tray at login, or
// points it here if the app has moved. It leaves an unchanged job alone, so
// it stays off if the user turned it off in Login Items.
func startAtLogin() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.Contains(exe, "/AppTranslocation/") {
		// opened where it was downloaded: macOS runs it from a temporary
		// copy, gone by the next login
		return errors.New("move D200 Deck to Applications first")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, "Library", "LaunchAgents", launchAgent+".plist")
	want := launchAgentPlist(exe)
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, want) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, want, 0o644)
}

// launchAgentPlist is the launchd job that runs exe's tray at login.
func launchAgentPlist(exe string) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + launchAgent + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>`)
	xml.EscapeText(&b, []byte(exe))
	b.WriteString(`</string>
		<string>tray</string>
	</array>
	<key>AssociatedBundleIdentifiers</key>
	<array>
		<string>` + launchAgent + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`)
	return b.Bytes()
}

// openURL opens u in the default browser.
var openURL = func(u string) error { return start("open", u) }

// detached is a command for program path in its own session, so it outlives
// the daemon.
func detached(path string, args []string) *exec.Cmd {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// runtimeEnv names the environment variable runtimeDir is under.
const runtimeEnv = "TMPDIR"

// runtimeDir holds the control socket, the single-instance lock and the log:
// the user's own temporary directory, emptied at boot.
func runtimeDir() (string, error) {
	dir := os.Getenv(runtimeEnv)
	if dir == "" {
		return "", errors.New(runtimeEnv + " is not set")
	}
	return dir, nil
}

// restartService starts the daemon; it's only called when it isn't running.
func restartService() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return detached(exe, []string{"daemon"}).Start()
}
