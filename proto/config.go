package proto

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

// ReadConfig reads the device's config file and names its version. It
// moves it gzipped, or plain while the device is still making the gzip
// stream after a save.
func (c *Conn) ReadConfig() ([]byte, ConfigInfo, error) {
	info, err := c.configInfo()
	if err != nil {
		return nil, info, err
	}
	size, flags := info.Size, []byte(nil)
	if info.GzipSize > 0 {
		size, flags = info.GzipSize, []byte{ConfigReadGzip}
	}
	out := make([]byte, 0, size)
	for len(out) < size {
		req := make([]byte, 6)
		binary.LittleEndian.PutUint32(req, uint32(len(out)))
		binary.LittleEndian.PutUint16(req[4:], MaxConfigRead)
		part, err := c.Do(CmdConfigRead, append(req, flags...))
		if err != nil {
			return nil, info, err
		}
		if len(part) == 0 {
			break
		}
		out = append(out, part...)
	}
	if info.GzipSize > 0 {
		if out, err = Gunzip(out, info.Size); err != nil {
			return nil, info, err
		}
	}
	if sum := sha256.Sum256(out); hex.EncodeToString(sum[:]) != info.Rev {
		return nil, info, errors.New("the config changed while it was read; read it again")
	}
	info.GzipSize = 0
	return out, info, nil
}

// configInfo asks for the config's version and, once the device has it, its
// gzipped size.
func (c *Conn) configInfo() (ConfigInfo, error) {
	var info ConfigInfo
	q, _ := json.Marshal(ConfigInfoRequest{Gzip: true})
	b, err := c.Do(CmdConfigInfo, q)
	if err == nil {
		err = json.Unmarshal(b, &info)
	}
	return info, err
}

// WriteConfig replaces the device's config with b, if it's still version
// base ("" replaces any). It moves it gzipped.
func (c *Conn) WriteConfig(base string, b []byte) (ConfigInfo, error) {
	var info ConfigInfo
	sum := sha256.Sum256(b)
	data := Gzip(b)
	cw := ConfigWrite{Base: base, Size: len(b), SHA256: hex.EncodeToString(sum[:]), GzipSize: len(data)}
	w, _ := json.Marshal(cw)
	if _, err := c.Do(CmdConfigWrite, w); err != nil {
		return info, err
	}
	for off := 0; off < len(data); off += MaxConfigData {
		part := make([]byte, 4, 4+MaxConfigData)
		binary.LittleEndian.PutUint32(part, uint32(off))
		part = append(part, data[off:min(off+MaxConfigData, len(data))]...)
		if _, err := c.Do(CmdConfigData, part); err != nil {
			return info, err
		}
	}
	info = ConfigInfo{}
	r, err := c.DoWait(CmdConfigCommit, nil, ConfirmTimeout+ReplyTimeout)
	if err == nil {
		err = json.Unmarshal(r, &info)
	}
	return info, err
}

// Gzip compresses a config for transfer. Below BestCompression, Go's
// compressor stores base64 image data as it is, finding no repeats; the
// device gzips once per config version, so the time is worth it.
func Gzip(b []byte) []byte {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	w.Write(b)
	w.Close()
	return buf.Bytes()
}

// Gunzip decompresses a config of size bytes; it fails if it's another size.
func Gunzip(b []byte, size int) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	out, err := io.ReadAll(io.LimitReader(r, int64(size)+1))
	if err != nil {
		return nil, err
	}
	if len(out) != size {
		return nil, errors.New("the gzipped config isn't the size it should be")
	}
	return out, nil
}
