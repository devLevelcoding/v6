package ws

import (
	"bufio"
	"encoding/binary"
	"errors"
	"net"
	"time"
)

// Opcode is a WebSocket frame opcode (RFC 6455 §5.2).
type Opcode byte

const (
	opContinuation Opcode = 0x0
	OpText         Opcode = 0x1
	OpBinary       Opcode = 0x2
	opClose        Opcode = 0x8
	opPing         Opcode = 0x9
	opPong         Opcode = 0xA
)

// Close status codes used by GoStream.
const (
	CloseNormal        = 1000
	CloseGoingAway     = 1001
	CloseProtocolError = 1002
	CloseMessageTooBig = 1009
	CloseInternalError = 1011
)

// ErrClosed is returned by ReadMessage once the peer has sent a close frame (or
// the connection dropped). The Conn is spent.
var ErrClosed = errors.New("ws: connection closed")

// Conn is one WebSocket connection. It is NOT safe for concurrent writers —
// GoStream serializes writes through a single goroutine per client. Concurrent
// read + write is fine.
type Conn struct {
	nc      net.Conn
	br      *bufio.Reader
	readLim int64
	writeTO time.Duration
	idleTO  time.Duration
	closed  bool

	// readBuf is reused across frames (CoverGo U7). Safe because a Conn has a
	// single reader goroutine and ReadMessage copies each frame's payload into
	// the message before the next readFrame call.
	readBuf []byte

	// rhdr/whdr are per-connection frame-header scratch (CoverGo U4): passing a
	// local array's slice to io.ReadFull / conn.Write made the compiler heap-
	// allocate it on every frame. As Conn fields they don't escape per-frame.
	rhdr [8]byte
	whdr [10]byte
}

func newConn(nc net.Conn, br *bufio.Reader) *Conn {
	return &Conn{nc: nc, br: br, readLim: 1 << 20, writeTO: 10 * time.Second}
}

// SetReadLimit caps the size of a reassembled message; a larger one closes the
// connection with 1009. Default 1 MiB.
func (c *Conn) SetReadLimit(n int64) { c.readLim = n }

// SetIdleTimeout makes ReadMessage refresh the read deadline before each frame,
// so a peer that goes silent for longer than d is dropped. Zero disables it.
func (c *Conn) SetIdleTimeout(d time.Duration) { c.idleTO = d }

// RemoteAddr is the peer's address.
func (c *Conn) RemoteAddr() net.Addr { return c.nc.RemoteAddr() }

// ReadMessage returns the next complete text or binary message. Control frames
// (ping/close) are handled transparently: a ping is answered with a pong; a
// close makes this and every later call return ErrClosed.
func (c *Conn) ReadMessage() (Opcode, []byte, error) {
	if c.closed {
		return 0, nil, ErrClosed
	}
	var (
		msg     []byte
		msgOp   Opcode
		haveMsg bool
	)
	for {
		if c.idleTO > 0 {
			_ = c.nc.SetReadDeadline(time.Now().Add(c.idleTO))
		}
		fin, op, masked, payload, err := c.readFrame()
		if err != nil {
			// Any framing/IO error ends the connection. Best-effort close frame.
			_ = c.Close(CloseProtocolError, "read error")
			return 0, nil, ErrClosed
		}
		if !masked {
			return c.fail(CloseProtocolError, "client frame not masked")
		}
		if op >= opClose && (len(payload) > 125 || !fin) {
			return c.fail(CloseProtocolError, "fragmented or oversized control frame")
		}

		switch op {
		case opClose:
			c.replyClose(payload)
			c.closed = true
			return 0, nil, ErrClosed
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				c.closed = true
				return 0, nil, err
			}
			continue
		case opPong:
			continue
		case OpText, OpBinary:
			if haveMsg {
				return c.fail(CloseProtocolError, "new data frame before FIN")
			}
			msgOp, haveMsg = op, true
			msg = append(msg, payload...)
		case opContinuation:
			if !haveMsg {
				return c.fail(CloseProtocolError, "continuation without a start frame")
			}
			msg = append(msg, payload...)
		default:
			return c.fail(CloseProtocolError, "unknown opcode")
		}

		if int64(len(msg)) > c.readLim {
			return c.fail(CloseMessageTooBig, "message exceeds read limit")
		}
		if fin && haveMsg {
			return msgOp, msg, nil
		}
	}
}

// WriteMessage sends one unfragmented data frame.
func (c *Conn) WriteMessage(op Opcode, data []byte) error {
	if c.closed {
		return ErrClosed
	}
	return c.writeFrame(op, data)
}

// Ping sends a ping frame. The peer's pong is consumed transparently by
// ReadMessage.
func (c *Conn) Ping() error {
	if c.closed {
		return ErrClosed
	}
	return c.writeFrame(opPing, nil)
}

// Close sends a close frame with the given code/reason and drops the TCP
// connection. Safe to call more than once.
func (c *Conn) Close(code uint16, reason string) error {
	if c.closed {
		return nil
	}
	c.closed = true
	body := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(body, code)
	copy(body[2:], reason)
	_ = c.writeFrame(opClose, body)
	return c.nc.Close()
}

func (c *Conn) fail(code uint16, reason string) (Opcode, []byte, error) {
	_ = c.Close(code, reason)
	return 0, nil, ErrClosed
}

func (c *Conn) replyClose(payload []byte) {
	code := uint16(CloseNormal)
	if len(payload) >= 2 {
		code = binary.BigEndian.Uint16(payload)
	}
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, code)
	_ = c.writeFrame(opClose, body)
	_ = c.nc.Close()
}
