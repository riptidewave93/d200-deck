package main

import (
	"bytes"
	"testing"
)

func TestIcoFromPNG(t *testing.T) {
	p := drawIcon(iconBlue, trayIconSize, false)
	ico := icoFromPNG(p)
	if len(ico) != 22+len(p) || ico[2] != 1 || ico[4] != 1 || ico[6] != 64 || ico[7] != 64 || ico[18] != 22 || !bytes.Equal(ico[22:], p) {
		t.Errorf("ico header % x", ico[:22])
	}
}
