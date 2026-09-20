package magic

import (
	"errors"
	"strings"
	"testing"
)

func TestAppRequestMarshalMatchesNativeFormat(t *testing.T) {
	request := AppRequest{
		MagicUUID:   "0012345600aabbccddeeff00112233445566778899aabbccddeeff0011223344556677889900",
		TargetPort:  6667,
		Mode:        ConnectionModeWEB2,
		SessionName: "SESSION0123456789abcdef0123456789ab",
	}
	got, err := request.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	want := "app 0012345600aabbccddeeff00112233445566778899aabbccddeeff0011223344556677889900 6667 2 SESSION0123456789abcdef0123456789ab\n"
	if string(got) != want {
		t.Fatalf("request mismatch:\n got %q\nwant %q", got, want)
	}
}

// Structure and field order match the runtime capture; endpoints are RFC 5737
// documentation addresses rather than the measured session's real values.
func TestParseAppResponseEightFieldWEB2(t *testing.T) {
	line := []byte("app 48 203.0.113.10 vrelay-example.5gen.care 6667 192.0.2.20 77 2\n")
	got, err := ParseAppResponse(line)
	if err != nil {
		t.Fatal(err)
	}
	want := AppResponse{
		Fields:           8,
		ConnectionNumber: 48,
		StreamHost:       "203.0.113.10",
		ControlHost:      "vrelay-example.5gen.care",
		TargetPort:       6667,
		DirectIP:         "192.0.2.20",
		DirectPort:       77,
		Mode:             ConnectionModeWEB2,
	}
	if got != want {
		t.Fatalf("response mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// The four-field form is what the relay sent, five times in one morning
// (2026-09-20), each time with connection number 1 — the camera had just
// registered again. It names the relay hosts and nothing else; a session opened
// from it uses the target port the request asked for.
func TestParseAppResponseFourFieldRelayOnly(t *testing.T) {
	got, err := ParseAppResponse([]byte("app 1 165.232.73.94 vrelay-de0.5gen.care\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := AppResponse{
		Fields:           4,
		ConnectionNumber: 1,
		StreamHost:       "165.232.73.94",
		ControlHost:      "vrelay-de0.5gen.care",
	}
	if got != want {
		t.Fatalf("response mismatch:\n got %+v\nwant %+v", got, want)
	}
	if _, err := ParseAppResponse([]byte("app 1 165.232.73.94 bad\x7fhost\n")); err == nil {
		t.Fatal("a four-field response with an unprintable control host was accepted")
	}
}

// The response ConnectionNumber is the same value the relay-open frame carries;
// this is the byte-level correlation observed between the 8800 and 9901 flows.
func TestAppResponseNumberFeedsRelayOpen(t *testing.T) {
	response, err := ParseAppResponse([]byte("app 48 203.0.113.10 vrelay-example.5gen.care 6667 192.0.2.20 77 2"))
	if err != nil {
		t.Fatal(err)
	}
	frame := RelayOpen{
		Version:          RelayOpenVersion2,
		ConnectionNumber: response.ConnectionNumber,
		TargetPort:       response.TargetPort,
		MagicUUID:        "0012345600aabbccddeeff00112233445566778899aabbccddeeff0011223344556677889900",
		SessionName:      "SESSION0123456789abcdef0123456789ab",
	}
	encoded, err := frame.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	// v%03d %03d %05d ... => "v002 048 06667 ..."
	if want := "v002 048 06667 "; string(encoded[:len(want)]) != want {
		t.Fatalf("relay-open prefix mismatch: %q", encoded[:len(want)])
	}
}

func TestParseAppResponseRejectsUnprovenForms(t *testing.T) {
	for _, line := range []string{
		"app 0 203.0.113.10 vrelay-example.5gen.care 6667 192.0.2.20 77 2", // num must be > 0
		"app 48",              // two-field form: acknowledged natively, not reconstructed
		"app 48 203.0.113.10", // three-field form
		"app 48 203.0.113.10 host 6667 192.0.2.20", // neither four nor eight fields
		"nope 48 a b 1 c 2 3",                      // wrong keyword
		"app x 203.0.113.10 h 1 c 2 3",             // non-numeric num
	} {
		if _, err := ParseAppResponse([]byte(line)); err == nil {
			t.Fatalf("expected rejection for %q", line)
		}
	}
}

// A non-positive num is the relay saying no. It has to come back as its own
// type — the bridge does not retry it — and carry the line the relay sent, so
// a reason field in a short-form refusal reaches the log instead of being
// reduced to "connection number must be positive".
func TestParseAppResponseReportsARefusal(t *testing.T) {
	for _, line := range []string{
		"app 0\n",
		"app -1 no_device\r\n",
		"app 0 203.0.113.10 vrelay-example.5gen.care 6667 192.0.2.20 77 2\n",
	} {
		_, err := ParseAppResponse([]byte(line))
		var refused *RelayRefusedError
		if !errors.As(err, &refused) {
			t.Fatalf("%q: want RelayRefusedError, got %v", line, err)
		}
		want := strings.TrimRight(line, "\r\n")
		if refused.Response != want {
			t.Fatalf("%q: response %q, want %q", line, refused.Response, want)
		}
		if refused.ConnectionNumber > 0 {
			t.Fatalf("%q: connection number %d reported as a refusal", line, refused.ConnectionNumber)
		}
		if !strings.Contains(err.Error(), "not connected to the relay") || !strings.Contains(err.Error(), want) {
			t.Fatalf("%q: error does not explain the refusal: %v", line, err)
		}
	}
}

// Every other rejection names the line it rejected: the first four-field
// response the relay sent in the field was unexplainable afterwards precisely
// because the error only said how many fields it had.
func TestParseAppResponseErrorsQuoteTheResponse(t *testing.T) {
	for _, line := range []string{
		"app 48 203.0.113.10 host 6667 192.0.2.20",
		"app 48",
		"nope 48 a b 1 c 2 3",
		"app x 203.0.113.10 h 1 c 2 3",
	} {
		_, err := ParseAppResponse([]byte(line + "\n"))
		if err == nil {
			t.Fatalf("expected rejection for %q", line)
		}
		var refused *RelayRefusedError
		if errors.As(err, &refused) {
			t.Fatalf("%q: a positive or unparsable num is not a refusal: %v", line, err)
		}
		if !strings.Contains(err.Error(), line) {
			t.Fatalf("%q: error does not quote the response: %v", line, err)
		}
	}
}

// A response the relay never sends must not become an unbounded or unreadable
// log line: it is cut to a fixed length, and %q escapes whatever is left.
func TestParseAppResponseBoundsTheQuotedResponse(t *testing.T) {
	line := "app 0 " + strings.Repeat("x", 1000) + "\x01\xff"
	_, err := ParseAppResponse([]byte(line))
	var refused *RelayRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("want RelayRefusedError, got %v", err)
	}
	if len(refused.Response) > maxDescribedResponse+len("...") {
		t.Fatalf("response not bounded: %d bytes", len(refused.Response))
	}
	if !strings.HasSuffix(refused.Response, "...") {
		t.Fatalf("a cut response should say so: %q", refused.Response)
	}
	long := "app 0 " + strings.Repeat("y", 100) + "\x01\xff"
	_, err = ParseAppResponse([]byte(long))
	if message := err.Error(); strings.Contains(message, "\x01") || strings.Contains(message, "\xff") {
		t.Fatalf("control bytes reached the message: %q", message)
	}
}
