package deckd

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
)

// configFile is the config deckd shows, as a file: what clients read and
// replace over the config channel.
type configFile struct {
	path string
	b    []byte
}

func (f configFile) info() proto.ConfigInfo {
	sum := sha256.Sum256(f.b)
	return proto.ConfigInfo{Rev: hex.EncodeToString(sum[:]), Size: len(f.b)}
}

// upload is a config being written.
type upload struct {
	proto.ConfigWrite
	b []byte
}

func failed(err error) (byte, []byte) { return proto.StatusFailed, []byte(err.Error()) }

func okJSON(v any) (byte, []byte) {
	b, _ := json.Marshal(v)
	return proto.StatusOK, b
}

// configRequest answers the config commands.
func (d *deckd) configRequest(cmd byte, p []byte) (byte, []byte) {
	switch cmd {
	case proto.CmdConfigInfo:
		return okJSON(d.file.info())
	case proto.CmdConfigRead:
		if len(p) != 6 {
			return proto.StatusBadRequest, nil
		}
		off, n := int(binary.LittleEndian.Uint32(p)), int(binary.LittleEndian.Uint16(p[4:]))
		if n == 0 {
			n = proto.ConfigReadSize
		}
		if n > proto.MaxConfigRead {
			return proto.StatusBadRequest, nil
		}
		off = min(off, len(d.file.b))
		return proto.StatusOK, d.file.b[off:min(off+n, len(d.file.b))]
	case proto.CmdConfigWrite:
		var w proto.ConfigWrite
		if json.Unmarshal(p, &w) != nil || w.Size <= 0 || w.Size > deck.MaxConfigBytes {
			return proto.StatusBadRequest, nil
		}
		d.upload = &upload{ConfigWrite: w, b: make([]byte, 0, w.Size)}
		return proto.StatusOK, nil
	case proto.CmdConfigData:
		u := d.upload
		if u == nil || len(p) < 4 || int(binary.LittleEndian.Uint32(p)) != len(u.b) || len(u.b)+len(p)-4 > u.Size {
			return proto.StatusBadRequest, nil
		}
		u.b = append(u.b, p[4:]...)
		return proto.StatusOK, nil
	case proto.CmdConfigCommit:
		u := d.upload
		d.upload = nil
		if u == nil {
			return proto.StatusBadRequest, nil
		}
		return d.commit(u)
	}
	return proto.StatusUnknownCmd, nil
}

func (d *deckd) commit(u *upload) (byte, []byte) {
	sum := sha256.Sum256(u.b)
	if len(u.b) != u.Size || hex.EncodeToString(sum[:]) != u.SHA256 {
		return failed(fmt.Errorf("the config arrived incomplete or damaged; send it again"))
	}
	if u.Base != "" && u.Base != d.file.info().Rev {
		return failed(fmt.Errorf("the config changed since you read it; read it again"))
	}
	c, err := parseConfig(u.b)
	if err != nil {
		return failed(err)
	}
	if err := writeFile(d.file.path, u.b); err != nil {
		log.Printf("config: %v", err)
		return failed(err)
	}
	d.file.b = u.b
	info := d.file.info()
	log.Printf("config: saved %s", info.Rev[:12])
	d.ui <- func() {
		d.cfg = c
		d.setPages([]string{c.Home})
	}
	go func() {
		ev, _ := json.Marshal(info)
		if err := d.ch.send(proto.EventSeq, proto.EventConfigChanged, ev); err != nil {
			log.Printf("config channel: %v", err)
		}
	}()
	return okJSON(info)
}

// writeFile replaces path with b, so a crash leaves the old file or the new
// one, never part of one.
func writeFile(path string, b []byte) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if err2 := f.Close(); err == nil {
		err = err2
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		dir.Sync()
		dir.Close()
	}
	return nil
}
