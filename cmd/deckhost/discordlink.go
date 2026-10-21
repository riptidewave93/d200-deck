package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/discord"
)

// discordLink keeps deckhost logged in to Discord, follows the user's voice
// settings and runs discord actions.
type discordLink struct {
	login   func() (*discord.Conn, error)
	publish func(map[string]bool) // the state for the decks
	retry   time.Duration         // between tries while Discord isn't running

	mu      sync.Mutex
	conn    *discord.Conn // nil while not connected
	voice   discord.VoiceSettings
	channel string // the voice channel's ID, "" if none
	shown   string // the voice channel the decks were last told of
	status  string // for the tray; "" when Discord isn't set up
	kick    chan struct{}
}

func newDiscordLink(login func() (*discord.Conn, error), publish func(map[string]bool)) *discordLink {
	return &discordLink{login: login, publish: publish, retry: 5 * time.Second,
		status: "connecting", kick: make(chan struct{}, 1)}
}

// run connects to Discord, again whenever the connection ends, until stop
// is closed. If the user refuses deckhost (or doesn't answer Discord's
// prompt), it waits for a Discord key without a fallback, or a restart,
// before trying again.
func (l *discordLink) run(stop <-chan struct{}) {
	for {
		conn, err := l.login()
		var wait <-chan time.Time
		switch {
		case err == nil:
			l.follow(conn)
			wait = time.After(l.retry)
		case errors.Is(err, errNotSetUp):
			l.setStatus("")
			wait = time.After(l.retry) // until the user sets it up
		case errors.Is(err, discord.ErrNotRunning):
			l.setStatus("not running")
			wait = time.After(l.retry)
		default:
			log.Printf("discord: %v", err)
			l.setStatus("not logged in: Restart to try again")
		}
		select {
		case <-stop:
			return
		case <-l.kick:
		case <-wait:
		}
	}
}

func (l *discordLink) setStatus(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.status = s
}

// statusText is how the link is doing, for the tray.
func (l *discordLink) statusText() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.status
}

// follow tracks the voice settings and voice channel on conn until it
// ends.
func (l *discordLink) follow(conn *discord.Conn) {
	v, err := conn.VoiceSettings()
	var ch struct {
		ID string `json:"id"`
	}
	if err == nil {
		var data json.RawMessage
		if data, err = conn.Do("GET_SELECTED_VOICE_CHANNEL", nil, ""); err == nil {
			json.Unmarshal(data, &ch) // null when in none
		}
	}
	for _, evt := range []string{"VOICE_SETTINGS_UPDATE", "VOICE_CHANNEL_SELECT"} {
		if err == nil {
			_, err = conn.Do("SUBSCRIBE", nil, evt)
		}
	}
	if err != nil {
		conn.Close()
		l.setStatus(err.Error())
		return
	}
	l.mu.Lock()
	l.conn, l.voice, l.channel, l.status = conn, v, ch.ID, "connected"
	l.publishLocked()
	l.mu.Unlock()
	log.Print("discord: connected")
	for e := range conn.Events() {
		l.mu.Lock()
		switch e.Evt {
		case "VOICE_SETTINGS_UPDATE":
			json.Unmarshal(e.Data, &l.voice) // updates may leave fields out
		case "VOICE_CHANNEL_SELECT":
			var c struct {
				ChannelID string `json:"channel_id"`
			}
			json.Unmarshal(e.Data, &c)
			l.channel = c.ChannelID
		}
		l.publishLocked()
		l.mu.Unlock()
	}
	log.Printf("discord: disconnected: %v", conn.Err())
	l.mu.Lock()
	l.conn, l.voice, l.channel, l.status = nil, discord.VoiceSettings{}, "", "not running"
	l.publishLocked()
	l.mu.Unlock()
}

// publishLocked tells the decks whether Discord can be used, and its state.
func (l *discordLink) publishLocked() {
	v := l.voice
	s := map[string]bool{
		"discord.ready":             l.conn != nil,
		"discord.mute":              v.Mute || v.Deaf, // deafened is muted too, as Discord shows it
		"discord.deafen":            v.Deaf,
		"discord.voice-mode":        v.Mode.Type == "PUSH_TO_TALK",
		"discord.noise-suppression": v.NoiseSuppression,
		"discord.echo-cancellation": v.EchoCancellation,
		"discord.auto-gain":         v.AutomaticGainControl,
		"discord.leave-voice":       l.channel != "",
	}
	if l.shown != "" && l.shown != l.channel {
		s["discord.join-voice:"+l.shown] = false
	}
	if l.channel != "" {
		s["discord.join-voice:"+l.channel] = true
	}
	l.shown = l.channel
	l.publish(s)
}

func (l *discordLink) title() string { return "Discord" }

