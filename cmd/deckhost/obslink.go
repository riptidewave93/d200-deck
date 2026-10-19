package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/internal/obs"
)

// obsSettings are obs.json: where OBS's WebSocket server is and its
// password. Without the file, deckhost tries the default port with no
// password.
type obsSettings struct {
	URL      string `json:"url"`
	Password string `json:"password"`
}

// obsDial connects to OBS as obs.json says. setUp is whether obs.json
// exists.
func obsDial() (conn *obs.Conn, setUp bool, err error) {
	var s obsSettings
	if dir, err := configDir(); err == nil {
		err = readJSON(filepath.Join(dir, "obs.json"), &s)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, true, fmt.Errorf("obs.json: %w", err)
		}
		setUp = err == nil
	}
	if s.URL == "" {
		s.URL = obs.DefaultURL
	}
	conn, err = obs.Dial(s.URL, s.Password)
	return conn, setUp, err
}

// obsLink keeps deckhost connected to OBS, follows its state and runs obs
// actions.
type obsLink struct {
	dial    func() (*obs.Conn, bool, error)
	publish func(map[string]bool)
	retry   time.Duration

	mu     sync.Mutex
	conn   *obs.Conn
	st     obsState
	shown  obsState // as last published
	status string   // for the tray; "" while OBS isn't set up or found
}

// obsState is what OBS keys show.
type obsState struct {
	scene                                      string
	stream, record, paused, replay, virtualCam bool
	muted                                      map[string]bool // audio sources by name
}

func newOBSLink(dial func() (*obs.Conn, bool, error), publish func(map[string]bool)) *obsLink {
	return &obsLink{dial: dial, publish: publish, retry: 5 * time.Second}
}

func (l *obsLink) title() string { return "OBS" }

func (l *obsLink) ops() []string { return deck.OBSOps }

func (l *obsLink) statusText() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.status
}

func (l *obsLink) setStatus(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.status = s
}

// run connects to OBS, again whenever the connection ends, until stop is
// closed.
func (l *obsLink) run(stop <-chan struct{}) {
	for {
		conn, setUp, err := l.dial()
		switch {
		case err == nil:
			l.follow(conn)
		case errors.Is(err, obs.ErrAuth):
			l.setStatus("needs its WebSocket password (see docs/obs.md)")
		case errors.Is(err, obs.ErrNotRunning) && !setUp:
			l.setStatus("") // OBS may not be installed: say nothing
		case errors.Is(err, obs.ErrNotRunning):
			l.setStatus("not running")
		default:
			log.Printf("obs: %v", err)
			l.setStatus(err.Error())
		}
		select {
		case <-stop:
			return
		case <-time.After(l.retry):
		}
	}
}

// follow tracks OBS's state on conn until the connection ends.
func (l *obsLink) follow(conn *obs.Conn) {
	st, err := obsFetch(conn)
	if err != nil {
		conn.Close()
		l.setStatus(err.Error())
		return
	}
	l.mu.Lock()
	l.conn, l.st, l.status = conn, st, "connected"
	l.publishLocked()
	l.mu.Unlock()
	log.Print("obs: connected")
	for e := range conn.Events() {
		var d struct {
			SceneName    string `json:"sceneName"`
			OldSceneName string `json:"oldSceneName"`
			InputName    string `json:"inputName"`
			OldInputName string `json:"oldInputName"`
			InputMuted   bool   `json:"inputMuted"`
			OutputActive bool   `json:"outputActive"`
			OutputState  string `json:"outputState"`
		}
		json.Unmarshal(e.Data, &d)
		l.mu.Lock()
		s := &l.st
		switch e.Type {
		case "CurrentProgramSceneChanged":
			s.scene = d.SceneName
		case "SceneNameChanged":
			if s.scene == d.OldSceneName {
				s.scene = d.SceneName
			}
		case "InputMuteStateChanged":
			s.muted[d.InputName] = d.InputMuted
		case "InputNameChanged":
			if m, ok := s.muted[d.OldInputName]; ok {
				delete(s.muted, d.OldInputName)
				s.muted[d.InputName] = m
			}
		case "InputRemoved":
			delete(s.muted, d.InputName)
		case "StreamStateChanged":
			s.stream = d.OutputActive
		case "RecordStateChanged":
			s.record = d.OutputActive
			switch d.OutputState {
			case "OBS_WEBSOCKET_OUTPUT_PAUSED":
				s.paused = true
			case "OBS_WEBSOCKET_OUTPUT_RESUMED", "OBS_WEBSOCKET_OUTPUT_STOPPED":
				s.paused = false
			}
		case "ReplayBufferStateChanged":
			s.replay = d.OutputActive
		case "VirtualcamStateChanged":
			s.virtualCam = d.OutputActive
		}
		l.publishLocked()
		l.mu.Unlock()
	}
	log.Printf("obs: disconnected: %v", conn.Err())
	l.mu.Lock()
	l.conn, l.st, l.status = nil, obsState{}, "not running"
	l.publishLocked()
	l.mu.Unlock()
}

