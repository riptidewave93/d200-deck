package discord

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fake is a Discord app: it answers the handshake, then each command with
// reply(m), and sends what's put on send.
type fake struct {
	t     *testing.T
	c     net.Conn
	reply func(m Message) (op uint32, v any)
}

func writeFrame(c net.Conn, op uint32, v any) {
	b, _ := json.Marshal(v)
	h := make([]byte, 8)
	binary.LittleEndian.PutUint32(h, op)
	binary.LittleEndian.PutUint32(h[4:], uint32(len(b)))
	c.Write(append(h, b...))
}

func (f *fake) serve() {
	op, b, err := readFrame(f.c)
	if err != nil || op != opHandshake || !strings.Contains(string(b), `"client_id":"app"`) {
		f.t.Errorf("handshake %d %s %v", op, b, err)
		return
	}
	writeFrame(f.c, opFrame, Message{Cmd: "DISPATCH", Evt: "READY"})
	for {
		op, b, err := readFrame(f.c)
		if err != nil {
			return
		}
		if op == opPong {
			writeFrame(f.c, opFrame, Message{Cmd: "DISPATCH", Evt: "PONGED", Data: b})
			continue
		}
		var m Message
		json.Unmarshal(b, &m)
		if op, v := f.reply(m); v != nil {
			writeFrame(f.c, op, v)
		}
	}
}

// connect returns a Conn to a fake answering with reply.
func connect(t *testing.T, reply func(m Message) (uint32, any)) (*Conn, net.Conn) {
	a, b := net.Pipe()
	f := &fake{t: t, c: b, reply: reply}
	go f.serve()
	d, err := NewConn(a, "app")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close(); b.Close() })
	return d, b
}

