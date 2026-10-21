//go:build !linux && !windows

package main

func notify(title, body, url string) {}
