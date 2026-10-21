//go:build !linux && !windows

package main

func (p program) candidates() []candidate { return nil }

func findProgram([]candidate) func() error { return nil }
