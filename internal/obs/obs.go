// Package obs talks to OBS Studio's WebSocket server (obs-websocket 5,
// built into OBS 28 and later).
package obs

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// DefaultURL is OBS's WebSocket server with its default port.
const DefaultURL = "ws://127.0.0.1:4455"

// Event subscriptions: general, scenes, inputs and outputs.
const subscriptions = 1<<0 | 1<<2 | 1<<3 | 1<<6

// ErrAuth means OBS wants a password and the one given is wrong or missing.
var ErrAuth = errors.New("wrong or missing WebSocket password")

// ErrNotRunning means nothing answered: OBS isn't running, or its WebSocket
// server is off.
var ErrNotRunning = errors.New("not running")

var requestTimeout = 5 * time.Second

// Event is an OBS event, such as CurrentProgramSceneChanged.
type Event struct {
	Type string          `json:"eventType"`
	Data json.RawMessage `json:"eventData"`
}

// Conn is a connection to OBS.
type Conn struct {
	ws     *websocket.Conn
	events chan Event

	mu      sync.Mutex
	nextID  int
	pending map[string]chan response
	err     error
}

type message struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
}

type response struct {
	ID     string `json:"requestId"`
	Status struct {
		Result  bool   `json:"result"`
		Code    int    `json:"code"`
		Comment string `json:"comment"`
	} `json:"requestStatus"`
	Data json.RawMessage `json:"responseData"`
}

// Dial connects to OBS at url and identifies with password ("" for none).
func Dial(url, password string) (*Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"obswebsocket.json"}})
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNotRunning, err)
	}
	ws.SetReadLimit(4 << 20)
	c := &Conn{ws: ws, events: make(chan Event, 256), pending: map[string]chan response{}}
	if err := c.identify(ctx, password); err != nil {
		ws.CloseNow()
		return nil, err
	}
	go c.read()
	return c, nil
}

func (c *Conn) identify(ctx context.Context, password string) error {
	var hello struct {
		Auth *struct {
			Challenge string `json:"challenge"`
			Salt      string `json:"salt"`
		} `json:"authentication"`
	}
	if err := c.expect(ctx, 0, &hello); err != nil {
		return err
	}
	id := map[string]any{"rpcVersion": 1, "eventSubscriptions": subscriptions}
	if hello.Auth != nil {
		if password == "" {
			return ErrAuth
		}
		id["authentication"] = Auth(password, hello.Auth.Salt, hello.Auth.Challenge)
	}
	if err := c.send(ctx, 1, id); err != nil {
		return err
	}
	err := c.expect(ctx, 2, nil)
	if websocket.CloseStatus(err) == 4009 { // AuthenticationFailed
		return ErrAuth
	}
	return err
}

// Auth is the obs-websocket authentication string.
func Auth(password, salt, challenge string) string {
	h := sha256.Sum256([]byte(password + salt))
	secret := base64.StdEncoding.EncodeToString(h[:])
	h = sha256.Sum256([]byte(secret + challenge))
	return base64.StdEncoding.EncodeToString(h[:])
}

func (c *Conn) send(ctx context.Context, op int, d any) error {
	b, err := json.Marshal(map[string]any{"op": op, "d": d})
	if err != nil {
		return err
	}
	return c.ws.Write(ctx, websocket.MessageText, b)
}

// expect reads one message, which must have op, into v.
func (c *Conn) expect(ctx context.Context, op int, v any) error {
	_, b, err := c.ws.Read(ctx)
	if err != nil {
		return err
	}
	var m message
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if m.Op != op {
		return fmt.Errorf("obs sent op %d, not %d", m.Op, op)
	}
	if v != nil {
		return json.Unmarshal(m.D, v)
	}
	return nil
}

// read takes events and responses until the connection ends.
func (c *Conn) read() {
	var err error
	for {
		var b []byte
		if _, b, err = c.ws.Read(context.Background()); err != nil {
			break
		}
		var m message
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		switch m.Op {
		case 5:
			var e Event
			if json.Unmarshal(m.D, &e) == nil {
				select {
				case c.events <- e:
				default: // the reader mustn't stall the responses
				}
			}
		case 7:
			var r response
			if json.Unmarshal(m.D, &r) == nil {
				c.mu.Lock()
				ch := c.pending[r.ID]
				delete(c.pending, r.ID)
				c.mu.Unlock()
				if ch != nil {
					ch <- r
				}
			}
		}
	}
	c.mu.Lock()
	c.err = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.events)
}

// Request sends request typ with data (nil for none) and returns the
// response data.
func (c *Conn) Request(typ string, data any) (json.RawMessage, error) {
	ch := make(chan response, 1)
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return nil, c.err
	}
	c.nextID++
	id := strconv.Itoa(c.nextID)
	c.pending[id] = ch
	c.mu.Unlock()
	req := map[string]any{"requestType": typ, "requestId": id}
	if data != nil {
		req["requestData"] = data
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if err := c.send(ctx, 6, req); err != nil {
		return nil, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return nil, errors.New("obs: disconnected")
		}
		if !r.Status.Result {
			return nil, fmt.Errorf("obs %s: %s (%d)", typ, r.Status.Comment, r.Status.Code)
		}
		return r.Data, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("obs %s: no answer", typ)
	}
}

// Events are OBS's events; closed when the connection ends.
func (c *Conn) Events() <-chan Event { return c.events }

// Err is why the connection ended.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close ends the connection.
func (c *Conn) Close() { c.ws.Close(websocket.StatusNormalClosure, "") }
