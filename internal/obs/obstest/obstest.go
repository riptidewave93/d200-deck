// Package obstest is a fake OBS WebSocket server for tests.
package obstest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/coder/websocket"

	"github.com/riptidewave93/d200-deck/internal/obs"
)

// Salt and Challenge are what the fake asks clients to authenticate with.
const (
	Salt      = "lM1GncleQOaCu9lT1yeUZhFYnqhsLLP1G5lAGo3ixaI="
	Challenge = "+IxH4CnCiqpX1rM9scsNynZzbOe4KhDeYcTNS3PDaeY="
)

// Server is a fake OBS with scenes, audio sources and recording.
type Server struct {
	URL      string
	srv      *httptest.Server
	password string

	mu        sync.Mutex
	scene     string
	muted     map[string]bool
	recording bool
	requests  []string
	conns     map[*websocket.Conn]bool
}

// New starts a fake OBS on scene "Main" with a "Mic" source, wanting
// password ("" for none).
func New(password string) *Server {
	s := &Server{password: password, scene: "Main", muted: map[string]bool{"Mic": false}, conns: map[*websocket.Conn]bool{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	s.URL = "ws" + strings.TrimPrefix(s.srv.URL, "http")
	return s
}

// Close stops the server, ending its connections.
func (s *Server) Close() {
	s.mu.Lock()
	for c := range s.conns {
		c.CloseNow()
	}
	s.mu.Unlock()
	s.srv.Close()
}

// Requests are the request types received, in order.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// Event sends an event to every client.
func (s *Server) Event(typ string, data any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventLocked(typ, data)
}

func (s *Server) eventLocked(typ string, data any) {
	for c := range s.conns {
		send(c, 5, map[string]any{"eventType": typ, "eventIntent": 0, "eventData": data})
	}
}

func send(c *websocket.Conn, op int, d any) error {
	b, _ := json.Marshal(map[string]any{"op": op, "d": d})
	return c.Write(context.Background(), websocket.MessageText, b)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"obswebsocket.json"}})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := context.Background()
	hello := map[string]any{"obsWebSocketVersion": "5.5.0", "rpcVersion": 1}
	if s.password != "" {
		hello["authentication"] = map[string]string{"challenge": Challenge, "salt": Salt}
	}
	send(c, 0, hello)
	var id struct {
		D struct {
			Auth string `json:"authentication"`
		} `json:"d"`
	}
	if _, b, err := c.Read(ctx); err != nil || json.Unmarshal(b, &id) != nil {
		return
	}
	if s.password != "" && id.D.Auth != obs.Auth(s.password, Salt, Challenge) {
		c.Close(4009, "Authentication failed.")
		return
	}
	s.mu.Lock()
	send(c, 2, map[string]any{"negotiatedRpcVersion": 1})
	s.conns[c] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
	}()
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		var m struct {
			Op int `json:"op"`
			D  struct {
				Type string            `json:"requestType"`
				ID   string            `json:"requestId"`
				Data map[string]string `json:"requestData"`
			} `json:"d"`
		}
		if json.Unmarshal(b, &m) != nil || m.Op != 6 {
			continue
		}
		s.mu.Lock()
		s.requests = append(s.requests, m.D.Type)
		data, code := s.request(m.D.Type, m.D.Data)
		status := map[string]any{"result": code == 100, "code": code}
		if code != 100 {
			status["comment"] = "fake OBS can't"
		}
		send(c, 7, map[string]any{"requestType": m.D.Type, "requestId": m.D.ID, "requestStatus": status, "responseData": data})
		s.mu.Unlock()
	}
}

// request runs a request: its response data and status code (100 is OK).
func (s *Server) request(typ string, args map[string]string) (any, int) {
	switch typ {
	case "GetCurrentProgramScene":
		return map[string]any{"currentProgramSceneName": s.scene, "sceneName": s.scene}, 100
	case "SetCurrentProgramScene":
		s.scene = args["sceneName"]
		s.eventLocked("CurrentProgramSceneChanged", map[string]string{"sceneName": s.scene})
		return nil, 100
	case "GetRecordStatus":
		return map[string]bool{"outputActive": s.recording, "outputPaused": false}, 100
	case "ToggleRecord":
		s.recording = !s.recording
		state := "OBS_WEBSOCKET_OUTPUT_STOPPED"
		if s.recording {
			state = "OBS_WEBSOCKET_OUTPUT_STARTED"
		}
		s.eventLocked("RecordStateChanged", map[string]any{"outputActive": s.recording, "outputState": state})
		return map[string]bool{"outputActive": s.recording}, 100
	case "GetStreamStatus", "GetVirtualCamStatus":
		return map[string]bool{"outputActive": false}, 100
	case "GetReplayBufferStatus":
		return nil, 604 // the replay buffer is off in the settings
	case "GetInputList":
		var inputs []map[string]string
		for name := range s.muted {
			inputs = append(inputs, map[string]string{"inputName": name})
		}
		inputs = append(inputs, map[string]string{"inputName": "Camera"})
		return map[string]any{"inputs": inputs}, 100
	case "GetInputMute", "ToggleInputMute":
		m, ok := s.muted[args["inputName"]]
		if !ok {
			return nil, 600 // no such source, or no audio
		}
		if typ == "ToggleInputMute" {
			m = !m
			s.muted[args["inputName"]] = m
			s.eventLocked("InputMuteStateChanged", map[string]any{"inputName": args["inputName"], "inputMuted": m})
		}
		return map[string]bool{"inputMuted": m}, 100
	}
	return nil, 204 // unknown request type
}
