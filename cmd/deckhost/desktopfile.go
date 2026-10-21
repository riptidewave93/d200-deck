package main

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// desktopEntry is what deckhost needs from a desktop file (the freedesktop
// Desktop Entry Specification).
type desktopEntry struct {
	Exec    []string // the command, its field codes dropped
	TryExec string
	Path    string // the directory to run it in
	Hidden  bool
}

// parseDesktopEntry reads the [Desktop Entry] group of a desktop file.
func parseDesktopEntry(r io.Reader) (desktopEntry, error) {
	var e desktopEntry
	var exec string
	in := false
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			in = line == "[Desktop Entry]"
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !in || !ok {
			continue
		}
		k, v = strings.TrimSpace(k), unescapeDesktopValue(strings.TrimSpace(v))
		switch k {
		case "Type":
			if v != "Application" {
				return e, errors.New("not an application")
			}
		case "Exec":
			exec = v
		case "TryExec":
			e.TryExec = v
		case "Path":
			e.Path = v
		case "Hidden":
			e.Hidden = v == "true"
		}
	}
	if err := sc.Err(); err != nil {
		return e, err
	}
	argv, err := splitExec(exec)
	if err != nil {
		return e, err
	}
	if len(argv) == 0 {
		return e, errors.New("no Exec")
	}
	e.Exec = argv
	return e, nil
}

// unescapeDesktopValue undoes a string value's escapes: \s \n \t \r \\.
func unescapeDesktopValue(v string) string {
	if !strings.Contains(v, `\`) {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+1 < len(v) {
			if r, ok := map[byte]string{'s': " ", 'n': "\n", 't': "\t", 'r': "\r", '\\': `\`}[v[i+1]]; ok {
				b.WriteString(r)
				i++
				continue
			}
		}
		b.WriteByte(v[i])
	}
	return b.String()
}

// splitExec splits an Exec value into arguments: spaces separate them,
// double quotes group them (with \" \` \$ \\ inside), and field codes such
// as %U are dropped, as deckhost opens no files.
func splitExec(s string) ([]string, error) {
	var argv []string
	var cur strings.Builder
	have, quoted := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted && c == '\\' && i+1 < len(s) && strings.IndexByte("\"`$\\", s[i+1]) >= 0:
			cur.WriteByte(s[i+1])
			i++
		case c == '"':
			quoted, have = !quoted, true
		case !quoted && (c == ' ' || c == '\t'):
			if have {
				argv = append(argv, cur.String())
			}
			cur.Reset()
			have = false
		case c == '%' && i+1 < len(s):
			if s[i+1] == '%' {
				cur.WriteByte('%')
			}
			i++ // a field code: dropped
			have = have || s[i] == '%'
		default:
			cur.WriteByte(c)
			have = true
		}
	}
	if quoted {
		return nil, errors.New("unterminated quote in Exec")
	}
	if have {
		argv = append(argv, cur.String())
	}
	return argv, nil
}
