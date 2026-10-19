package main

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/riptidewave93/d200-deck/deck"
)

// Media players on Linux: MPRIS on the session bus.

const (
	mprisPrefix = "org.mpris.MediaPlayer2."
	mprisPath   = "/org/mpris/MediaPlayer2"
	mprisPlayer = "org.mpris.MediaPlayer2.Player"
)

type mpris struct{ conn *dbus.Conn }

func openMedia() (mediaBackend, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	return &mpris{conn}, nil
}

func (m *mpris) close() { m.conn.Close() }

func (m *mpris) players() ([]mediaPlayer, error) {
	var names []string
	if err := m.conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return nil, err
	}
	slices.Sort(names)
	var ps []mediaPlayer
	for _, n := range names {
		if !strings.HasPrefix(n, mprisPrefix) {
			continue
		}
		var props map[string]dbus.Variant
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		err := m.conn.Object(n, mprisPath).CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, mprisPlayer).Store(&props)
		cancel()
		if err != nil {
			continue // a player that doesn't answer isn't one we can control
		}
		p := mediaPlayer{ID: n, Name: shortName(strings.TrimPrefix(n, mprisPrefix))}
		if v, ok := props["PlaybackStatus"].Value().(string); ok {
			p.Playing = v == "Playing"
		}
		p.Shuffle, _ = props["Shuffle"].Value().(bool)
		if v, ok := props["LoopStatus"].Value().(string); ok {
			p.Repeat = v != "" && v != "None"
		}
		ps = append(ps, p)
	}
	return ps, nil
}

func (m *mpris) control(p mediaPlayer, op string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	obj := m.conn.Object(p.ID, mprisPath)
	set := func(prop string, v any) error {
		return obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Set", 0, mprisPlayer, prop, dbus.MakeVariant(v)).Err
	}
	switch op {
	case deck.MediaShuffle:
		return set("Shuffle", !p.Shuffle)
	case deck.MediaRepeat:
		loop := "Playlist"
		if p.Repeat {
			loop = "None"
		}
		return set("LoopStatus", loop)
	}
	method := map[string]string{
		deck.MediaPlayPause: "PlayPause", deck.MediaNext: "Next",
		deck.MediaPrevious: "Previous", deck.MediaStop: "Stop",
	}[op]
	return obj.CallWithContext(ctx, mprisPlayer+"."+method, 0).Err
}
