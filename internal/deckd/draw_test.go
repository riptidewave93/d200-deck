package deckd

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"slices"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/render"
)

func TestFlash(t *testing.T) {
	d := newTestDeckd()
	d.flash(2)
	if got := keyColour(d, 2); got != render.Colours[render.Identify] {
		t.Errorf("identify colour %#x", got)
	}
	d.flashError(3)
	if got := keyColour(d, 3); got != render.Colours[render.Error] {
		t.Errorf("error colour %#x", got)
	}
	select {
	case i := <-d.redraw:
		if i != 3 {
			t.Errorf("redraw of key %d", i)
		}
	case <-time.After(2 * time.Second):
		t.Error("error flash never ends")
	}
}

// After identify's white screen, the whole screen is as it was, the gaps
// between keys included.
func TestFlashAllRestores(t *testing.T) {
	d := newTestDeckd()
	d.drawPage()
	before := slices.Clone(d.canvas.Pix)
	d.flashAll()
	if slices.ContainsFunc(d.canvas.Pix, func(p uint32) bool { return p != 0xffffff }) {
		t.Fatal("not all white")
	}
	d.drawKey(0, render.Idle) // drawn during the flash: waits for it to end
	runUI(t, d)
	if !slices.Equal(d.canvas.Pix, before) {
		t.Error("the screen isn't as it was before identify")
	}
}

func TestAnimatedKeys(t *testing.T) {
	d := newTestDeckd()
	d.cfg.Images = map[string]string{"anim": animGIF(t)}
	d.cfg.Pages["main"].Keys[11] = deck.Button{Image: "anim"}
	d.renderer.SetConfig(d.cfg)
	d.drawPage()
	if d.frameAt[11].IsZero() || !d.frameAt[0].IsZero() {
		t.Fatalf("frames at %v", d.frameAt)
	}
	first := keyColour(d, 11)
	select {
	case <-d.frames.C:
		d.nextFrames()
	case <-time.After(time.Second):
		t.Fatal("no frame")
	}
	if keyColour(d, 11) == first {
		t.Error("the next frame wasn't drawn")
	}
	d.press(0, true) // to another page: nothing animated
	if !d.frameAt[11].IsZero() {
		t.Error("still animating a key on another page")
	}
}

// animGIF is a base64 GIF of two 196 px frames, red then green, 50 ms each.
func animGIF(t *testing.T) string {
	pal := color.Palette{color.RGBA{0xff, 0, 0, 0xff}, color.RGBA{0, 0xff, 0, 0xff}}
	g := &gif.GIF{}
	for i := range 2 {
		f := image.NewPaletted(image.Rect(0, 0, 196, 196), pal)
		for j := range f.Pix {
			f.Pix[j] = uint8(i)
		}
		g.Image = append(g.Image, f)
		g.Delay = append(g.Delay, 5)
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}
