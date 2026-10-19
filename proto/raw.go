// Package proto is the config channel protocol between the D200 and its
// clients (the website over WebHID, the PC app over hidraw/HID API).
//
// Requests are output reports of RawOutSize bytes:
//
//	[0] seq  [1] cmd  [2:4] payload length (LE)  [4:] payload
//
// Each request is answered by one or more input reports of RawInSize bytes:
//
//	[0] seq  [1] status  [2] flags (FlagMore: another report follows)
//	[3] payload length  [4:] payload
//
// One request is in flight at a time: the device keeps only the latest
// unread output report.
//
// The device also sends events on its own: input reports with seq EventSeq
// and an event code in place of the status. Requests never use EventSeq.
// Reports are reassembled per seq, so events and replies may interleave.
package proto

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/osrelease"
)

// Report sizes; they match the raw HID report descriptor.
const (
	RawInSize  = 64
	RawOutSize = 1024

	MaxRequest = RawOutSize - 4
	MaxReply   = RawInSize - 4

	// MaxMessage bounds a reassembled reply or event.
	MaxMessage = 64 << 10
)

// Version is the protocol version reported by CmdHello.
const Version = 2

// Commands.
const (
	CmdHello    = 0x01 // reply: Hello as JSON
	CmdIdentify = 0x03 // payload: key number; flashes the key
	CmdResult   = 0x04 // payload: Result as JSON
	// CmdActionHost says the sender runs PC actions. The action host sends
	// it every ActionHostPeriod; the device sends EventAction only while it
	// has heard one within ActionHostTimeout.
	CmdActionHost = 0x05
)

// Action host heartbeat timing.
const (
	ActionHostPeriod  = 2 * time.Second
	ActionHostTimeout = 5 * time.Second
)

// EventSeq is the seq of events.
const EventSeq = 0

// Events.
const (
	EventAction = 0x01 // payload: ActionEvent as JSON
)

// Reply statuses.
const (
	StatusOK         = 0x00
	StatusUnknownCmd = 0x01
	StatusBadRequest = 0x02
)

// FlagMore marks a reply report that is followed by another.
const FlagMore = 0x01

// Hello describes the device.
type Hello struct {
	Protocol int            `json:"protocol"`
	Firmware string         `json:"firmware"` // deckd version
	OS       osrelease.Info `json:"os"`
	Serial   string         `json:"serial"`
	Keys     int            `json:"keys"`
}

// ActionEvent asks the action host to run a PC action. The host answers
// with CmdResult and the same ID.
type ActionEvent struct {
	ID     uint32      `json:"id"`
	Key    int         `json:"key"`
	Action deck.Action `json:"action"`
}

// Result says how a PC action went; Error is empty on success.
type Result struct {
	ID    uint32 `json:"id"`
	Error string `json:"error,omitempty"`
}

// Request is a decoded request.
type Request struct {
	Seq     byte
	Cmd     byte
	Payload []byte
}

// EncodeRequest builds an output report.
func EncodeRequest(seq, cmd byte, payload []byte) ([]byte, error) {
	if len(payload) > MaxRequest {
		return nil, errors.New("request too long")
	}
	b := make([]byte, RawOutSize)
	b[0], b[1] = seq, cmd
	binary.LittleEndian.PutUint16(b[2:], uint16(len(payload)))
	copy(b[4:], payload)
	return b, nil
}

// DecodeRequest parses an output report.
func DecodeRequest(b []byte) (Request, error) {
	if len(b) < 4 {
		return Request{}, errors.New("short request")
	}
	n := int(binary.LittleEndian.Uint16(b[2:]))
	if n > len(b)-4 {
		return Request{}, errors.New("bad request length")
	}
	return Request{Seq: b[0], Cmd: b[1], Payload: b[4 : 4+n]}, nil
}

// EncodeReply splits a reply into input reports.
func EncodeReply(seq, status byte, payload []byte) [][]byte {
	var out [][]byte
	for {
		n := min(len(payload), MaxReply)
		b := make([]byte, RawInSize)
		b[0], b[1], b[3] = seq, status, byte(n)
		copy(b[4:], payload[:n])
		payload = payload[n:]
		if len(payload) > 0 {
			b[2] = FlagMore
		}
		out = append(out, b)
		if len(payload) == 0 {
			return out
		}
	}
}

// ReplyPart is one decoded input report.
type ReplyPart struct {
	Seq, Status byte
	More        bool
	Payload     []byte
}

// DecodeReply parses an input report.
func DecodeReply(b []byte) (ReplyPart, error) {
	if len(b) < 4 || int(b[3]) > len(b)-4 {
		return ReplyPart{}, errors.New("bad reply")
	}
	return ReplyPart{Seq: b[0], Status: b[1], More: b[2]&FlagMore != 0, Payload: b[4 : 4+int(b[3])]}, nil
}
