package magic

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateFixtures regenerates the golden wire fixtures under testdata/ when set:
//
//	go test ./internal/magic -run TestWireFixtures -update
//
// The fixtures are anonymized: every value below is a synthetic placeholder in
// the documented format, carrying no real device identity or secret.
var updateFixtures = flag.Bool("update", false, "regenerate golden wire fixtures")

const (
	fxDeviceID = 0x00123456
	fxSID      = "SID0123456789"
	fxToken    = "TOK012345678901234567890123"
	// A canonical 36-char UUID, matching the app's session label shape.
	fxSessionName = "0123abcd-4567-89ab-cdef-0123456789ab"
	// fxAppResponse is a canonical eight-field WEB2 response with placeholder
	// hosts; see docs/magic-web2-protocol.md for the field semantics.
	fxAppResponse = "app 34 relay-stream.example relay-control.example 6667 192.0.2.20 55 2\n"
)

func fixturePath(name string) string { return filepath.Join("testdata", name) }

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath(name))
	if err != nil {
		t.Fatalf("read fixture %s: %v (run with -update to generate)", name, err)
	}
	return data
}

func writeFixture(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath(name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// canonicalMagicUUID is the derived 78-byte identifier for the fixture inputs.
func canonicalMagicUUID(t *testing.T) string {
	t.Helper()
	uuid, err := GenerateMagicUUID(fxDeviceID, fxSID, fxToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(uuid) != 78 {
		t.Fatalf("magic UUID length = %d, want 78", len(uuid))
	}
	return uuid
}

// TestWireFixtures locks the on-wire byte encoding of the reconstructed Magic
// WEB2 frames against golden files, and proves each codec round-trips them. A
// change in any frame's bytes now fails here until the fixture is deliberately
// regenerated with -update.
func TestWireFixtures(t *testing.T) {
	magicUUID := canonicalMagicUUID(t)

	appReq := AppRequest{MagicUUID: magicUUID, TargetPort: 6667, Mode: ConnectionModeWEB2, SessionName: fxSessionName}
	appReqBytes, err := appReq.MarshalText()
	if err != nil {
		t.Fatalf("marshal app request: %v", err)
	}

	relayOpen := RelayOpen{Version: RelayOpenVersion2, ConnectionNumber: 34, TargetPort: 6667, MagicUUID: magicUUID, SessionName: fxSessionName}
	relayOpenBytes, err := relayOpen.MarshalText()
	if err != nil {
		t.Fatalf("marshal relay-open: %v", err)
	}

	if *updateFixtures {
		writeFixture(t, "app_request.txt", appReqBytes)
		writeFixture(t, "app_response.txt", []byte(fxAppResponse))
		writeFixture(t, "relay_open.bin", relayOpenBytes)
		t.Log("wire fixtures regenerated")
		return
	}

	// 1. Marshalling must reproduce the golden bytes exactly.
	if got, want := appReqBytes, readFixture(t, "app_request.txt"); !bytes.Equal(got, want) {
		t.Errorf("app request bytes drifted:\n got %q\nwant %q", got, want)
	}
	if got, want := relayOpenBytes, readFixture(t, "relay_open.bin"); !bytes.Equal(got, want) {
		t.Errorf("relay-open bytes drifted:\n got %q\nwant %q", got, want)
	}

	// 2. Parsing the golden bytes must recover the exact fields.
	resp, err := ParseAppResponse(readFixture(t, "app_response.txt"))
	if err != nil {
		t.Fatalf("parse app response fixture: %v", err)
	}
	wantResp := AppResponse{
		ConnectionNumber: 34,
		StreamHost:       "relay-stream.example",
		ControlHost:      "relay-control.example",
		TargetPort:       6667,
		DirectIP:         "192.0.2.20",
		DirectPort:       55,
		Mode:             ConnectionModeWEB2,
	}
	if resp != wantResp {
		t.Errorf("app response fields drifted:\n got %+v\nwant %+v", resp, wantResp)
	}

	parsedOpen, err := ParseRelayOpen(readFixture(t, "relay_open.bin"))
	if err != nil {
		t.Fatalf("parse relay-open fixture: %v", err)
	}
	if parsedOpen != relayOpen {
		t.Errorf("relay-open round-trip drifted:\n got %+v\nwant %+v", parsedOpen, relayOpen)
	}
}
