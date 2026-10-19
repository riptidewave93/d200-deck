//go:build !linux && !windows

package main

import "errors"

func openMedia() (mediaBackend, error) { return nil, errors.ErrUnsupported }
