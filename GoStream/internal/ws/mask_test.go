package ws

import (
	"bytes"
	"math/rand"
	"testing"
)

// refMask is the obviously-correct reference the fast path must match.
func refMask(key [4]byte, b []byte) {
	for i := range b {
		b[i] ^= key[i&3]
	}
}

func TestMaskBytesMatchesReference(t *testing.T) {
	key := [4]byte{0xDE, 0xAD, 0xBE, 0xEF}
	for _, n := range []int{0, 1, 3, 4, 7, 8, 9, 15, 16, 17, 31, 64, 65, 1000, 4096, 4097} {
		src := make([]byte, n)
		rand.Read(src)

		a := append([]byte(nil), src...)
		b := append([]byte(nil), src...)
		maskBytes(key, 0, a)
		refMask(key, b)
		if !bytes.Equal(a, b) {
			t.Fatalf("n=%d: fast path disagrees with reference", n)
		}

		// masking twice restores the original
		maskBytes(key, 0, a)
		if !bytes.Equal(a, src) {
			t.Fatalf("n=%d: double-mask did not round-trip", n)
		}
	}
}

// TestMaskBytesUnalignedStart exercises the word-alignment prologue.
func TestMaskBytesUnalignedStart(t *testing.T) {
	key := [4]byte{1, 2, 3, 4}
	backing := make([]byte, 4096+8)
	rand.Read(backing)
	for off := 0; off < 8; off++ {
		seg := backing[off : off+4000]
		a := append([]byte(nil), seg...)
		b := append([]byte(nil), seg...)
		maskBytes(key, 0, a)
		refMask(key, b)
		if !bytes.Equal(a, b) {
			t.Fatalf("offset %d: mismatch", off)
		}
	}
}

func BenchmarkMaskBytes(b *testing.B) {
	key := [4]byte{0xDE, 0xAD, 0xBE, 0xEF}
	buf := make([]byte, 4096)
	b.SetBytes(int64(len(buf)))
	for b.Loop() {
		maskBytes(key, 0, buf)
	}
}
