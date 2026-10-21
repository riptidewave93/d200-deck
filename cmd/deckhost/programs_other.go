//go:build !linux && !windows

package main

var programCandidates = map[string][]candidate{}

func findProgram([]candidate) func() error { return nil }
