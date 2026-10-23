package main

import (
	"errors"
	"net"

	"github.com/jfreymuth/pulse/proto"
)

// The mic on Linux: the default source, through the PulseAudio protocol,
// which PipeWire speaks too.

type pulseMic struct {
	c    *proto.Client
	conn net.Conn
}

func openMic() (micBackend, error) {
	c, conn, err := proto.Connect("")
	if err != nil {
		return nil, err
	}
	name := proto.PropList{"application.name": proto.PropListString("deckhost")}
	if err := c.Request(&proto.SetClientName{Props: name}, &proto.SetClientNameReply{}); err != nil {
		conn.Close()
		return nil, err
	}
	return &pulseMic{c, conn}, nil
}

func (p *pulseMic) close() { p.conn.Close() }

// source is the default source, if it's a microphone rather than
// the sound playing (a monitor).
func (p *pulseMic) source() (*proto.GetSourceInfoReply, error) {
	var server proto.GetServerInfoReply
	if err := p.c.Request(&proto.GetServerInfo{}, &server); err != nil {
		return nil, err
	}
	var s proto.GetSourceInfoReply
	err := p.c.Request(&proto.GetSourceInfo{SourceIndex: proto.Undefined, SourceName: server.DefaultSourceName}, &s)
	if errors.Is(err, proto.ErrNoSuchEntity) || err == nil && s.MonitorSourceIndex != proto.Undefined {
		return nil, errNoMic
	}
	return &s, err
}

func (p *pulseMic) muted() (bool, error) {
	s, err := p.source()
	if err != nil {
		return false, err
	}
	return s.Mute, nil
}

func (p *pulseMic) setMuted(on bool) error {
	s, err := p.source()
	if err != nil {
		return err
	}
	return p.c.Request(&proto.SetSourceMute{SourceIndex: s.SourceIndex, Mute: on}, nil)
}
