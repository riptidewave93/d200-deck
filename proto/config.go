package proto

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// ReadConfig reads the device's config file and names its version.
func (c *Conn) ReadConfig() ([]byte, ConfigInfo, error) {
	var info ConfigInfo
	b, err := c.Do(CmdConfigInfo, nil)
	if err == nil {
		err = json.Unmarshal(b, &info)
	}
	if err != nil {
		return nil, info, err
	}
	out := make([]byte, 0, info.Size)
	for len(out) < info.Size {
		req := make([]byte, 6)
		binary.LittleEndian.PutUint32(req, uint32(len(out)))
		binary.LittleEndian.PutUint16(req[4:], MaxConfigRead)
		part, err := c.Do(CmdConfigRead, req)
		if err != nil {
			return nil, info, err
		}
		if len(part) == 0 {
			break
		}
		out = append(out, part...)
	}
	if sum := sha256.Sum256(out); hex.EncodeToString(sum[:]) != info.Rev {
		return nil, info, errors.New("the config changed while it was read; read it again")
	}
	return out, info, nil
}

// WriteConfig replaces the device's config with b, if it's still version
// base ("" replaces any).
func (c *Conn) WriteConfig(base string, b []byte) (ConfigInfo, error) {
	var info ConfigInfo
	sum := sha256.Sum256(b)
	w, _ := json.Marshal(ConfigWrite{Base: base, Size: len(b), SHA256: hex.EncodeToString(sum[:])})
	if _, err := c.Do(CmdConfigWrite, w); err != nil {
		return info, err
	}
	for off := 0; off < len(b); off += MaxConfigData {
		part := make([]byte, 4, 4+MaxConfigData)
		binary.LittleEndian.PutUint32(part, uint32(off))
		part = append(part, b[off:min(off+MaxConfigData, len(b))]...)
		if _, err := c.Do(CmdConfigData, part); err != nil {
			return info, err
		}
	}
	r, err := c.DoWait(CmdConfigCommit, nil, ConfirmTimeout+ReplyTimeout)
	if err == nil {
		err = json.Unmarshal(r, &info)
	}
	return info, err
}