func (l *discordLink) ops() []string { return deck.DiscordOps }

// discordArgs are the args Discord ops take.
type discordArgs struct {
	ID    string  `json:"id"`    // a channel
	Guild string  `json:"guild"` // the channel's server
	Step  float64 `json:"step"`  // volume change
}

// do runs Discord operation op for a key's press, or its release.
func (l *discordLink) do(op string, rawArgs json.RawMessage, release bool) error {
	if !slices.Contains(deck.DiscordOps, op) {
		return fmt.Errorf("discord has no %q op", op)
	}
	var args discordArgs
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return fmt.Errorf("discord %s: bad args: %w", op, err)
		}
	}
	held := op == deck.DiscordPushToTalk || op == deck.DiscordPushToMute
	if release && !held {
		return nil
	}
	switch op { // links: Discord needn't be logged in, or even running
	case deck.DiscordOpen:
		return openURL("discord://-/channels/@me")
	case deck.DiscordChannel:
		guild := args.Guild
		if guild == "" {
			guild = "@me"
		}
		if !isSnowflake(args.ID) || (guild != "@me" && !isSnowflake(guild)) {
			return errors.New("discord channel needs args id, and guild for a server's channel: Discord IDs")
		}
		return openURL("discord://-/channels/" + guild + "/" + args.ID)
	}

	l.mu.Lock()
	conn, v, channel, status := l.conn, l.voice, l.channel, l.status
	l.mu.Unlock()
	if conn == nil {
		if status == "" {
			status = errNotSetUp.Error()
		}
		if !release {
			select {
			case l.kick <- struct{}{}:
			default:
			}
		}
		return fmt.Errorf("discord: %s", status)
	}

	step := args.Step
	if step == 0 {
		step = 10
	}
	var change map[string]any
	switch op {
	case deck.DiscordMute:
		if v.Deaf {
			change = map[string]any{"deaf": false, "mute": false} // as clicking the mic does
		} else {
			change = map[string]any{"mute": !v.Mute}
		}
	case deck.DiscordDeafen:
		change = map[string]any{"deaf": !v.Deaf}
	case deck.DiscordPushToTalk:
		change = map[string]any{"mute": release}
	case deck.DiscordPushToMute:
		change = map[string]any{"mute": !release}
	case deck.DiscordVoiceMode:
		mode := "PUSH_TO_TALK"
		if v.Mode.Type == mode {
			mode = "VOICE_ACTIVITY"
		}
		change = map[string]any{"mode": map[string]string{"type": mode}}
	case deck.DiscordNoiseSuppression:
		change = map[string]any{"noise_suppression": !v.NoiseSuppression}
	case deck.DiscordEchoCancellation:
		change = map[string]any{"echo_cancellation": !v.EchoCancellation}
	case deck.DiscordAutoGain:
		change = map[string]any{"automatic_gain_control": !v.AutomaticGainControl}
	case deck.DiscordInputVolumeUp, deck.DiscordInputVolumeDown:
		if op == deck.DiscordInputVolumeDown {
			step = -step
		}
		change = map[string]any{"input": map[string]float64{"volume": min(max(v.Input.Volume+step, 0), 100)}}
	case deck.DiscordOutputVolumeUp, deck.DiscordOutputVolumeDown:
		if op == deck.DiscordOutputVolumeDown {
			step = -step
		}
		change = map[string]any{"output": map[string]float64{"volume": min(max(v.Output.Volume+step, 0), 200)}}
	case deck.DiscordJoinVoice, deck.DiscordLeaveVoice:
		return l.selectVoice(conn, op, args.ID, channel)
	}
	nv, err := conn.SetVoiceSettings(change)
	if err != nil {
		return err
	}
	l.mu.Lock()
	if l.conn == conn {
		l.voice = nv
		l.publishLocked()
	}
	l.mu.Unlock()
	return nil
}

// selectVoice joins voice channel id, or leaves the voice channel (also when
// join-voice's channel is the one it's in).
func (l *discordLink) selectVoice(conn *discord.Conn, op, id, channel string) error {
	args := map[string]any{"channel_id": nil}
	switch {
	case op == deck.DiscordLeaveVoice && channel == "":
		return nil
	case op == deck.DiscordJoinVoice && !isSnowflake(id):
		return errors.New("discord join-voice needs args id: a voice channel's ID")
	case op == deck.DiscordJoinVoice && id != channel:
		// force: the user asked, by pressing the key, to move channel
		args = map[string]any{"channel_id": id, "force": true}
	}
	// Discord's VOICE_CHANNEL_SELECT event, not the reply, sets l.channel:
	// one writer, in order
	_, err := conn.Do("SELECT_VOICE_CHANNEL", args, "")
	return err
}

// isSnowflake reports whether s looks like a Discord ID.
func isSnowflake(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
