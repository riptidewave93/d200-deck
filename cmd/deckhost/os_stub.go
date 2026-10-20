//go:build !linux && !windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

// Placeholders until deckhost is ported to this system.

var errUnsupported = errors.New("not supported on this system yet")

var openURL = func(u string) error { return errUnsupported }

func detached(path string, args []string) *exec.Cmd { return exec.Command(path, args...) }

func runtimeDir() (string, error) { return "", errUnsupported }

func lockInstance(path string) (*os.File, error) { return nil, errUnsupported }

func restartSelf(lock *os.File) error { return errUnsupported }

func restartService() error { return errUnsupported }

func secureSocket(path string) error { return nil }

func logToFile() {}

func trayIcon(png []byte) []byte { return png }

func notify(title, body, url string) {}
