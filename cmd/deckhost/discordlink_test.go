package main

import (
	"encoding/json"
	"maps"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/discord"
	"github.com/riptidewave93/d200-deck/internal/discord/discordtest"
	"github.com/riptidewave93/d200-deck/proto"
)

// startLink runs a link that logs in with login and returns the states it
// publishes.
func startLink(t *testing.T, login func() (*discord.Conn, error)) (*discordLink, chan map[string]bool) {
	states := make(chan map[string]bool, 32)
	l := newDiscordLink(login, func(s map[string]bool) { states <- s })
	l.retry = 10 * time.Millisecond
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go l.run(stop)
	return l, states
}

// waitState waits for a state with the given ready, mute and deaf.
func waitState(t *testing.T, states chan map[string]bool, ready, mute, deaf bool) {
	t.Helper()
	for {
		select {
		case s := <-states:
			if s["discord.ready"] == ready && s["discord.mute"] == mute && s["discord.deafen"] == deaf {
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no state with ready %v, mute %v, deaf %v", ready, mute, deaf)
		}
	}
}

func TestDiscordLink(t *testing.T) {
	fake := discordtest.New()
	l, states := startLink(t, fake.Dial)
	waitState(t, states, true, false, false)
	if s := l.statusText(); s != "connected" {
		t.Errorf("status %q", s)
	}

	steps := []struct {
		op         string
		release    bool
		mute, deaf bool
	}{
		{deck.DiscordMute, false, true, false},
		{deck.DiscordMute, true, true, false}, // letting go does nothing
		{deck.DiscordMute, false, false, false},
		{deck.DiscordDeafen, false, false, true},
		{deck.DiscordMute, false, false, false}, // undeafens
		{deck.DiscordDeafen, false, false, true},
		{deck.DiscordDeafen, false, false, false},
		{deck.DiscordPushToTalk, false, false, false},
		{deck.DiscordPushToTalk, true, true, false},
		{deck.DiscordPushToTalk, false, false, false},
		{deck.DiscordPushToMute, false, true, false},
		{deck.DiscordPushToMute, true, false, false},
	}
	for i, s := range steps {
		if err := l.do(s.op, nil, s.release); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if mute, deaf := fake.Voice(); mute != s.mute || deaf != s.deaf {
			t.Fatalf("step %d (%s, release %v): mute %v, deaf %v", i, s.op, s.release, mute, deaf)
		}
	}

	fake.SetVoice(false, true) // deafened in Discord: shown as muted too
	waitState(t, states, true, true, true)
	fake.SetVoice(true, true)

	fake.Close() // Discord quits: not ready and dark, then it reconnects
	waitState(t, states, false, false, false)
	waitState(t, states, true, true, true)
}

func TestDiscordLinkNotConnected(t *testing.T) {
	cases := []struct {
		err          error
		status, want string
	}{
		{discord.ErrNotRunning, "not running", "discord: not running"},
		{errNotSetUp, "", "discord: not set up"},
	}
	for _, tc := range cases {
		l, _ := startLink(t, func() (*discord.Conn, error) { return nil, tc.err })
		waitFor(t, "status "+tc.status, func() bool { return l.statusText() == tc.status })
		if err := l.do(deck.DiscordMute, nil, false); err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("do: %v, want %q", err, tc.want)
		}
	}
}

func TestDiscordLinkRefused(t *testing.T) {
	fake := discordtest.New()
	fake.Refuse = true
	var tries atomic.Int32
	l, _ := startLink(t, func() (*discord.Conn, error) {
		tries.Add(1)
		c, err := fake.Dial()
		if err != nil {
			return nil, err
		}
		if err := c.Authenticate(discord.Token{AccessToken: "x"}); err != nil {
			c.Close()
			return nil, err
		}
		return c, nil
	})
	waitFor(t, "refusal", func() bool { return strings.HasPrefix(l.statusText(), "not logged in") })
	time.Sleep(50 * time.Millisecond)
	if n := tries.Load(); n != 1 {
		t.Fatalf("asked %d times without a key press", n)
	}
	if err := l.do(deck.DiscordMute, nil, false); err == nil {
		t.Error("do worked while refused")
	}
	waitFor(t, "second try", func() bool { return tries.Load() == 2 })
}

