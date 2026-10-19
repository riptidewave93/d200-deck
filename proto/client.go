package proto

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Transport moves raw reports to and from the device.
type Transport interface {
	// WriteReport sends one output report (RawOutSize bytes).
	WriteReport(b []byte) error
	// ReadReport waits for one input report; Close makes it return.
	ReadReport(b []byte) (int, error)
	Close() error
}

// ReplyTimeout is how long Do waits for the device.
var ReplyTimeout = 2 * time.Second

// Event is a message the device sent on its own.
type Event struct {
	Code    byte
	Payload []byte
}

type reply struct {
	seq     uint16
	status  byte
	payload []byte
}

// Conn is a connection to the config channel: requests one at a time with
// Do, and events from the device on Events.
type Conn struct {
	t       Transport
	mu      sync.Mutex // one request in flight
	seq     uint16
	replies chan reply
	events  chan Event
	// synced is set by the first reply: events queued in the device before
	// we connected arrive ahead of it and are dropped.
	synced atomic.Bool
	done   chan struct{}
	err    error
}

// NewConn starts reading from t.
func NewConn(t Transport) *Conn {
	var b [2]byte
	rand.Read(b[:]) // other clients see our replies: don't start where they do
	c := &Conn{t: t, seq: binary.LittleEndian.Uint16(b[:]), replies: make(chan reply, 4), events: make(chan Event, 16), done: make(chan struct{})}
	go c.read()
	return c
}

// Events delivers events from the device. Events that nobody receives in
// time are dropped.
func (c *Conn) Events() <-chan Event { return c.events }

// Done is closed when the connection fails; Err then says why.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err is why the connection failed.
func (c *Conn) Err() error { return c.err }

// Close closes the transport.
func (c *Conn) Close() error { return c.t.Close() }

// partial is a message being reassembled.
type partial struct {
	b        []byte
	tooLarge bool // over MaxMessage: dropped when it ends
}

func (c *Conn) read() {
	defer close(c.done)
	parts := map[uint16]*partial{}
	buf := make([]byte, RawInSize)
	for {
		n, err := c.t.ReadReport(buf)
		if err != nil {
			c.err = err
			return
		}
		p, err := DecodeReply(buf[:n])
		if err != nil {
			continue
		}
		if p.First {
			parts[p.Seq] = &partial{} // drops what's left of a message cut short
		}
		m := parts[p.Seq]
		if m == nil {
			continue // we missed the start
		}
		if !m.tooLarge {
			m.b = append(m.b, p.Payload...)
			if len(m.b) > MaxMessage {
				m.b, m.tooLarge = nil, true
			}
		}
		if p.More {
			continue
		}
		delete(parts, p.Seq)
		if m.tooLarge {
			continue
		}
		msg := m.b
		if p.Seq == EventSeq {
			if c.synced.Load() {
				select {
				case c.events <- Event{Code: p.Status, Payload: msg}:
				default:
				}
			}
			continue
		}
		select {
		case c.replies <- reply{p.Seq, p.Status, msg}:
		default:
		}
	}
}

// Do sends a request and returns the reply payload.
func (c *Conn) Do(cmd byte, payload []byte) ([]byte, error) {
	return c.DoWait(cmd, payload, ReplyTimeout)
}

// DoWait is Do, waiting up to wait for the reply: for requests the device
// may ask about first.
func (c *Conn) DoWait(cmd byte, payload []byte, wait time.Duration) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	if c.seq == EventSeq {
		c.seq++
	}
	req, err := EncodeRequest(c.seq, cmd, payload)
	if err != nil {
		return nil, err
	}
	if err := c.t.WriteReport(req); err != nil {
		return nil, err
	}
	timeout := time.NewTimer(wait)
	defer timeout.Stop()
	for {
		select {
		case r := <-c.replies:
			if r.seq != c.seq {
				continue // a late reply to an earlier request
			}
			c.synced.Store(true)
			if r.status == StatusFailed {
				return nil, fmt.Errorf("device: %s", r.payload)
			}
			if r.status != StatusOK {
				return nil, fmt.Errorf("device: status %d", r.status)
			}
			return r.payload, nil
		case <-c.done:
			if c.err == nil {
				return nil, io.EOF
			}
			return nil, c.err
		case <-timeout.C:
			return nil, errors.New("device didn't reply")
		}
	}
}
