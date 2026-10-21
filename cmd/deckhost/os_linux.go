package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// openURL opens u in the default browser.
var openURL = func(u string) error { return start("xdg-open", u) }

// systemdRun, where systemd runs, puts started programs in their own systemd
// scope, so they aren't part of deckhost's service and outlive it.
var systemdRun = func() string {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return ""
	}
	p, _ := exec.LookPath("systemd-run")
	return p
}()

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

// runtimeEnv names the environment variable runtimeDir is under.
const runtimeEnv = "XDG_RUNTIME_DIR"

// runtimeDir holds the control socket and the single-instance lock.
func runtimeDir() (string, error) {
	dir := os.Getenv(runtimeEnv)
	if dir == "" {
		return "", errors.New(runtimeEnv + " is not set")
	}
	return dir, nil
}

// restartService restarts the daemon through systemd or, where there's no
// systemd unit for it, starts it logging to deckhost.log next to its socket.
func restartService() error {
	if exec.Command("systemctl", "--user", "restart", "deckhost.service").Run() == nil {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := detached(exe, []string{"daemon"})
	if dir, err := runtimeDir(); err == nil {
		f, err := os.OpenFile(filepath.Join(dir, "deckhost.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err == nil {
			defer f.Close()
			cmd.Stdout, cmd.Stderr = f, f
		}
	}
	return cmd.Start()
}

// logToFile sends the daemon's log to a file if nothing else keeps it:
// journald does here.
func logToFile() {}