func data(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestDo(t *testing.T) {
	d, _ := connect(t, func(m Message) (uint32, any) {
		switch m.Cmd {
		case "GET_VOICE_SETTINGS":
			return opFrame, Message{Cmd: m.Cmd, Nonce: m.Nonce, Data: data(map[string]any{"mute": true, "mode": map[string]string{"type": "PUSH_TO_TALK"}})}
		case "SET_VOICE_SETTINGS":
			return opFrame, Message{Cmd: m.Cmd, Nonce: m.Nonce, Data: data(m.Args)}
		case "AUTHENTICATE":
			return opFrame, Message{Cmd: m.Cmd, Nonce: m.Nonce, Evt: "ERROR", Data: data(Error{4009, "Invalid OAuth2 access token"})}
		case "AUTHORIZE":
			return opFrame, Message{Cmd: m.Cmd, Nonce: m.Nonce, Data: data(map[string]string{"code": "abc"})}
		}
		return 0, nil // no reply
	})
	v, err := d.VoiceSettings()
	if err != nil || !v.Mute || v.Deaf || v.Mode.Type != "PUSH_TO_TALK" {
		t.Errorf("VoiceSettings = %+v, %v", v, err)
	}
	if v, err = d.SetVoiceSettings(map[string]any{"deaf": true}); err != nil || !v.Deaf {
		t.Errorf("SetVoiceSettings = %+v, %v", v, err)
	}
	var e *Error
	if err := d.Authenticate(Token{AccessToken: "x"}); !errors.As(err, &e) || e.Code != 4009 || !strings.Contains(err.Error(), "Invalid") {
		t.Errorf("Authenticate: %v", err)
	}
	if code, err := d.Authorize(App{ClientID: "app"}); code != "abc" || err != nil {
		t.Errorf("Authorize = %q, %v", code, err)
	}

	old := Timeout
	Timeout = 50 * time.Millisecond
	t.Cleanup(func() { Timeout = old })
	if _, err := d.Do("NOTHING", nil, ""); err == nil {
		t.Error("no error without a reply")
	}
}

func TestEvents(t *testing.T) {
	d, c := connect(t, func(m Message) (uint32, any) {
		return opFrame, Message{Cmd: m.Cmd, Nonce: m.Nonce}
	})
	if _, err := d.Do("SUBSCRIBE", nil, "VOICE_SETTINGS_UPDATE"); err != nil {
		t.Fatal(err)
	}
	writeFrame(c, opFrame, Message{Cmd: "DISPATCH", Evt: "VOICE_SETTINGS_UPDATE", Data: data(map[string]bool{"mute": true})})
	writeFrame(c, opPing, map[string]int{"n": 7}) // answered with a pong carrying the same data
	if e := <-d.Events(); e.Evt != "VOICE_SETTINGS_UPDATE" || string(e.Data) != `{"mute":true}` {
		t.Errorf("event %+v", e)
	}
	if e := <-d.Events(); e.Evt != "PONGED" || string(e.Data) != `{"n":7}` {
		t.Errorf("pong %+v", e)
	}

	writeFrame(c, opClose, Error{1000, "bye"})
	<-d.Done()
	if _, ok := <-d.Events(); ok {
		t.Error("events not closed")
	}
	if err := d.Err(); err == nil || !strings.Contains(err.Error(), "bye") {
		t.Errorf("Err = %v", err)
	}
	if _, err := d.Do("X", nil, ""); err == nil {
		t.Error("Do after close")
	}
}

func TestHandshakeRefused(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	go func() {
		readFrame(b)
		writeFrame(b, opClose, Error{4000, "Invalid Client ID"})
	}()
	if _, err := NewConn(a, "app"); err == nil || !strings.Contains(err.Error(), "Invalid Client ID") {
		t.Errorf("NewConn: %v", err)
	}
}

func TestBigFrame(t *testing.T) {
	h := make([]byte, 8)
	binary.LittleEndian.PutUint32(h[4:], maxFrame+1)
	if _, _, err := readFrame(strings.NewReader(string(h))); err == nil {
		t.Error("read an oversized frame")
	}
}

func TestDial(t *testing.T) {
	dir := t.TempDir()
	for _, env := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(env, "")
	}
	t.Setenv("XDG_RUNTIME_DIR", dir)
	if _, err := Dial("app"); err == nil {
		t.Fatal("dialled with no Discord running")
	}
	// a Flatpak Discord
	p := filepath.Join(dir, "app/com.discordapp.Discord/discord-ipc-0")
	if !slices.Contains(Sockets(), p) {
		t.Fatalf("%s not in Sockets", p)
	}
	os.MkdirAll(filepath.Dir(p), 0o700)
	l, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := l.Accept()
		if err == nil {
			f := &fake{t: t, c: c, reply: func(Message) (uint32, any) { return 0, nil }}
			f.serve()
		}
	}()
	d, err := Dial("app")
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
}

func TestToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("client_secret") != "s" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "abc" || r.Form.Get("redirect_uri") != RedirectURI {
				http.Error(w, "bad code", http.StatusBadRequest)
				return
			}
			w.Write([]byte(`{"access_token":"a1","refresh_token":"r1","expires_in":3600}`))
		case "refresh_token":
			w.Write([]byte(`{"access_token":"a2","refresh_token":"r2","expires_in":3600}`))
		}
	}))
	defer srv.Close()
	old := TokenURL
	TokenURL = srv.URL
	t.Cleanup(func() { TokenURL = old })

	app := App{ClientID: "app", ClientSecret: "s"}
	tok, err := app.Exchange("abc")
	if err != nil || tok.AccessToken != "a1" || tok.RefreshToken != "r1" || time.Until(tok.Expiry) < 59*time.Minute {
		t.Errorf("Exchange = %+v, %v", tok, err)
	}
	if tok, err = app.Refresh(tok); err != nil || tok.AccessToken != "a2" {
		t.Errorf("Refresh = %+v, %v", tok, err)
	}
	if _, err := (App{ClientID: "app", ClientSecret: "wrong"}).Exchange("abc"); err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("wrong secret: %v", err)
	}
}
