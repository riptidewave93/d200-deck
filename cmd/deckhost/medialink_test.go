package main

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
)

// fakeMedia is a media backend with players to set.
type fakeMedia struct {
	mu   sync.Mutex
	ps   []mediaPlayer
	done []string // "<player ID> <op>"
	fail error
}

func (f *fakeMedia) players() ([]mediaPlayer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ps), f.fail
}

func (f *fakeMedia) control(p mediaPlayer, op string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = append(f.done, p.ID+" "+op)
	for i := range f.ps {
		if f.ps[i].ID == p.ID && op == deck.MediaPlayPause {
			f.ps[i].Playing = !f.ps[i].Playing
		}
	}
	return nil
}

func (f *fakeMedia) close() {}

// art is a 300x200 PNG for every song.
func (f *fakeMedia) art(p mediaPlayer) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes(), nil
}

func (f *fakeMedia) set(ps ...mediaPlayer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ps = ps
}

func TestMedia(t *testing.T) {
	fake := &fakeMedia{}
	var mu sync.Mutex
	state := map[string]bool{}
	type look struct {
		label string
		image []byte
	}
	looks := map[string]look{}
	l := newMediaLink(func() (mediaBackend, error) { return fake, nil }, func(s map[string]bool) {
		mu.Lock()
		defer mu.Unlock()
		for k, v := range s {
			state[k] = v
		}
	}, func(name, label string, image []byte) {
		mu.Lock()
		defer mu.Unlock()
		if label == "" {
			delete(looks, name)
		} else {
			looks[name] = look{label, image}
		}
	})
	l.poll = 5 * time.Millisecond
	is := func(name string, on bool) func() bool {
		return func() bool {
			mu.Lock()
			defer mu.Unlock()
			return state[name] == on
		}
	}
	stop := make(chan struct{})
	defer close(stop)
	go l.run(stop)
	waitFor(t, "no players", func() bool { return l.statusText() == "no players" })
	if err := l.do(deck.MediaPlayPause, nil, false); err == nil {
		t.Error("play-pause worked without players")
	}

	spotify := mediaPlayer{ID: "org.mpris.MediaPlayer2.spotify", Name: "spotify", Shuffle: true}
	firefox := mediaPlayer{ID: "org.mpris.MediaPlayer2.firefox.instance_1_2", Name: "firefox", Playing: true,
		Title: "A Song Whose Title Goes On And On", Artist: "Band"}
	fake.set(spotify, firefox)
	waitFor(t, "firefox playing", is("media.play-pause", true))
	waitFor(t, "spotify's shuffle", is("media.shuffle:spotify", true))
	if !is("media.ready", true)() || is("media.play-pause:firefox", false)() || l.statusText() != "spotify, firefox (playing)" {
		t.Errorf("state %v, status %q", state, l.statusText())
	}
	// the song playing on now-playing keys: shortened, with its art scaled
	waitFor(t, "song", func() bool {
		mu.Lock()
		defer mu.Unlock()
		_, ok := looks["media.now-playing"]
		return ok
	})
	mu.Lock()
	song := looks["media.now-playing"]
	_, spotifySong := looks["media.now-playing:spotify"]
	mu.Unlock()
	art, err := jpeg.DecodeConfig(bytes.NewReader(song.image))
	if song.label != "A Song Whose Title…\nBand" || err != nil || art.Width != artSize || art.Height != artSize || spotifySong {
		t.Errorf("song %q, art %+v %v, spotify's %v", song.label, art, err, spotifySong)
	}

	// without an id: the one playing; with one: the player it names
	if err := l.do(deck.MediaPlayPause, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := l.do(deck.MediaNowPlaying, nil, false); err != nil { // plays it again
		t.Fatal(err)
	}
	if err := l.do(deck.MediaNext, []byte(`{"id":"Spot"}`), false); err != nil {
		t.Fatal(err)
	}
	if err := l.do(deck.MediaNext, []byte(`{"id":"vlc"}`), false); err == nil {
		t.Error("a missing player worked")
	}
	fake.mu.Lock()
	done := slices.Clone(fake.done)
	fake.mu.Unlock()
	if !slices.Equal(done, []string{firefox.ID + " play-pause", firefox.ID + " play-pause", spotify.ID + " next"}) {
		t.Errorf("done %q", done)
	}
	// firefox played again: still the one ops are for
	waitFor(t, "playing", is("media.play-pause", true))
	if p, _ := l.find(""); p.ID != firefox.ID {
		t.Errorf("active %q", p.ID)
	}

	fake.set(spotify)
	waitFor(t, "firefox gone", func() bool { return l.statusText() == "spotify" })
	mu.Lock()
	if len(looks) != 0 {
		t.Errorf("songs still shown: %v", looks)
	}
	mu.Unlock()
	if is("media.play-pause:firefox", true)() {
		t.Error("firefox's state stayed")
	}
	fake.mu.Lock()
	fake.fail = errors.New("bus gone")
	fake.mu.Unlock()
	waitFor(t, "not available", is("media.ready", false))
}

func TestPickActive(t *testing.T) {
	a := mediaPlayer{ID: "a"}
	b := mediaPlayer{ID: "b", Playing: true}
	c := mediaPlayer{ID: "c", Playing: true}
	for _, tc := range []struct {
		ps   []mediaPlayer
		last string
		want string
	}{
		{nil, "a", ""},
		{[]mediaPlayer{a}, "", "a"},
		{[]mediaPlayer{a, b}, "a", "b"},
		{[]mediaPlayer{a, b, c}, "c", "c"},
		{[]mediaPlayer{a, {ID: "b"}}, "b", "b"},
	} {
		if got := pickActive(tc.ps, tc.last); got != tc.want {
			t.Errorf("%v, %q: %q", tc.ps, tc.last, got)
		}
	}
}

func TestShortName(t *testing.T) {
	for in, want := range map[string]string{
		"spotify": "spotify", "firefox.instance_1_84": "firefox", "VLC": "vlc", "chromium.instance123": "chromium",
	} {
		if got := shortName(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
