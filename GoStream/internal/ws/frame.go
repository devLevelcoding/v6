package ws

import (
	"encoding/binary"
	"errors"
	"io"
	"time"
)

func (c *Conn) readFrame() (fin bool, op Opcode, masked bool, payload []byte, err error) {
	h := c.rhdr[:2]
	if _, err = io.ReadFull(c.br, h); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	if h[0]&0x70 != 0 {
		return false, 0, false, nil, errRSV
	}
	op = Opcode(h[0] & 0x0f)
	masked = h[1]&0x80 != 0
	length := int64(h[1] & 0x7f)

	switch length {
	case 126:
		ext := c.rhdr[:2]
		if _, err = io.ReadFull(c.br, ext); err != nil {
			return
		}
		length = int64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := c.rhdr[:8]
		if _, err = io.ReadFull(c.br, ext); err != nil {
			return
		}
		length = int64(binary.BigEndian.Uint64(ext))
	}
	// Hard cap so a bogus length can't drive a huge allocation; the friendlier
	// 1009 for an over-limit *message* is raised in ReadMessage.
	if length < 0 || length > c.readLim+1<<16 {
		return false, 0, masked, nil, errFrameRange
	}

	buf := c.scratch(int(length))
	if masked {
		mask := c.rhdr[:4]
		if _, err = io.ReadFull(c.br, mask); err != nil {
			return
		}
		if _, err = io.ReadFull(c.br, buf); err != nil {
			return
		}
		maskBytes([4]byte{mask[0], mask[1], mask[2], mask[3]}, 0, buf)
		return fin, op, true, buf, nil
	}
	_, err = io.ReadFull(c.br, buf)
	return fin, op, false, buf, err
}

// Sentinel framing errors, package-level so they don't heap-allocate per frame
// (CoverGo U4).
var (
	errRSV        = errors.New("ws: RSV bits set (no extensions negotiated)")
	errFrameRange = errors.New("ws: frame length out of range")
)

// scratch returns c.readBuf resized to n, growing (and keeping) the backing
// array so consecutive frames don't each allocate. The slice is only valid
// until the next readFrame — ReadMessage copies the payload out before then.
func (c *Conn) scratch(n int) []byte {
	if cap(c.readBuf) < n {
		c.readBuf = make([]byte, n, max(n, 2*cap(c.readBuf)))
	}
	return c.readBuf[:n]
}

func (c *Conn) writeFrame(op Opcode, data []byte) error {
	if c.writeTO > 0 {
		_ = c.nc.SetWriteDeadline(time.Now().Add(c.writeTO))
	}
	// c.whdr is per-connection scratch — no per-frame header allocation (U4).
	b0 := byte(0x80) | byte(op) // FIN=1
	var head []byte
	switch n := len(data); {
	case n <= 125:
		c.whdr[0], c.whdr[1] = b0, byte(n)
		head = c.whdr[:2]
	case n <= 0xffff:
		c.whdr[0], c.whdr[1] = b0, 126
		c.whdr[2], c.whdr[3] = byte(n>>8), byte(n)
		head = c.whdr[:4]
	default:
		c.whdr[0], c.whdr[1] = b0, 127
		binary.BigEndian.PutUint64(c.whdr[2:], uint64(n))
		head = c.whdr[:10]
	}
	if _, err := c.nc.Write(head); err != nil {
		return err
	}
	_, err := c.nc.Write(data)
	return err
}
