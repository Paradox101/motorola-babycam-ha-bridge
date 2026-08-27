package magic

import "testing"

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
		"app 48",                        // two-field form: acknowledged natively, not reconstructed
		"app 48 203.0.113.10 host.only", // four-field form
		"nope 48 a b 1 c 2 3",           // wrong keyword
		"app x 203.0.113.10 h 1 c 2 3",  // non-numeric num
	} {
		if _, err := ParseAppResponse([]byte(line)); err == nil {
			t.Fatalf("expected rejection for %q", line)
		}
	}
}
