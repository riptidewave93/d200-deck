//go:build linux || darwin

package main

import (
	"errors"
	"os"
	"syscall"
)

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

// restartSelf replaces this process with a fresh deckhost. exec releases
// lock.
func restartSelf(lock *os.File) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// secureSocket makes the control socket the user's alone.
func secureSocket(path string) error { return os.Chmod(path, 0o600) }

// trayIcon is the tray icon in the format the system wants: PNG here.
func trayIcon(png []byte) []byte { return png }
