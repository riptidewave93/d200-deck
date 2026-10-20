package deckd

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/riptidewave93/d200-deck/deck"
	"github.com/riptidewave93/d200-deck/proto"
)

// configFile is the config deckd shows, as a file: what clients read and
// replace over the config channel. Reads come from flash, so the file isn't
// kept in memory too, but for the default config's, shown without a good file.
type configFile struct {
	path string
	cfg  *deck.Config // parsed
	rev  string
	data blob
	gz   *blob // data gzipped, made when first read
}

// blob is bytes to read from: a file kept open, so it's still the same
// bytes after a rename replaces it, or memory.
type blob struct {
	r    io.ReaderAt // *os.File or *bytes.Reader
	size int
}

func (b blob) read(off, n int) ([]byte, error) {
	off = min(off, b.size)
	buf := make([]byte, min(n, b.size-off))
	_, err := b.r.ReadAt(buf, int64(off))
	if err == io.EOF {
		err = nil
	}
	return buf, err
}

func (b blob) close() {
	if f, ok := b.r.(*os.File); ok {
		f.Close()
	}
}

// openBlob opens the file at path.
func openBlob(path string) (blob, error) {
	f, err := os.Open(path)
	if err != nil {
		return blob{}, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return blob{}, err
	}
	return blob{f, int(fi.Size())}, nil
}

// set shows cfg: from mem if it isn't nil, else from the file, whose rev is
// given or worked out. gz, if not nil, is the file gzipped.
func (f *configFile) set(cfg *deck.Config, mem []byte, rev string, gz []byte) error {
	data := blob{bytes.NewReader(mem), len(mem)}
	if mem == nil {
		var err error
		if data, err = openBlob(f.path); err != nil {
			return err
		}
		if rev == "" {
			h := sha256.New()
			io.Copy(h, io.NewSectionReader(data.r, 0, int64(data.size)))
			rev = hex.EncodeToString(h.Sum(nil))
		}
	} else {
		sum := sha256.Sum256(mem)
		rev = hex.EncodeToString(sum[:])
	}
	f.data.close()
	if f.gz != nil {
		f.gz.close()
	}
	f.cfg, f.rev, f.data, f.gz = cfg, rev, data, nil
	if gz != nil {
		f.keepGzip(gz)
	}
	return nil
}

// keepGzip keeps gz as the config gzipped: on flash next to a file.
func (f *configFile) keepGzip(gz []byte) {
	if _, ok := f.data.r.(*os.File); !ok {
		f.gz = &blob{bytes.NewReader(gz), len(gz)}
		return
	}
	path := f.path + ".gz"
	err := writeFile(path, gz)
	var b blob
	if err == nil {
		b, err = openBlob(path)
	}
	if err != nil {
		log.Printf("config: %v", err)
		return
	}
	f.gz = &b
}

// gzip is the config gzipped, made once per version.
func (f *configFile) gzip() (blob, error) {
	if f.gz == nil {
		all, err := f.data.read(0, f.data.size)
		if err != nil {
			return blob{}, err
		}
		f.keepGzip(proto.Gzip(all))
		if f.gz == nil {
			return blob{}, errors.New("can't gzip the config")
		}
	}
	return *f.gz, nil
}

func (f *configFile) info() proto.ConfigInfo {
	return proto.ConfigInfo{Rev: f.rev, Size: f.data.size}
}

// upload is a config being written.
type upload struct {
	proto.ConfigWrite
	b  []byte
	gz []byte // as sent, if gzipped: kept for reads, saving gzipping it again
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
			gz, err := d.file.gzip()
			if err != nil {
				return failed(err)
			}
			info.GzipSize = gz.size
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
		b := d.file.data
		if len(p) == 7 && p[6]&proto.ConfigReadGzip != 0 {
			var err error
			if b, err = d.file.gzip(); err != nil {
				return failed(err)
			}
		}
		part, err := b.read(off, n)
		if err != nil {
			return failed(err)
		}
		return proto.StatusOK, part
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
		u.gz, u.b = u.b, b
	}
	sum := sha256.Sum256(u.b)
	if len(u.b) != u.Size || hex.EncodeToString(sum[:]) != u.SHA256 {
		return failed(damaged)
	}
	if u.Base != "" && u.Base != d.file.info().Rev {
		return failed(fmt.Errorf("the config changed since you read it; read it again"))
	}
	c, err := deck.ParseStrict(u.b)
	if err == nil {
		err = c.CheckIcons()
	}
	if err != nil {
		return failed(err)
	}
	if n := newActions(d.file.cfg, c); n > 0 {
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
	if err := d.file.set(c, nil, u.SHA256, u.gz); err != nil {
		log.Printf("config: %v", err)
		return failed(err)
	}
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
