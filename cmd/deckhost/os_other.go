//go:build !linux && !windows && !darwin

package main

import (
	"errors"
	"os"
	"os/exec"
)

// Placeholders until deckhost is ported to this system. A port provides, in
// files for its GOOS (then left out of the *_other.go files' build tags):
//   - os_*.go: the functions below
//   - notify_*.go: notify, the desktop notifications
//   - media_*.go: openMedia, the media players' controls
//   - programs_*.go: programCandidates and findProgram, for "Open app" keys
//   - internal/hidraw: opening the device's HID nodes

var errUnsupported = errors.New("not supported on this system yet")

const runtimeEnv = ""

var openURL = func(u string) error { return errUnsupported }

func detached(path string, args []string) *exec.Cmd { return exec.Command(path, args...) }

func runtimeDir() (string, error) { return "", errUnsupported }

func lockInstance(path string) (*os.File, error) { return nil, errUnsupported }

func restartSelf(lock *os.File) error { return errUnsupported }

func restartService() error { return errUnsupported }

func secureSocket(path string) error { return nil }

func logToFile() {}

func trayIcon(png []byte) []byte { return png }
