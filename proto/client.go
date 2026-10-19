package proto

import "fmt"

// Transport moves raw reports to and from the device.
type Transport interface {
	// WriteReport sends one output report (RawOutSize bytes).
	WriteReport(b []byte) error
	// ReadReport receives one input report.
	ReadReport(b []byte) (int, error)
}

// Client sends requests over a Transport, one at a time.
type Client struct {
	t   Transport
	seq byte
}

// NewClient returns a client for t.
func NewClient(t Transport) *Client { return &Client{t: t} }

// Do sends a request and returns the reply payload.
func (c *Client) Do(cmd byte, payload []byte) ([]byte, error) {
	c.seq++
	req, err := EncodeRequest(c.seq, cmd, payload)
	if err != nil {
		return nil, err
	}
	if err := c.t.WriteReport(req); err != nil {
		return nil, err
	}
	var reply []byte
	buf := make([]byte, RawInSize)
	for {
		n, err := c.t.ReadReport(buf)
		if err != nil {
			return nil, err
		}
		p, err := DecodeReply(buf[:n])
		if err != nil {
			return nil, err
		}
		if p.Seq != c.seq {
			continue // a late reply to an earlier request
		}
		if p.Status != StatusOK {
			return nil, fmt.Errorf("device: status %d", p.Status)
		}
		reply = append(reply, p.Payload...)
		if !p.More {
			return reply, nil
		}
	}
}
