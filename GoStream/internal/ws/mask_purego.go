//go:build purego

package ws

// maskBytes is the allocation-free, unsafe-free fallback (CoverGo U9): build
// with `-tags purego` to select it. Same contract as the unsafe version.
func maskBytes(key [4]byte, pos int, b []byte) int {
	for i := range b {
		b[i] ^= key[pos&3]
		pos++
	}
	return pos & 3
}
