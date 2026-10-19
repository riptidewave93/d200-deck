package deckd

import (
	"testing"

	"github.com/riptidewave93/d200-deck/internal/gfx"
	"github.com/riptidewave93/d200-deck/internal/ota"
)

func TestUpdateScreens(t *testing.T) {
	for name, o := range map[string]*overlay{
		"confirm":  confirmScreen(ota.ConfirmRequest{Version: "v", Flavour: "debug"}, func(bool) {}),
		"older":    confirmScreen(ota.ConfirmRequest{Version: "v", Flavour: "debug", Older: true}, func(bool) {}),
		"progress": progressScreen("Updating", "Kernel", 2, 4),
		"failed":   progressScreen(ota.StepFailed, "", 0, 0),
	} {
		for k, b := range o.keys {
			if _, ok := o.drawn[k]; ok {
				t.Errorf("%s: key %d drawn twice", name, k)
			}
			if b.Icon != "" && !gfx.HasIcon(b.Icon) {
				t.Errorf("%s key %d: no icon %q", name, k, b.Icon)
			}
		}
	}
	if _, ok := progressScreen(ota.StepFailed, "", 0, 0).drawn[13]; ok {
		t.Error("warning shown after a failed install")
	}
}
