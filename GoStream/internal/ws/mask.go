//go:build !purego

package ws

import "unsafe"

const wordSize = int(unsafe.Sizeof(uintptr(0)))

// maskBytes XORs b in place with the 4-byte key, starting at key offset pos, and
// returns the new offset (CoverGo U9). It processes a machine word at a time via
// unsafe once past small buffers — ~4–8× the byte loop for realistic frame
// sizes. The `purego` build tag selects the plain-Go fallback in mask_purego.go.
func maskBytes(key [4]byte, pos int, b []byte) int {
	if len(b) < 2*wordSize {
		for i := range b {
			b[i] ^= key[pos&3]
			pos++
		}
		return pos & 3
	}

	// Align to a word boundary one byte at a time.
	if n := int(uintptr(unsafe.Pointer(&b[0])) % uintptr(wordSize)); n != 0 {
		n = wordSize - n
		for i := 0; i < n; i++ {
			b[i] ^= key[pos&3]
			pos++
		}
		b = b[n:]
	}

	// Word-aligned key repeated to fill a uintptr.
	var k [wordSize]byte
	for i := range k {
		k[i] = key[(pos+i)&3]
	}
	kw := *(*uintptr)(unsafe.Pointer(&k))

	n := (len(b) / wordSize) * wordSize
	base := unsafe.Pointer(&b[0])
	for i := 0; i < n; i += wordSize {
		p := (*uintptr)(unsafe.Add(base, i))
		*p ^= kw
	}

	b = b[n:]
	for i := range b {
		b[i] ^= key[pos&3]
		pos++
	}
	return pos & 3
}
