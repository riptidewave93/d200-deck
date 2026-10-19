package discord

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/windows"
)

// Sockets lists Discord's named pipes, in order.
func Sockets() []string {
	paths := make([]string, 10)
	for i := range paths {
		paths[i] = fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i)
	}
	return paths
}

// dial opens a named pipe for overlapped I/O, so Go's poller handles it:
// deadlines work and Close ends a waiting read.
func dial(path string) (net.Conn, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, err
	}
	return pipeConn{os.NewFile(uintptr(h), path)}, nil
}

// pipeConn is a named pipe as a net.Conn.
type pipeConn struct{ *os.File }

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

func (c pipeConn) LocalAddr() net.Addr  { return pipeAddr(c.Name()) }
func (c pipeConn) RemoteAddr() net.Addr { return pipeAddr(c.Name()) }
