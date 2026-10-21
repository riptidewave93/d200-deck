//go:build !linux && !windows && !darwin

package main

var programCandidates = map[string][]candidate{}

func findProgram([]candidate) func() error { return nil }