func TestDaemonDiscord(t *testing.T) {
	fake := discordtest.New()
	dev, results, states := fakeD200(proto.Version)
	defer dev.Close()
	d := newDaemon()
	link := newDiscordLink(fake.Dial, d.setState)
	d.apps[deck.AppDiscord] = link
	stop := make(chan struct{})
	defer close(stop)
	go link.run(stop)
	d.find = func() []string { return []string{"/dev/hidraw4"} }
	d.open = func(string) (proto.Transport, error) { return dev, nil }
	d.scan()

	seen := map[string]bool{} // as the deck has it: a name not sent is off
	waitDeck := func(mute bool) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for !seen["discord.ready"] || seen["discord.mute"] != mute {
			select {
			case s := <-states:
				if s.Host != d.id {
					t.Fatalf("state from host %q", s.Host)
				}
				maps.Copy(seen, s.State)
			case <-deadline:
				t.Fatalf("deck never told mute %v", mute)
			}
		}
	}
	waitDeck(false)

	press := func(release bool, op string) {
		b, _ := json.Marshal(proto.ActionEvent{ID: 7, Host: d.id, Key: 2, Release: release,
			Action: deck.Action{Type: deck.ActionApp, App: deck.AppDiscord, Op: op}})
		dev.Send(proto.EventSeq, proto.EventAction, b)
	}
	press(false, deck.DiscordMute)
	select {
	case r := <-results:
		if r.ID != 7 || r.Error != "" {
			t.Errorf("result %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no result")
	}
	waitDeck(true)

	press(false, deck.DiscordPushToTalk) // talk: unmuted until let go
	waitDeck(false)
	press(true, deck.DiscordPushToTalk)
	waitDeck(true)

	fake.SetVoice(false, false)
	waitDeck(false)
	if s := d.getStatus(); len(s.Apps) != 1 || s.Apps[0] != (AppStatus{"Discord", "connected"}) {
		t.Errorf("status %+v", s.Apps)
	}

	go func() {
		for range results {
		}
	}()
	for range 20 { // quick taps end muted
		fake.SetVoice(false, false)
		press(false, deck.DiscordPushToTalk)
		press(true, deck.DiscordPushToTalk)
		waitFor(t, "tap", func() bool { m, _ := fake.Voice(); return m })
	}
	time.Sleep(20 * time.Millisecond)
	if m, _ := fake.Voice(); !m {
		t.Error("unmuted after taps")
	}
}

func TestUnknownApp(t *testing.T) {
	d := newDaemon()
	if err := d.runAction(deck.Action{Type: deck.ActionApp, App: "zoom", Op: "mute"}, false); err == nil || !strings.Contains(err.Error(), `no "zoom" app`) {
		t.Errorf("unknown app: %v", err)
	}
	l := newDiscordLink(nil, nil)
	if err := l.do("dance", nil, false); err == nil || !strings.Contains(err.Error(), `no "dance" op`) {
		t.Errorf("unknown op: %v", err)
	}
	if err := d.runAction(deck.Action{Type: deck.ActionHTTP}, true); err != nil {
		t.Errorf("release of a non-app action: %v", err)
	}
}

