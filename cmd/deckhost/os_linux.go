package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// openURL opens u in the default browser.
var openURL = func(u string) error { return start("xdg-open", u) }

// systemdRun, if found, puts started programs in their own systemd scope, so
// they aren't part of deckhost's service and outlive it.
var systemdRun, _ = exec.LookPath("systemd-run")

// detached is a command for program path in its own session and scope.
func detached(path string, args []string) *exec.Cmd {
	args = append([]string{path}, args...)
	if systemdRun != "" {
		args = append([]string{systemdRun, "--user", "--scope", "--quiet", "--"}, args...)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// runtimeDir holds the control socket and the single-instance lock.
func runtimeDir() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set")
	}
	return dir, nil
}

// lockInstance takes the lock at path, held until deckhost exits (or
// restarts: exec closes it).
func lockInstance(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("deckhost is already running")
	}
	return f, nil
}

// restartSelf replaces this process with a fresh deckhost.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// restartService restarts the daemon through its service manager.
func restartService() error {
	return exec.Command("systemctl", "--user", "restart", "deckhost.service").Run()
}
