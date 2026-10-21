package main

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// deckhost.exe is a GUI program (no console window when started at login),
// so a command run from a terminal attaches to the terminal's console to
// print there.
func init() {
	attach := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	const attachParentProcess = ^uintptr(0)
	if ok, _, _ := attach.Call(attachParentProcess); ok == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
		log.SetOutput(f)
	}
}

// openURL opens u in the default browser.
var openURL = func(u string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(u)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// detached is a command for program path in its own process group, so it
// outlives the daemon.
func detached(path string, args []string) *exec.Cmd {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	return cmd
}

// runtimeEnv names the environment variable runtimeDir is under.
const runtimeEnv = "LOCALAPPDATA"

// runtimeDir holds the control socket, the single-instance lock and the log.
func runtimeDir() (string, error) {
	base, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "deckhost")
	return dir, os.MkdirAll(dir, 0o700)
}

// lockInstance opens path with no sharing, held until deckhost exits.
func lockInstance(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return nil, errors.New("deckhost is already running")
	}
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

// restartSelf starts a fresh deckhost and exits.
func restartSelf(lock *os.File) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	lock.Close()
	if err := detached(exe, os.Args[1:]).Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// restartService starts the daemon; it's only called when it isn't running.
func restartService() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return detached(exe, []string{"daemon"}).Start()
}

// secureSocket does nothing: %LOCALAPPDATA% is the user's alone.
func secureSocket(path string) error { return nil }

// trayIcon is the tray icon as an .ico, which Windows wants.
func trayIcon(png []byte) []byte { return icoFromPNG(png) }
