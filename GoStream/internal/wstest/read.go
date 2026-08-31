package wstest

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
)

// Read returns the next data-frame message (opcode 1/2), transparently
// answering pings and surfacing a close as io.EOF.
func (c *Client) Read() (opcode byte, payload []byte, err error) {
	for {
		fin, op, p, e := c.readFrame()
		if e != nil {
			return 0, nil, e
		}
		switch op {
		case 0x8:
			return 0, nil, io.EOF
		case 0x9: // ping → pong
			_ = c.write(0xA, p)
			continue
		case 0xA:
			continue
		}
		if !fin {
			// accumulate continuation frames
			acc := append([]byte(nil), p...)
			for {
				f2, op2, p2, e2 := c.readFrame()
				if e2 != nil {
					return 0, nil, e2
				}
				if op2 == 0x9 {
					_ = c.write(0xA, p2)
					continue
				}
				acc = append(acc, p2...)
				if f2 {
					return op, acc, nil
				}
			}
		}
		return op, p, nil
	}
}

// ReadJSON reads the next message and unmarshals it.
func (c *Client) ReadJSON(v any) error {
	_, p, err := c.Read()
	if err != nil {
		return err
	}
	return json.Unmarshal(p, v)
}

func (c *Client) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(c.br, h[:]); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	opcode = h[0] & 0x0f
	n := int64(h[1] & 0x7f)
	switch n {
	case 126:
		var e [2]byte
		if _, err = io.ReadFull(c.br, e[:]); err != nil {
			return
		}
		n = int64(binary.BigEndian.Uint16(e[:]))
	case 127:
		var e [8]byte
		if _, err = io.ReadFull(c.br, e[:]); err != nil {
			return
		}
		n = int64(binary.BigEndian.Uint64(e[:]))
	}
	buf := make([]byte, n)
	_, err = io.ReadFull(c.br, buf)
	return fin, opcode, buf, err
}

// Close drops the TCP connection.
func (c *Client) Close() error { return c.conn.Close() }

// WSURL turns an httptest server URL into a ws:// URL with the given path+query.
func WSURL(base, pathAndQuery string) string {
	return strings.Replace(base, "http://", "ws://", 1) + pathAndQuery
}
