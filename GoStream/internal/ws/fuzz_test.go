package ws

import (
	"bufio"
	"bytes"
	"net"
	"testing"
	"time"
)

// nopConn is a net.Conn whose reads are driven elsewhere (a bufio.Reader over
// fuzz bytes) and whose writes / deadlines are discarded — enough to exercise
// ReadMessage's control-frame replies without real IO.
type nopConn struct{}

func (nopConn) Read([]byte) (int, error)         { return 0, nil }
func (nopConn) Write(p []byte) (int, error)      { return len(p), nil }
func (nopConn) Close() error                     { return nil }
func (nopConn) LocalAddr() net.Addr              { return nopAddr{} }
func (nopConn) RemoteAddr() net.Addr             { return nopAddr{} }
func (nopConn) SetDeadline(time.Time) error      { return nil }
func (nopConn) SetReadDeadline(time.Time) error  { return nil }
func (nopConn) SetWriteDeadline(time.Time) error { return nil }

type nopAddr struct{}

func (nopAddr) Network() string { return "nop" }
func (nopAddr) String() string  { return "nop" }

// FuzzReadMessage (CoverGo U23) feeds arbitrary bytes as the client->server
// frame stream of the hand-rolled RFC 6455 parser. Contract: ReadMessage never
// panics and never returns a message longer than the read limit.
func FuzzReadMessage(f *testing.F) {
	// masked "hi" text frame
	f.Add([]byte{0x81, 0x82, 0x00, 0x00, 0x00, 0x00, 'h', 'i'})
	f.Add([]byte{0x81, 0x00})                   // unmasked -> protocol error
	f.Add([]byte{0x88, 0x80, 0, 0, 0, 0})       // masked close
	f.Add([]byte{0x89, 0x80, 0, 0, 0, 0})       // masked ping -> pong reply
	f.Add([]byte{0xFF, 0xFF})                    // RSV + huge length nibble
	f.Add([]byte{0x81, 0xFE, 0xFF, 0xFF})        // 126 extended length, truncated
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		c := newConn(nopConn{}, bufio.NewReader(bytes.NewReader(data)))
		c.SetReadLimit(1 << 16)
		for i := 0; i < 32; i++ {
			op, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if int64(len(msg)) > 1<<16 {
				t.Fatalf("ReadMessage returned %d bytes, over the read limit (op=%d)", len(msg), op)
			}
		}
	})
}
