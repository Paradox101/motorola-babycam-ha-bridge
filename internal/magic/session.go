package magic

import (
	"crypto/rand"
	"fmt"
)

// NewSessionName returns a fresh canonical 36-character UUIDv4 string used as
// the per-connection session label in the app-discovery request and relay-open
// frame. The measured app used exactly one such label per relay session, so
// each new tunnel must mint its own.
func NewSessionName() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
