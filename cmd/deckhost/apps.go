package main

import (
	"encoding/json"
	"fmt"

	"github.com/riptidewave93/d200-deck/deck"
)

// An app integration runs the app actions ({"type": "app"}) of one app. It
// sets state "<app>.ready" while it can run them and "<app>.<op>" for ops
// that are on (see deck/apps.go). Its settings, such as the user's app
// registration, are in <app>.json in configDir.
type app interface {
	// run keeps it connected until stop is closed.
	run(stop <-chan struct{})
	// do runs op for a key's press, or its release.
	do(op string, args json.RawMessage, release bool) error
	// title is the app's name for people.
	title() string
	// statusText is how it is, for the tray; "" if it isn't set up.
	statusText() string
}

// AppStatus is how an app integration is.
type AppStatus struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

// runAction carries out PC action a, or for an app, its release.
func (d *daemon) runAction(a deck.Action, release bool) error {
	if a.Type != deck.ActionApp {
		if release {
			return nil // none of the others need it
		}
		return run(a)
	}
	ap, ok := d.apps[a.App]
	if !ok {
		return fmt.Errorf("deckhost has no %q app; is it up to date?", a.App)
	}
	return ap.do(a.Op, a.Args, release)
}
