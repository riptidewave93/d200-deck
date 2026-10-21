package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Finder starts an app without arguments: in the app bundle, that's the
// tray.
func init() {
	if len(os.Args) == 1 && strings.Contains(os.Args[0], ".app/Contents/MacOS/") {
		os.Args = append(os.Args, "tray")
	}
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
