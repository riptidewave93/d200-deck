package proto

import "testing"

// oneShot answers nothing; it records the requests written.
type oneShot struct{ seqs []uint16 }

func (o *oneShot) WriteReport(b []byte) error {
	r, _ := DecodeRequest(b)
	o.seqs = append(o.seqs, r.Seq)
	return nil
}
func (o *oneShot) ReadReport([]byte) (int, error) { select {} }
func (o *oneShot) Close() error                   { return nil }

func TestSeqSkipsEventSeq(t *testing.T) {
	old := ReplyTimeout
	ReplyTimeout = 0
	defer func() { ReplyTimeout = old }()
	o := &oneShot{}
	c := NewConn(o)
	c.seq = 0xfffe
	for range 3 {
		c.Do(CmdHello, nil)
	}
	if len(o.seqs) != 3 || o.seqs[0] != 0xffff || o.seqs[1] != 1 || o.seqs[2] != 2 {
		t.Errorf("seqs %v", o.seqs)
	}
}
