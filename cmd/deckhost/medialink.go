package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
)

// mediaPlayer is one media player as a backend sees it.
type mediaPlayer struct {
	ID      string // the backend's name for it, e.g. org.mpris.MediaPlayer2.spotify
	Name    string // its short name, e.g. spotify (see shortName)
	Playing bool
	Shuffle bool
	Repeat  bool
}

// mediaBackend lists the PC's media players and controls them
// (media_linux.go, media_windows.go).
type mediaBackend interface {
	players() ([]mediaPlayer, error)
	// control runs media op on p; shuffle and repeat toggle from p's state.
	control(p mediaPlayer, op string) error
	close()
}

// shortName is a player's name for keys' args.id: lowercase, up to the first
// character other than a letter, digit or -.
func shortName(s string) string {
	s = strings.ToLower(s)
	if i := strings.IndexFunc(s, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-'
	}); i >= 0 {
		s = s[:i]
	}
	return s
}

// mediaLink follows the PC's media players and runs media actions. Players
// come and go without telling, so it asks the backend every second.
type mediaLink struct {
	open    func() (mediaBackend, error)
	publish func(map[string]bool)
	poll    time.Duration

	mu      sync.Mutex
	backend mediaBackend
	players []mediaPlayer
	active  string          // the ID of the player that ops without an id are for
	shown   map[string]bool // the per-player state last published
	status  string
	kick    chan struct{}
}

func newMediaLink(open func() (mediaBackend, error), publish func(map[string]bool)) *mediaLink {
	return &mediaLink{open: open, publish: publish, poll: time.Second, kick: make(chan struct{}, 1)}
}

func (l *mediaLink) title() string { return "Media" }

func (l *mediaLink) ops() []string { return deck.MediaOps }

func (l *mediaLink) statusText() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.status
}

// run follows the players until stop is closed.
func (l *mediaLink) run(stop <-chan struct{}) {
	for {
		b, err := l.open()
		if errors.Is(err, errors.ErrUnsupported) {
			return // keys use the media keys instead
		}
		if err == nil {
			err = l.follow(b, stop)
			b.close()
		}
		if err == nil {
			return
		}
		log.Printf("media: %v", err)
		l.update(nil, nil)
		select {
		case <-stop:
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// follow polls b until stop is closed (nil) or b fails.
func (l *mediaLink) follow(b mediaBackend, stop <-chan struct{}) error {
	t := time.NewTicker(l.poll)
	defer t.Stop()
	for {
		ps, err := b.players()
		if err != nil {
			return err
		}
		l.update(b, ps)
		select {
		case <-stop:
			return nil
		case <-t.C:
		case <-l.kick:
			time.Sleep(150 * time.Millisecond) // let the player change
		}
	}
}

// update takes the players' new state and publishes it.
func (l *mediaLink) update(b mediaBackend, ps []mediaPlayer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.backend, l.players = b, ps
	l.active = pickActive(ps, l.active)
	var names []string
	for _, p := range ps {
		n := p.Name
		if p.Playing {
			n += " (playing)"
		}
		names = append(names, n)
	}
	switch {
	case b == nil:
		l.status = "not available"
	case len(ps) == 0:
		l.status = "no players"
	default:
		l.status = strings.Join(names, ", ")
	}

	s := map[string]bool{"media.ready": len(ps) > 0}
	set := func(suffix string, p mediaPlayer) {
		s[deck.AppMedia+"."+deck.MediaPlayPause+suffix] = p.Playing
		s[deck.AppMedia+"."+deck.MediaShuffle+suffix] = p.Shuffle
		s[deck.AppMedia+"."+deck.MediaRepeat+suffix] = p.Repeat
	}
	a, _ := l.find("")
	set("", a)
	shown := map[string]bool{}
	for _, p := range ps {
		if q, _ := l.find(p.Name); q.ID == p.ID {
			set(":"+p.Name, p)
			shown[p.Name] = true
		}
	}
	for n := range l.shown {
		if !shown[n] {
			set(":"+n, mediaPlayer{})
		}
	}
	l.shown = shown
	l.publish(s)
}

// pickActive is the player that ops without an id are for: the one
// playing, or else the last one that was.
func pickActive(ps []mediaPlayer, last string) string {
	i := slices.IndexFunc(ps, func(p mediaPlayer) bool { return p.ID == last })
	if i >= 0 && ps[i].Playing {
		return last
	}
	if j := slices.IndexFunc(ps, func(p mediaPlayer) bool { return p.Playing }); j >= 0 {
		return ps[j].ID
	}
	if i >= 0 {
		return last
	}
	if len(ps) > 0 {
		return ps[0].ID
	}
	return ""
}

// find is the player for id: the active one for "", else the one playing
// whose name starts with id, or the first such.
func (l *mediaLink) find(id string) (mediaPlayer, bool) {
	if id == "" {
		i := slices.IndexFunc(l.players, func(p mediaPlayer) bool { return p.ID == l.active })
		if i < 0 {
			return mediaPlayer{}, false
		}
		return l.players[i], true
	}
	id = strings.ToLower(id)
	var found []mediaPlayer
	for _, p := range l.players {
		if strings.HasPrefix(p.Name, id) {
			found = append(found, p)
		}
	}
	if i := slices.IndexFunc(found, func(p mediaPlayer) bool { return p.Playing }); i >= 0 {
		return found[i], true
	}
	if len(found) > 0 {
		return found[0], true
	}
	return mediaPlayer{}, false
}

// do runs media operation op for a key's press.
func (l *mediaLink) do(op string, rawArgs json.RawMessage, release bool) error {
	if !slices.Contains(deck.MediaOps, op) {
		return fmt.Errorf("media has no %q op", op)
	}
	if release {
		return nil
	}
	var args struct {
		ID string `json:"id"`
	}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return fmt.Errorf("media %s: bad args: %w", op, err)
		}
	}
	l.mu.Lock()
	b := l.backend
	p, ok := l.find(args.ID)
	l.mu.Unlock()
	switch {
	case b == nil:
		return errors.New("media: not available")
	case !ok && args.ID != "":
		return fmt.Errorf("media: no player %q", args.ID)
	case !ok:
		return errors.New("media: no players")
	}
	if err := b.control(p, op); err != nil {
		return fmt.Errorf("media %s: %w", op, err)
	}
	select {
	case l.kick <- struct{}{}:
	default:
	}
	return nil
}
