package deck

import (
	"encoding/json"
	"testing"
)

func TestDefaultConfigValid(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if err := back.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTypeKey(t *testing.T) {
	for _, r := range `Hello, World! ~_+{}|:\"<>?` {
		if _, _, ok := TypeKey(r); !ok {
			t.Errorf("can't type %q", r)
		}
	}
	if _, _, ok := TypeKey('é'); ok {
		t.Error("typed a non-ASCII rune")
	}
}
