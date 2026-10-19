// Package discord talks to the Discord desktop app over its local RPC socket:
// https://docs.discord.com/developers/topics/rpc
package discord

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Frame opcodes.
const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
	opPing      = 3
	opPong      = 4
)

// maxFrame bounds a frame from Discord.
const maxFrame = 1 << 20

// Timeout bounds a request to Discord.
var Timeout = 10 * time.Second

// Message is a command, a reply or an event.
type Message struct {
	Cmd   string          `json:"cmd"`
	Args  any             `json:"args,omitempty"`
	Evt   string          `json:"evt,omitempty"`
	Nonce string          `json:"nonce,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Error is an ERROR reply.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("discord: %s (%d)", e.Message, e.Code) }

// Conn is a connection to the Discord app.
type Conn struct {
	c      net.Conn
	wmu    sync.Mutex
	mu     sync.Mutex
	wait   map[string]chan Message
	events chan Message
	done   chan struct{}
	err    error
}

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

// Dial connects to the first Discord socket that answers and introduces
// the app with clientID.
func Dial(clientID string) (*Conn, error) {
	for _, p := range Sockets() {
		c, err := net.DialTimeout("unix", p, time.Second)
		if err != nil {
			continue
		}
		return NewConn(c, clientID)
	}
	return nil, errors.New("discord isn't running")
}

// NewConn does the handshake on c and starts reading.
func NewConn(c net.Conn, clientID string) (*Conn, error) {
	d := &Conn{c: c, wait: map[string]chan Message{}, events: make(chan Message, 16), done: make(chan struct{})}
	if err := d.write(opHandshake, map[string]any{"v": 1, "client_id": clientID}); err != nil {
		c.Close()
		return nil, err
	}
	c.SetReadDeadline(time.Now().Add(Timeout))
	op, b, err := readFrame(c)
	c.SetReadDeadline(time.Time{})
	if err == nil && op == opClose {
		err = closeError(b)
	}
	var m Message
	if err == nil {
		if err = json.Unmarshal(b, &m); err == nil && m.Evt != "READY" {
			err = fmt.Errorf("discord: handshake got %s %s", m.Cmd, m.Evt)
		}
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	go d.read()
	return d, nil
}

func closeError(b []byte) error {
	var e Error
	json.Unmarshal(b, &e)
	return &e
}

// Events are the events subscribed to. It's closed when the connection ends.
func (d *Conn) Events() <-chan Message { return d.events }

// Done is closed when the connection ends; Err then says why.
func (d *Conn) Done() <-chan struct{} { return d.done }

// Err is why the connection ended.
func (d *Conn) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

// Close ends the connection.
func (d *Conn) Close() error { return d.c.Close() }

func (d *Conn) read() {
	var err error
	defer func() {
		d.mu.Lock()
		d.err = err
		d.mu.Unlock()
		close(d.done)
		close(d.events)
		d.c.Close()
	}()
	for {
		var op uint32
		var b []byte
		if op, b, err = readFrame(d.c); err != nil {
			return
		}
		switch op {
		case opPing:
			d.write(opPong, json.RawMessage(b))
			continue
		case opClose:
			err = closeError(b)
			return
		case opFrame:
		default:
			continue
		}
		var m Message
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		if m.Nonce != "" {
			d.mu.Lock()
			ch := d.wait[m.Nonce]
			delete(d.wait, m.Nonce)
			d.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		} else if m.Evt != "" {
			select {
			case d.events <- m:
			default: // nobody reading: drop
			}
		}
	}
}

// Do sends cmd with args (and evt, for SUBSCRIBE) and returns the reply's
// data, or its error.
func (d *Conn) Do(cmd string, args any, evt string) (json.RawMessage, error) {
	return d.do(cmd, args, evt, Timeout)
}

func (d *Conn) do(cmd string, args any, evt string, timeout time.Duration) (json.RawMessage, error) {
	var n [8]byte
	rand.Read(n[:])
	nonce := hex.EncodeToString(n[:])
	ch := make(chan Message, 1)
	d.mu.Lock()
	d.wait[nonce] = ch
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.wait, nonce)
		d.mu.Unlock()
	}()
	if err := d.write(opFrame, Message{Cmd: cmd, Args: args, Evt: evt, Nonce: nonce}); err != nil {
		return nil, err
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case m := <-ch:
		if m.Evt == "ERROR" {
			e := &Error{}
			json.Unmarshal(m.Data, e)
			return nil, e
		}
		return m.Data, nil
	case <-d.done:
		return nil, d.Err()
	case <-t.C:
		return nil, fmt.Errorf("discord: no reply to %s", cmd)
	}
}

func (d *Conn) write(op uint32, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f := make([]byte, 8, 8+len(b))
	binary.LittleEndian.PutUint32(f, op)
	binary.LittleEndian.PutUint32(f[4:], uint32(len(b)))
	d.wmu.Lock()
	defer d.wmu.Unlock()
	_, err = d.c.Write(append(f, b...))
	return err
}

func readFrame(r io.Reader) (op uint32, b []byte, err error) {
	var h [8]byte
	if _, err = io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	op, n := binary.LittleEndian.Uint32(h[:]), binary.LittleEndian.Uint32(h[4:])
	if n > maxFrame {
		return 0, nil, fmt.Errorf("discord: %d-byte frame", n)
	}
	b = make([]byte, n)
	_, err = io.ReadFull(r, b)
	return op, b, err
}
