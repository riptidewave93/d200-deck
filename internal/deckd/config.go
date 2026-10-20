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
	gz   *gzipped // b gzipped, made when first read
}

type gzipped struct {
	rev string
	b   []byte
}

// gzip is the config gzipped, made once per version.
func (f *configFile) gzip() []byte {
	rev := f.info().Rev
	if f.gz == nil || f.gz.rev != rev {
		f.gz = &gzipped{rev, proto.Gzip(f.b)}
	}
	return f.gz.b
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

// sent is how many bytes the client sends: the config, or it gzipped.
func (u *upload) sent() int {
	if u.GzipSize > 0 {
		return u.GzipSize
	}
	return u.Size
}

func failed(err error) (byte, []byte) { return proto.StatusFailed, []byte(err.Error()) }

func okJSON(v any) (byte, []byte) {
	b, _ := json.Marshal(v)
	return proto.StatusOK, b
}

// configRequest answers the config commands.
func (d *deckd) configRequest(req proto.Request) (byte, []byte) {
	d.fileMu.Lock()
	defer d.fileMu.Unlock()
	p := req.Payload
	switch req.Cmd {
	case proto.CmdConfigInfo:
		info := d.file.info()
		var q proto.ConfigInfoRequest
		if len(p) > 0 && json.Unmarshal(p, &q) != nil {
			return proto.StatusBadRequest, nil
		}
		if q.Gzip {
			info.GzipSize = len(d.file.gzip())
		}
		return okJSON(info)
	case proto.CmdConfigRead:
		if len(p) != 6 && len(p) != 7 {
			return proto.StatusBadRequest, nil
		}
		off, n := int(binary.LittleEndian.Uint32(p)), int(binary.LittleEndian.Uint16(p[4:]))
		if n == 0 {
			n = proto.ConfigReadSize
		}
		if n > proto.MaxConfigRead {
			return proto.StatusBadRequest, nil
		}
		b := d.file.b
		if len(p) == 7 && p[6]&proto.ConfigReadGzip != 0 {
			b = d.file.gzip()
		}
		off = min(off, len(b))
		return proto.StatusOK, b[off:min(off+n, len(b))]
	case proto.CmdConfigWrite:
		var w proto.ConfigWrite
		if json.Unmarshal(p, &w) != nil || w.Size <= 0 || w.Size > deck.MaxConfigBytes ||
			w.GzipSize < 0 || w.GzipSize > deck.MaxConfigBytes {
			return proto.StatusBadRequest, nil
		}
		d.upload = &upload{ConfigWrite: w}
		d.upload.b = make([]byte, 0, d.upload.sent())
		return proto.StatusOK, nil
	case proto.CmdConfigData:
		u := d.upload
		if u == nil || len(p) < 4 || int(binary.LittleEndian.Uint32(p)) != len(u.b) || len(u.b)+len(p)-4 > u.sent() {
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
		return d.commit(req.Seq, u)
	}
	return proto.StatusUnknownCmd, nil
}

// commit saves and shows upload u, if it's whole, based on the config
// there now and valid. If it gives keys new actions, the device asks first,
// so a website or program can't make keys run things behind the user's back.
func (d *deckd) commit(seq uint16, u *upload) (byte, []byte) {
	damaged := fmt.Errorf("the config arrived incomplete or damaged; send it again")
	if u.GzipSize > 0 {
		if len(u.b) != u.GzipSize {
			return failed(damaged)
		}
		b, err := proto.Gunzip(u.b, u.Size)
		if err != nil {
			return failed(damaged)
		}
		u.b = b
	}
	sum := sha256.Sum256(u.b)
	if len(u.b) != u.Size || hex.EncodeToString(sum[:]) != u.SHA256 {
		return failed(damaged)
	}
	if u.Base != "" && u.Base != d.file.info().Rev {
		return failed(fmt.Errorf("the config changed since you read it; read it again"))
	}
	c, err := parseConfig(u.b)
	if err == nil {
		err = c.CheckIcons()
	}
	if err != nil {
		return failed(err)
	}
	old, _ := deck.Parse(d.file.b)
	if old == nil {
		old = &deck.Config{}
	}
	if n := newActions(old, c); n > 0 {
		base := d.file.info().Rev
		return d.ask(seq, "Save keys?", plural(n, "new\nkey action", "new\nkey actions"), "Save", func() (byte, []byte) {
			d.fileMu.Lock()
			defer d.fileMu.Unlock()
			if d.file.info().Rev != base {
				return failed(fmt.Errorf("the config changed since you read it; read it again"))
			}
			return d.save(u, c)
		})
	}
	return d.save(u, c)
}

// save writes and shows config c, from upload u.
func (d *deckd) save(u *upload, c *deck.Config) (byte, []byte) {
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