// waitKey waits for a state that sets key to v.
func waitKey(t *testing.T, states chan map[string]bool, key string, v bool) {
	t.Helper()
	for {
		select {
		case s := <-states:
			if got, ok := s[key]; ok && got == v {
				return
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no state with %s %v", key, v)
		}
	}
}

func TestDiscordSettings(t *testing.T) {
	fake := discordtest.New()
	l, states := startLink(t, fake.Dial)
	waitKey(t, states, "discord.ready", true)
	do := func(op string, args string) {
		t.Helper()
		var raw json.RawMessage
		if args != "" {
			raw = json.RawMessage(args)
		}
		if err := l.do(op, raw, false); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}

	do(deck.DiscordVoiceMode, "")
	waitKey(t, states, "discord.voice-mode", true)
	do(deck.DiscordVoiceMode, "")
	waitKey(t, states, "discord.voice-mode", false)
	for _, op := range []string{deck.DiscordNoiseSuppression, deck.DiscordEchoCancellation, deck.DiscordAutoGain} {
		do(op, "")
		waitKey(t, states, "discord."+op, true)
	}
	if v := fake.Settings(); !v.NoiseSuppression || !v.EchoCancellation || !v.AutomaticGainControl {
		t.Errorf("settings %+v", v)
	}

	volumes := []struct {
		op, args string
		in, out  float64
	}{
		{deck.DiscordInputVolumeUp, "", 100, 100}, // already at the top
		{deck.DiscordInputVolumeDown, "", 90, 100},
		{deck.DiscordInputVolumeDown, `{"step": 25}`, 65, 100},
		{deck.DiscordOutputVolumeUp, "", 65, 110},
		{deck.DiscordOutputVolumeUp, `{"step": 500}`, 65, 200},
		{deck.DiscordOutputVolumeDown, `{"step": 500}`, 65, 0},
	}
	for _, v := range volumes {
		do(v.op, v.args)
		if s := fake.Settings(); s.Input.Volume != v.in || s.Output.Volume != v.out {
			t.Errorf("%s %s: input %v, output %v", v.op, v.args, s.Input.Volume, s.Output.Volume)
		}
	}
}

func TestDiscordVoiceChannel(t *testing.T) {
	fake := discordtest.New()
	l, states := startLink(t, fake.Dial)
	waitKey(t, states, "discord.ready", true)
	join := func(id string) error { return l.do(deck.DiscordJoinVoice, json.RawMessage(`{"id": "`+id+`"}`), false) }

	if err := l.do(deck.DiscordLeaveVoice, nil, false); err != nil {
		t.Errorf("leaving with no channel: %v", err)
	}
	if err := join("123"); err != nil || fake.Channel() != "123" {
		t.Fatalf("join: %v, in %q", err, fake.Channel())
	}
	waitKey(t, states, "discord.join-voice:123", true)
	if err := join("456"); err != nil || fake.Channel() != "456" {
		t.Fatalf("move: %v, in %q", err, fake.Channel())
	}
	waitKey(t, states, "discord.join-voice:123", false)
	if err := join("456"); err != nil || fake.Channel() != "" { // pressed again: leave
		t.Fatalf("join again: %v, in %q", err, fake.Channel())
	}
	waitKey(t, states, "discord.leave-voice", false)

	fake.SetChannel("789") // joined in Discord
	waitKey(t, states, "discord.join-voice:789", true)
	if err := l.do(deck.DiscordLeaveVoice, nil, false); err != nil || fake.Channel() != "" {
		t.Errorf("leave: %v, in %q", err, fake.Channel())
	}
	waitKey(t, states, "discord.join-voice:789", false)

	if err := join("general"); err == nil {
		t.Error("joined a channel that isn't an ID")
	}
}

func TestDiscordLinks(t *testing.T) {
	urls := fakeBrowser(t)
	l := newDiscordLink(nil, nil) // never logged in: links work anyway
	for _, c := range []struct{ op, args string }{
		{deck.DiscordOpen, ""},
		{deck.DiscordChannel, `{"id": "22"}`},
		{deck.DiscordChannel, `{"id": "22", "guild": "11"}`},
	} {
		var raw json.RawMessage
		if c.args != "" {
			raw = json.RawMessage(c.args)
		}
		if err := l.do(c.op, raw, false); err != nil {
			t.Errorf("%s %s: %v", c.op, c.args, err)
		}
	}
	want := "discord://-/channels/@me discord://-/channels/@me/22 discord://-/channels/11/22"
	if got := strings.Join(urls(), " "); got != want {
		t.Errorf("opened %s", got)
	}
	for _, args := range []string{`{}`, `{"id": "x"}`, `{"id": "1", "guild": "../x"}`, `[`} {
		if err := l.do(deck.DiscordChannel, json.RawMessage(args), false); err == nil {
			t.Errorf("channel %s worked", args)
		}
	}
}
