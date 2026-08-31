package ws

import (
	"bufio"
	"bytes"
	"testing"
)

// BenchmarkReadFrame (CoverGo U1/U4) — the inbound frame path. After U4 the
// frame headers no longer heap-allocate per frame; the only alloc is the
// reusable payload buffer growing once.
func BenchmarkReadFrame(b *testing.B) {
	// one masked 64-byte text frame, repeated
	payload := bytes.Repeat([]byte("x"), 64)
	frame := append([]byte{0x81, 0x80 | 64, 0, 0, 0, 0}, payload...)
	stream := bytes.Repeat(frame, 4096)

	b.ReportAllocs()
	for b.Loop() {
		c := newConn(nopConn{}, bufio.NewReader(bytes.NewReader(stream)))
		c.SetReadLimit(1 << 16)
		for {
			if _, _, _, _, err := c.readFrame(); err != nil {
				break
			}
		}
	}
}
