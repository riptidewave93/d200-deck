// Package discordtest is a fake Discord app for tests: it keeps voice
// settings and the voice channel, answers commands and sends
// VOICE_SETTINGS_UPDATE and VOICE_CHANNEL_SELECT when they change.
package discordtest

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"sync"

	"github.com/riptidewave93/d200-deck/internal/discord"
)

// Discord is the fake app.
type Discord struct {
	mu      sync.Mutex
	v       discord.VoiceSettings
	channel string // voice channel ID, "" if none
	conns   []*conn
	// Refuse, if set, answers AUTHENTICATE with an error.
	Refuse bool
}

type conn struct {
	c          net.Conn
	wmu        sync.Mutex
	subscribed bool
}

// New returns a fake Discord, unmuted, in voice activity mode.
func New() *Discord {
	d := &Discord{}
	d.v.Mode.Type = "VOICE_ACTIVITY"
	d.v.Input.Volume, d.v.Output.Volume = 100, 100
	return d
}

// Dial connects to the fake as discord.Dial would.
func (d *Discord) Dial() (*discord.Conn, error) {
	a, b := net.Pipe()
	c := &conn{c: b}
	d.mu.Lock()
	d.conns = append(d.conns, c)
	d.mu.Unlock()
	go d.serve(c)
	return discord.NewConn(a, "app")
}

// Close ends every connection, as if Discord quit.
func (d *Discord) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.conns {
		c.c.Close()
	}
	d.conns = nil
}

// Voice returns whether it's muted and deafened.
func (d *Discord) Voice() (mute, deaf bool) {
	v := d.Settings()
	return v.Mute, v.Deaf
}

// Settings returns the voice settings.
func (d *Discord) Settings() discord.VoiceSettings {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.v
}

// Channel returns the voice channel's ID, "" if none.
func (d *Discord) Channel() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.channel
}

// SetVoice changes mute and deafen, as the user would in Discord.
func (d *Discord) SetVoice(mute, deaf bool) {
	d.mu.Lock()
	d.v.Mute, d.v.Deaf = mute, deaf
	d.mu.Unlock()
	d.notify("VOICE_SETTINGS_UPDATE")
}

// SetChannel joins voice channel id ("" leaves), as the user would.
func (d *Discord) SetChannel(id string) {
	d.mu.Lock()
	d.channel = id
	d.mu.Unlock()
	d.notify("VOICE_CHANNEL_SELECT")
}

// event is evt's data.
func (d *Discord) event(evt string) any {
	d.mu.Lock()
	defer d.mu.Unlock()
	if evt == "VOICE_CHANNEL_SELECT" {
		var id any
		if d.channel != "" {
			id = d.channel
		}
		return map[string]any{"channel_id": id}
	}
	return d.v
}

func (d *Discord) notify(evt string) {
	data := d.event(evt)
	d.mu.Lock()
	var conns []*conn
	for _, c := range d.conns {
		if c.subscribed {
			conns = append(conns, c)
		}
	}
	d.mu.Unlock()
	for _, c := range conns {
		c.send(discord.Message{Cmd: "DISPATCH", Evt: evt, Data: jsonOf(data)})
	}
}

func jsonOf(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func (c *conn) send(m discord.Message) {
	b, _ := json.Marshal(m)
	h := make([]byte, 8)
	binary.LittleEndian.PutUint32(h, 1)
	binary.LittleEndian.PutUint32(h[4:], uint32(len(b)))
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.c.Write(append(h, b...))
}

func read(r io.Reader) ([]byte, error) {
	var h [8]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, err
	}
	b := make([]byte, binary.LittleEndian.Uint32(h[4:]))
	_, err := io.ReadFull(r, b)
	return b, err
}

// channelObject is the channel as GET_SELECTED_VOICE_CHANNEL returns it.
func (d *Discord) channelObject() json.RawMessage {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.channel == "" {
		return json.RawMessage("null")
	}
	return jsonOf(map[string]string{"id": d.channel})
}

func (d *Discord) serve(c *conn) {
	defer c.c.Close()
	if _, err := read(c.c); err != nil { // handshake
		return
	}
	c.send(discord.Message{Cmd: "DISPATCH", Evt: "READY"})
	for {
		b, err := read(c.c)
		if err != nil {
			return
		}
		var m discord.Message
		json.Unmarshal(b, &m)
		args := jsonOf(m.Args)
		reply := discord.Message{Cmd: m.Cmd, Nonce: m.Nonce}
		changed := ""
		switch m.Cmd {
		case "AUTHENTICATE":
			if d.Refuse {
				reply.Evt, reply.Data = "ERROR", jsonOf(discord.Error{Code: 4009, Message: "Invalid OAuth2 access token"})
			}
		case "GET_VOICE_SETTINGS":
			reply.Data = jsonOf(d.Settings())
		case "SET_VOICE_SETTINGS":
			d.mu.Lock()
			json.Unmarshal(args, &d.v) // only the fields given change
			d.mu.Unlock()
			reply.Data = jsonOf(d.Settings())
			changed = "VOICE_SETTINGS_UPDATE"
		case "GET_SELECTED_VOICE_CHANNEL":
			reply.Data = d.channelObject()
		case "SELECT_VOICE_CHANNEL":
			var a struct {
				ChannelID *string `json:"channel_id"`
			}
			json.Unmarshal(args, &a)
			d.mu.Lock()
			d.channel = ""
			if a.ChannelID != nil {
				d.channel = *a.ChannelID
			}
			d.mu.Unlock()
			reply.Data = d.channelObject()
			changed = "VOICE_CHANNEL_SELECT"
		case "SUBSCRIBE":
			d.mu.Lock()
			c.subscribed = true
			d.mu.Unlock()
		}
		c.send(reply)
		if changed != "" {
			d.notify(changed)
		}
	}
}
