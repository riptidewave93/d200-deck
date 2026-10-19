package netid

import (
	"encoding/json"
	"os"
	"testing"
)

// vector is one line of testdata/vectors.json.
type vector struct {
	Serial string `json:"serial"`
	Dev4   string `json:"dev4"`
	Peer4  string `json:"peer4"`
	Net4   string `json:"net4"`
	Dev6   string `json:"dev6"`
	Net6   string `json:"net6"`
	MDNS   string `json:"mdns"`
}

func vectorFor(t *testing.T, serial string) vector {
	id, err := For(serial)
	if err != nil {
		t.Fatal(err)
	}
	return vector{serial, id.Dev4.String(), id.Peer4.String(), id.Net4.String(), id.Dev6.String(), id.Net6.String(), id.MDNS}
}

// TestVectors checks For against testdata/vectors.json. Regenerate the file
// only on purpose (it changes every deck's addresses): UPDATE_VECTORS=1.
func TestVectors(t *testing.T) {
	serials := []string{"00112233445566aa", "FFEEDDCCBBAA9988", "0000000000000000", "1"}
	if os.Getenv("UPDATE_VECTORS") != "" {
		var vs []vector
		for _, s := range serials {
			vs = append(vs, vectorFor(t, s))
		}
		b, _ := json.MarshalIndent(vs, "", "  ")
		os.WriteFile("testdata/vectors.json", append(b, '\n'), 0o644)
	}
	b, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []vector
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	for _, w := range want {
		if got := vectorFor(t, w.Serial); got != w {
			t.Errorf("%s:\n got %+v\nwant %+v", w.Serial, got, w)
		}
	}
}

func TestBadSerial(t *testing.T) {
	for _, s := range []string{"", "  ", "xyz", "00:11", string(make([]byte, 65))} {
		if _, err := For(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
	a, _ := For(" 00112233445566AA\n")
	b, _ := For("00112233445566aa")
	if a != b {
		t.Error("case or whitespace changes the ID")
	}
}

func TestUpdateURL(t *testing.T) {
	id, _ := For("00112233445566aa")
	if got := id.UpdateURL(); got != "http://198.19.23.5/update" {
		t.Errorf("UpdateURL() = %q", got)
	}
}
