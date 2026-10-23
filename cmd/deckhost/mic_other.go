//go:build !linux && !windows && !darwin

package main

import "errors"

func openMic() (micBackend, error) { return nil, errors.ErrUnsupported }
