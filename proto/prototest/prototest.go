// Package prototest is an in-memory config channel for tests: a
// proto.Transport for the client, with the test playing the device.
package prototest

import (
	"errors"
	"sync"

	"github.com/riptidewave93/d200-deck/proto"
)

// Device is the device end of an in-memory config channel.
type Device struct {
	V3       bool // send protocol 3 reports
	in       chan []byte
	requests chan proto.Request
	closed   chan struct{}
	once     sync.Once
	mu       sync.Mutex
	lastSeq  uint16
}

// ErrClosed is returned once the transport is closed.
var ErrClosed = errors.New("closed")

// New returns an open channel.
func New() *Device {
	return &Device{in: make(chan []byte, 256), requests: make(chan proto.Request, 16), closed: make(chan struct{})}
}

// WriteReport takes a request from the client.
func (d *Device) WriteReport(b []byte) error {
	req, err := proto.DecodeRequest(b)
	if err != nil {
		return err
	}
	req.Payload = append([]byte(nil), req.Payload...)
	d.mu.Lock()
	d.lastSeq = req.Seq
	d.mu.Unlock()
	select {
	case d.requests <- req:
		return nil
	case <-d.closed:
		return ErrClosed
	}
}

// ReadReport gives the client the next input report.
func (d *Device) ReadReport(b []byte) (int, error) {
	select {
	case r := <-d.in:
		return copy(b, r), nil
	case <-d.closed:
		return 0, ErrClosed
	}
}

// Close ends the connection.
func (d *Device) Close() error {
	d.once.Do(func() { close(d.closed) })
	return nil
}

// LastSeq is the seq of the last request.
func (d *Device) LastSeq() uint16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastSeq
}

// Requests delivers the client's requests.
func (d *Device) Requests() <-chan proto.Request { return d.requests }

// Send sends a message as input reports: 64-byte ones of protocol 3 if V3
// is set.
func (d *Device) Send(seq uint16, status byte, payload []byte) {
	encode := proto.EncodeReply
	if d.V3 {
		encode = proto.EncodeReplyV3
	}
	for _, r := range encode(seq, status, payload) {
		d.in <- r
	}
}

// Raw sends one input report as is.
func (d *Device) Raw(report []byte) { d.in <- report }

// Serve answers every request with h until the channel is closed.
func (d *Device) Serve(h func(proto.Request) (status byte, reply []byte)) {
	for {
		select {
		case req := <-d.requests:
			status, reply := h(req)
			d.Send(req.Seq, status, reply)
		case <-d.closed:
			return
		}
	}
}
