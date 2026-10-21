//go:build windows || darwin

package main

import (
	"log"
	"os"
	"path/filepath"
)

// maxLog is how big the log gets before it's started afresh.
const maxLog = 1 << 20

// logToFile sends the daemon's log to deckhost.log next to its socket.
func logToFile() {
	dir, err := runtimeDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "deckhost.log")
	flag := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLog {
		flag |= os.O_TRUNC
	}
	if f, err := os.OpenFile(path, flag, 0o600); err == nil {
		log.SetOutput(f)
		log.SetFlags(log.LstdFlags)
	}
}
