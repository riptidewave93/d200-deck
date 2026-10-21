package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// installClient downloads releases and sends them to devices: they take
// longer than artClient allows.
var installClient = &http.Client{Timeout: 10 * time.Minute}

// maxBundle bounds a downloaded update bundle.
const maxBundle = 1 << 30

// startInstall starts sending the newest OS release to the device Pick picks
// for serial. The user then confirms it on the device's update page.
func (d *daemon) startInstall(serial string) error {
	dev, err := d.getStatus().Pick(serial)
	if err != nil {
		return err
	}
	up := dev.OSUpdate
	if up == nil {
		return fmt.Errorf("no update for D200 %s", shortSerial(dev.Hello.Serial))
	}
	base, err := dev.webURL()
	if err != nil {
		return err
	}
	if !d.setInstall(dev.Path, "", "Downloading "+up.Version+"…") {
		return errors.New("already updating it")
	}
	go func() {
		err := d.install(dev.Path, *up, base)
		if err != nil {
			log.Printf("%s: update: %v", dev.Path, err)
			d.os.notify("Device update failed", err.Error(), "")
			if err := openURL(up.URL); err != nil {
				log.Printf("release page: %v", err)
			}
		} else {
			if err := openURL(base + "/update"); err != nil {
				log.Printf("update page: %v", err)
			}
			d.os.notify("Press Install on the device", up.Version+" is on the device, waiting for you.", "")
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if cur := d.devices[dev.Path]; cur != nil {
			cur.Install = ""
			if err != nil {
				cur.InstallError = err.Error()
			}
		}
	}()
	return nil
}

// setInstall sets what device path's install is doing, if it's doing was;
// was "" starts one, clearing the last one's error.
func (d *daemon) setInstall(path, was, now string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	dev := d.devices[path]
	if dev == nil || dev.Install != was {
		return false
	}
	if was == "" {
		dev.InstallError = ""
	}
	dev.Install = now
	return true
}

// install downloads rel's file and sends it to the device at base.
func (d *daemon) install(path string, rel Release, base string) error {
	f, err := download(rel)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	d.setInstall(path, "Downloading "+rel.Version+"…", "Sending to device…")
	return sendBundle(base, f)
}

// download saves rel's file to a temporary file, checked against the
// release's sums.
func download(rel Release) (*os.File, error) {
	var sums bytes.Buffer
	if err := fetch(rel.urls[readyFile], &sums, 1<<20); err != nil {
		return nil, err
	}
	want, err := sumOf(sums.Bytes(), rel.File)
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "d200-update-*.tar")
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	err = fetch(rel.urls[rel.File], io.MultiWriter(f, h), maxBundle)
	if err == nil && hex.EncodeToString(h.Sum(nil)) != want {
		err = fmt.Errorf("%s doesn't match %s: try again", rel.File, readyFile)
	}
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	return f, nil
}

// fetch downloads url, up to max bytes, to w.
func fetch(url string, w io.Writer, max int64) error {
	if url == "" {
		return errors.New("the release has no such file")
	}
	resp, err := installClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading: %s", resp.Status)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, max+1))
	if err == nil && n > max {
		err = errors.New("downloading: the file is too big")
	}
	return err
}

// sumOf finds file's SHA-256 in sums, as sha256sum writes them.
func sumOf(sums []byte, file string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		sum, name, ok := strings.Cut(sc.Text(), " ")
		name = strings.TrimLeft(name, " *")
		if ok && name == file && len(sum) == 64 {
			return strings.ToLower(sum), nil
		}
	}
	return "", fmt.Errorf("%s isn't in %s", file, readyFile)
}

// sendBundle uploads bundle f to the device's update page at base, which
// then asks for a key press on the device.
func sendBundle(base string, f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", base+"/update/bundle", f)
	if err != nil {
		return err
	}
	req.ContentLength = fi.Size()
	req.Header.Set("X-Deck-Update", "deckhost")
	resp, err := installClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending it to the device: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("the device didn't take it: %s", strings.TrimSpace(string(b)))
	}
	return nil
}

// installCommand is deckhost install: it has the daemon send the update and
// shows how it goes.
func installCommand(serial string) error {
	if _, err := control(strings.TrimSpace("install " + serial)); err != nil {
		return err
	}
	shown := ""
	for {
		time.Sleep(500 * time.Millisecond)
		s, running := daemonStatus()
		if !running {
			return errors.New("deckhost stopped")
		}
		dev, err := s.Pick(serial)
		switch {
		case err != nil:
			return err
		case dev.Install != "":
			if dev.Install != shown {
				fmt.Println(dev.Install)
				shown = dev.Install
			}
		case dev.InstallError != "":
			return errors.New(dev.InstallError)
		default:
			fmt.Println("Press Install on the device.")
			return nil
		}
	}
}
