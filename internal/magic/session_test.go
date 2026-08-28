package magic

import (
	"regexp"
	"testing"
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewSessionNameShapeAndUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s := NewSessionName()
		if len(s) != 36 {
			t.Fatalf("session name %q has length %d, want 36", s, len(s))
		}
		if !uuidV4.MatchString(s) {
			t.Fatalf("session name %q is not a canonical UUIDv4", s)
		}
		if seen[s] {
			t.Fatalf("session name %q repeated", s)
		}
		seen[s] = true
	}
}
