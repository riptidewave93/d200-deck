package proto_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/proto"
	"github.com/riptidewave93/d200-deck/proto/prototest"
)

func echo(req proto.Request) (byte, []byte) { return proto.StatusOK, req.Payload }

func TestDo(t *testing.T) {
	dev := prototest.New()
	go dev.Serve(echo)
	c := proto.NewConn(dev)
	defer c.Close()
	for _, n := range []int{0, 10, 300, proto.MaxRequest} {
		payload := bytes.Repeat([]byte{byte(n)}, n)
		got, err := c.Do(proto.CmdHello, payload)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("%d bytes: got %d bytes back", n, len(got))
		}
	}
}

func TestDoStatus(t *testing.T) {
	dev := prototest.New()
	go dev.Serve(func(proto.Request) (byte, []byte) { return proto.StatusUnknownCmd, nil })
	c := proto.NewConn(dev)
	defer c.Close()
	if _, err := c.Do(0x7f, nil); err == nil {
		t.Error("no error for a failed request")
	}
}

func TestRandomFirstSeq(t *testing.T) {
	// clients see each other's replies: they mustn't all count from the same seq
	first := func() uint16 {
		dev := prototest.New()
		go dev.Serve(echo)
		c := proto.NewConn(dev)
		defer c.Close()
		c.Do(proto.CmdHello, nil)
		return dev.LastSeq()
	}
	if a, b, c := first(), first(), first(); a == b && b == c {
		t.Errorf("three clients all started at seq %d", a)
	}
}

func TestLateReplyIgnored(t *testing.T) {
	dev := prototest.New()
	c := proto.NewConn(dev)
	defer c.Close()
	go func() {
		req := <-dev.Requests()
		dev.Send(req.Seq-1, proto.StatusOK, []byte("stale"))
		dev.Send(req.Seq, proto.StatusOK, []byte("fresh"))
	}()
	got, err := c.Do(proto.CmdHello, nil)
	if err != nil || string(got) != "fresh" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestTimeout(t *testing.T) {
	old := proto.ReplyTimeout
	proto.ReplyTimeout = 50 * time.Millisecond
	defer func() { proto.ReplyTimeout = old }()
	dev := prototest.New()
	c := proto.NewConn(dev)
	defer c.Close()
	go func() { <-dev.Requests() }()
	if _, err := c.Do(proto.CmdHello, nil); err == nil {
		t.Error("no error without a reply")
	}
}

func TestEvents(t *testing.T) {
	dev := prototest.New()
	c := proto.NewConn(dev)
	defer c.Close()

	// queued before the first reply: dropped
	dev.Send(proto.EventSeq, proto.EventAction, []byte("stale"))
	go func() {
		req := <-dev.Requests()
		dev.Send(req.Seq, proto.StatusOK, nil)
	}()
	if _, err := c.Do(proto.CmdHello, nil); err != nil {
		t.Fatal(err)
	}

	big := bytes.Repeat([]byte("x"), 3*proto.MaxReply)
	dev.Send(proto.EventSeq, proto.EventAction, big)
	select {
	case ev := <-c.Events():
		if ev.Code != proto.EventAction || !bytes.Equal(ev.Payload, big) {
			t.Errorf("got event %d with %d bytes", ev.Code, len(ev.Payload))
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	select {
	case ev := <-c.Events():
		t.Errorf("unexpected event %q", ev.Payload)
	default:
	}
}

func TestInterleaved(t *testing.T) {
	dev := prototest.New()
	c := proto.NewConn(dev)
	defer c.Close()
	go func() {
		req := <-dev.Requests()
		dev.Send(req.Seq, proto.StatusOK, nil) // sync
		req = <-dev.Requests()
		reply := proto.EncodeReply(req.Seq, proto.StatusOK, bytes.Repeat([]byte("r"), 2*proto.MaxReply))
		event := proto.EncodeReply(proto.EventSeq, proto.EventAction, bytes.Repeat([]byte("e"), 2*proto.MaxReply))
		for i := range reply {
			dev.Raw(event[i])
			dev.Raw(reply[i])
		}
	}()
	c.Do(proto.CmdHello, nil)
	got, err := c.Do(proto.CmdHello, nil)
	if err != nil || !bytes.Equal(got, bytes.Repeat([]byte("r"), 2*proto.MaxReply)) {
		t.Fatalf("reply %q, %v", got, err)
	}
	ev := <-c.Events()
	if !bytes.Equal(ev.Payload, bytes.Repeat([]byte("e"), 2*proto.MaxReply)) {
		t.Errorf("event %q", ev.Payload)
	}
}

func TestBadReportsSkipped(t *testing.T) {
	dev := prototest.New()
	c := proto.NewConn(dev)
	defer c.Close()
	go func() {
		req := <-dev.Requests()
		dev.Raw([]byte{1, 2})
		dev.Send(req.Seq, proto.StatusOK, []byte("ok"))
	}()
	if got, err := c.Do(proto.CmdHello, nil); err != nil || string(got) != "ok" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestOversizedMessageDropped(t *testing.T) {
	dev := prototest.New()
	c := proto.NewConn(dev)
	defer c.Close()
	go func() {
		req := <-dev.Requests()
		dev.Send(req.Seq, proto.StatusOK, make([]byte, proto.MaxMessage+proto.MaxReply))
		dev.Send(req.Seq, proto.StatusOK, []byte("ok"))
	}()
	if got, err := c.Do(proto.CmdHello, nil); err != nil || string(got) != "ok" {
		t.Errorf("got %d bytes, %v", len(got), err)
	}
}

func TestCutShortMessage(t *testing.T) {
	dev := prototest.New()
	c := proto.NewConn(dev)
	defer c.Close()
	go func() {
		req := <-dev.Requests()
		long := proto.EncodeReply(req.Seq, proto.StatusOK, bytes.Repeat([]byte("x"), 3*proto.MaxReply))
		dev.Raw(long[1]) // the middle of a message whose start we missed: ignored
		dev.Raw(long[0]) // the start of a message the device gave up on
		dev.Send(req.Seq, proto.StatusOK, []byte("ok"))
	}()
	if got, err := c.Do(proto.CmdHello, nil); err != nil || string(got) != "ok" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestClosed(t *testing.T) {
	dev := prototest.New()
	c := proto.NewConn(dev)
	dev.Close()
	<-c.Done()
	if !errors.Is(c.Err(), prototest.ErrClosed) {
		t.Errorf("Err() = %v", c.Err())
	}
	if _, err := c.Do(proto.CmdHello, nil); err == nil {
		t.Error("Do worked on a closed connection")
	}
}
