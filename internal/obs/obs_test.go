package obs_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/riptidewave93/d200-deck/internal/obs"
	"github.com/riptidewave93/d200-deck/internal/obs/obstest"
)

func TestDial(t *testing.T) {
	fake := obstest.New("hunter2")
	defer fake.Close()
	if _, err := obs.Dial(fake.URL, ""); !errors.Is(err, obs.ErrAuth) {
		t.Errorf("no password: %v", err)
	}
	if _, err := obs.Dial(fake.URL, "wrong"); !errors.Is(err, obs.ErrAuth) {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := obs.Dial("ws://127.0.0.1:1", ""); !errors.Is(err, obs.ErrNotRunning) {
		t.Errorf("nothing there: %v", err)
	}
	c, err := obs.Dial(fake.URL, "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	b, err := c.Request("GetCurrentProgramScene", nil)
	var scene struct {
		Name string `json:"currentProgramSceneName"`
	}
	if err != nil || json.Unmarshal(b, &scene) != nil || scene.Name != "Main" {
		t.Errorf("scene %q, %v", b, err)
	}
	if _, err := c.Request("Nonsense", nil); err == nil {
		t.Error("an unknown request worked")
	}
	if _, err := c.Request("SetCurrentProgramScene", map[string]string{"sceneName": "BRB"}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-c.Events():
		if e.Type != "CurrentProgramSceneChanged" {
			t.Errorf("event %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}

	fake.Close()
	select {
	case _, ok := <-c.Events():
		if ok {
			t.Error("an event after closing")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("events not closed")
	}
	if c.Err() == nil {
		t.Error("no error after the end")
	}
	if _, err := c.Request("GetCurrentProgramScene", nil); err == nil {
		t.Error("a request worked after the end")
	}
}
