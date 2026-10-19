//go:build !windows

package discord

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// socketDirs are where Discord's socket may be, under each base directory:
// a native install, Flatpak and Snap.
var socketDirs = []string{"", "app/com.discordapp.Discord", "snap.discord"}

// Sockets lists the paths Discord's socket may be at, in order.
func Sockets() []string {
	var paths []string
	for _, env := range []string{"XDG_RUNTIME_DIR", "TMPDIR", "TMP", "TEMP"} {
		if base := os.Getenv(env); base != "" {
			paths = appendSockets(paths, base)
		}
	}
	return appendSockets(paths, "/tmp")
}

func appendSockets(paths []string, base string) []string {
	for _, d := range socketDirs {
		for i := range 10 {
			paths = append(paths, filepath.Join(base, d, fmt.Sprintf("discord-ipc-%d", i)))
		}
	}
	return paths
}

// dial connects to the socket at path if it's the user's own, not one
// another user put in a shared directory such as /tmp to get the token.
func dial(path string) (net.Conn, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
		return nil, fmt.Errorf("%s: not the user's", path)
	}
	return net.DialTimeout("unix", path, time.Second)
}