// obsFetch reads OBS's state.
func obsFetch(conn *obs.Conn) (obsState, error) {
	st := obsState{muted: map[string]bool{}}
	get := func(req string, data any, v any) error {
		b, err := conn.Request(req, data)
		if err == nil {
			err = json.Unmarshal(b, v)
		}
		return err
	}
	var scene struct {
		Name string `json:"currentProgramSceneName"`
	}
	if err := get("GetCurrentProgramScene", nil, &scene); err != nil {
		return st, err
	}
	st.scene = scene.Name
	var out struct {
		Active bool `json:"outputActive"`
		Paused bool `json:"outputPaused"`
	}
	if get("GetStreamStatus", nil, &out) == nil {
		st.stream = out.Active
	}
	if get("GetRecordStatus", nil, &out) == nil {
		st.record, st.paused = out.Active, out.Paused
	}
	out.Active = false
	if get("GetReplayBufferStatus", nil, &out) == nil { // fails while it's off in the settings
		st.replay = out.Active
	}
	out.Active = false
	if get("GetVirtualCamStatus", nil, &out) == nil {
		st.virtualCam = out.Active
	}
	var inputs struct {
		Inputs []struct {
			Name string `json:"inputName"`
		} `json:"inputs"`
	}
	if err := get("GetInputList", nil, &inputs); err != nil {
		return st, err
	}
	for _, in := range inputs.Inputs {
		var m struct {
			Muted bool `json:"inputMuted"`
		}
		if get("GetInputMute", map[string]string{"inputName": in.Name}, &m) == nil { // fails for video sources
			st.muted[in.Name] = m.Muted
		}
	}
	return st, nil
}

// publishLocked tells the decks OBS's state: what changed since last time.
func (l *obsLink) publishLocked() {
	s, old := l.st, l.shown
	p := map[string]bool{
		"obs.ready":         l.conn != nil,
		"obs.stream":        s.stream,
		"obs.record":        s.record,
		"obs.pause-record":  s.paused,
		"obs.replay-buffer": s.replay,
		"obs.virtual-cam":   s.virtualCam,
	}
	if old.scene != s.scene && old.scene != "" {
		p["obs.scene:"+old.scene] = false
	}
	if s.scene != "" {
		p["obs.scene:"+s.scene] = true
	}
	for name := range old.muted {
		if _, ok := s.muted[name]; !ok {
			p["obs.mute:"+name] = false
		}
	}
	for name, m := range s.muted {
		p["obs.mute:"+name] = m
	}
	l.shown = obsState{scene: s.scene, muted: map[string]bool{}}
	for name, m := range s.muted {
		l.shown.muted[name] = m
	}
	l.publish(p)
}

// do runs OBS operation op for a key's press.
func (l *obsLink) do(op string, rawArgs json.RawMessage, release bool) error {
	if !slices.Contains(deck.OBSOps, op) {
		return fmt.Errorf("obs has no %q op", op)
	}
	if release {
		return nil
	}
	var args struct {
		ID string `json:"id"`
	}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return fmt.Errorf("obs %s: bad args: %w", op, err)
		}
	}
	l.mu.Lock()
	conn, status := l.conn, l.status
	l.mu.Unlock()
	if conn == nil {
		if status == "" {
			status = "not running"
		}
		return fmt.Errorf("obs: %s", status)
	}
	req, data := "", map[string]string(nil)
	switch op {
	case deck.OBSScene, deck.OBSMute:
		if args.ID == "" {
			return fmt.Errorf("obs %s needs args id: the name in OBS", op)
		}
		req, data = "SetCurrentProgramScene", map[string]string{"sceneName": args.ID}
		if op == deck.OBSMute {
			req, data = "ToggleInputMute", map[string]string{"inputName": args.ID}
		}
	case deck.OBSStream:
		req = "ToggleStream"
	case deck.OBSRecord:
		req = "ToggleRecord"
	case deck.OBSPauseRecord:
		req = "ToggleRecordPause"
	case deck.OBSReplayBuffer:
		req = "ToggleReplayBuffer"
	case deck.OBSSaveReplay:
		req = "SaveReplayBuffer"
	case deck.OBSVirtualCam:
		req = "ToggleVirtualCam"
	}
	var err error
	if data != nil {
		_, err = conn.Request(req, data)
	} else {
		_, err = conn.Request(req, nil)
	}
	return err // the state follows from OBS's events
}
