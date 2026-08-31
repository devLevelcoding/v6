// Package uid generates random opaque identifiers. It exists so the rest of the
// tree does not pull in a UUID dependency for what is a one-liner over
// crypto/rand.
package uid

import (
	"crypto/rand"
	"encoding/hex"
)

// New returns a random 128-bit identifier as 32 lowercase hex characters.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("uid: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
