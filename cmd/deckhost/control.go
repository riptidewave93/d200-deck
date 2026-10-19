package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/riptidewave93/d200-deck/deck"
)

// The control socket takes one command per connection, as a line of text,
// and answers with one line: JSON for status, "ok" or "error: ..." otherwise.

func socketPath() (string, error) {
	dir, err := runtimeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "deckhost.sock"), nil
}

func listenControl(d *daemon) (net.Listener, error) {
	path, err := socketPath()
	if err != nil {
		return nil, err
	}
	lock, err := lockInstance(path + ".lock")
	if err != nil {
		return nil, err
	}
	d.lock = lock
	os.Remove(path) // left by a daemon that died
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := secureSocket(path); err != nil {
		l.Close()
		return nil, err
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go d.serveControl(c, l)
		}
	}()
	return l, nil
}

func (d *daemon) serveControl(c net.Conn, l net.Listener) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return
	}
	reply := func(err error) {
		if err != nil {
			fmt.Fprintf(c, "error: %v\n", err)
		} else {
			fmt.Fprintln(c, "ok")
		}
	}
	cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	if arg != "" && cmd != "update" {
		reply(errors.New("unknown command"))
		return
	}
	switch cmd {
	case "status":
		b, _ := json.Marshal(d.getStatus())
		fmt.Fprintf(c, "%s\n", b)
	case "configure":
		reply(openURL(deck.ConfigureURL))
	case "update":
		u, err := d.getStatus().UpdateURL(arg)
		if err == nil {
			err = openURL(u)
		}
		reply(err)
	case "restart":
		reply(nil)
		c.Close()
		l.Close()
		log.Fatalf("restart: %v", restartSelf(d.lock))
	case "exit":
		reply(nil)
		c.Close()
		l.Close()
		log.Print("exit requested")
		os.Exit(0)
	default:
		reply(errors.New("unknown command"))
	}
}

// control sends one command to the daemon and returns its answer.
func control(cmd string) (string, error) {
	path, err := socketPath()
	if err != nil {
		return "", err
	}
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return "", fmt.Errorf("deckhost isn't running: %w", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintln(c, cmd); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if msg, ok := strings.CutPrefix(line, "error: "); ok {
		return "", errors.New(msg)
	}
	return line, nil
}
