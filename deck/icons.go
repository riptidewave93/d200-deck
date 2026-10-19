package deck

import (
	"errors"
	"fmt"
	"slices"

	"github.com/riptidewave93/d200-deck/internal/svgpath"
)

// Icon is a config's icon: single-colour SVG path data D (filled nonzero,
// drawn in the button's colour) in ViewBox, or PNG, a small picture drawn as
// it is. The icons package has a catalog.
type Icon struct {
	D       string `json:"d,omitempty"`
	ViewBox string `json:"viewBox,omitempty"`
	PNG     string `json:"png,omitempty"` // base64
}

// Icon limits.
const (
	MaxIcons     = 256
	MaxIconPath  = 16 << 10 // bytes of path data
	MaxIconBytes = 24 << 10 // one PNG icon, before base64
	MaxIconSide  = 128      // pixels
)

func validateIcon(ic Icon) error {
	if ic.PNG != "" {
		if ic.D != "" || ic.ViewBox != "" {
			return errors.New("png and d are both set")
		}
		if _, err := decode(ic.PNG, MaxIconBytes); err != nil {
			return err
		}
		size, err := ImageSize(ic.PNG)
		if err != nil {
			return err
		}
		if size.X > MaxIconSide || size.Y > MaxIconSide {
			return fmt.Errorf("%dx%d pixels; at most %dx%d", size.X, size.Y, MaxIconSide, MaxIconSide)
		}
		return nil
	}
	if ic.D == "" || len(ic.D) > MaxIconPath {
		return fmt.Errorf("needs d, up to %d bytes, or png", MaxIconPath)
	}
	if _, _, _, _, err := svgpath.ViewBox(ic.ViewBox); err != nil {
		return err
	}
	_, err := svgpath.Parse(ic.D)
	return err
}

// IconsUsed lists the icons c's keys can show, sorted: their own, their on
// looks', and their actions' built-in looks.
func (c *Config) IconsUsed() []string {
	var names []string
	for _, p := range c.Pages {
		for _, b := range p.Keys {
			for _, on := range []bool{false, true} {
				if s := b.Shown(on); s.Icon != "" && s.Image == "" {
					names = append(names, s.Icon)
				}
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// MissingIcons lists the icons c's keys can show that aren't in c.Icons.
// The device draws them blank; the configuration website should add them
// (icons.Embed) before saving.
func (c *Config) MissingIcons() []string {
	var missing []string
	for _, n := range c.IconsUsed() {
		if _, ok := c.Icons[n]; !ok {
			missing = append(missing, n)
		}
	}
	return missing
}

// CheckIcons fails if c's keys can show icons it doesn't have. Configs
// written to a device should pass; ones already there are shown anyway.
func (c *Config) CheckIcons() error {
	if m := c.MissingIcons(); len(m) > 0 {
		return fmt.Errorf("icons not in the config: %q", m)
	}
	return nil
}
