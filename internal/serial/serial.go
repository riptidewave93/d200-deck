// Package serial reads the D200's CPU serial, which is also its USB serial.
package serial

import (
	"errors"
	"os"
	"strings"
)

// Path is where the kernel publishes the serial.
var Path = "/proc/device-tree/serial-number"

// Read returns the serial.
func Read() (string, error) {
	b, err := os.ReadFile(Path)
	if err != nil {
		return "", err
	}
	s := strings.TrimRight(string(b), "\x00\n")
	if s == "" {
		return "", errors.New("empty serial")
	}
	return s, nil
}
