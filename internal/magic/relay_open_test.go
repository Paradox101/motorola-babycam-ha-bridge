package magic

import (
	"strings"
	"testing"
)

func TestRelayOpenCapturedShape(t *testing.T) {
	// Synthetic identifiers preserve captured lengths but contain no camera,
	// account, session, SID, or token material.
	want := RelayOpen{
		Version:          RelayOpenVersion2,
		ConnectionNumber: 34,
		TargetPort:       6667,
		MagicUUID:        strings.Repeat("M", 78),
		SessionName:      strings.Repeat("S", 36),
	}
	encoded, err := want.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 139 {
		t.Fatalf("captured frame shape is 139 bytes, got %d", len(encoded))
	}
	if !strings.HasPrefix(string(encoded), "v002 034 06667 078 ") {
		t.Fatalf("unexpected prefix: %q", encoded[:19])
	}
	got, err := ParseRelayOpen(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip mismatch: %#v != %#v", got, want)
	}
}

func TestRelayOpenRejectsAmbiguousOrMalformedFrames(t *testing.T) {
	tests := []RelayOpen{
		{Version: 1000, ConnectionNumber: 1, TargetPort: 6667, MagicUUID: "m", SessionName: "s"},
		{Version: 2, ConnectionNumber: 1, TargetPort: 100000, MagicUUID: "m", SessionName: "s"},
		{Version: 2, ConnectionNumber: 1, TargetPort: 6667, MagicUUID: "with space", SessionName: "s"},
	}
	for _, test := range tests {
		if _, err := test.MarshalText(); err == nil {
			t.Fatalf("expected validation failure for %#v", test)
		}
	}
	if _, err := ParseRelayOpen([]byte("v002 034 06667 078 too-short")); err == nil {
		t.Fatal("expected truncated frame to fail")
	}
}
