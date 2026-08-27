package rtspmock

import (
	"bytes"
	"net"
	"testing"
	"time"
)

// TestCameraClientDirect exercises the mock camera and client against each
// other with no tunnel, so any failure in later bridge tests is attributable to
// the tunnel, not the harness.
func TestCameraClientDirect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	want := SyntheticRTP(6)
	cam := &Camera{RTPPackets: want}
	go cam.Serve(listener)

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	session, err := Dialogue(conn, "rtsp://127.0.0.1/owner/streaming", len(want))
	if err != nil {
		t.Fatal(err)
	}
	if session.SDP != DefaultSDP {
		t.Fatalf("SDP mismatch:\n got %q\nwant %q", session.SDP, DefaultSDP)
	}
	if len(session.RTP) != len(want) {
		t.Fatalf("got %d RTP packets, want %d", len(session.RTP), len(want))
	}
	for i := range want {
		if !bytes.Equal(session.RTP[i], want[i]) {
			t.Fatalf("RTP packet %d mismatch (len got %d want %d)", i, len(session.RTP[i]), len(want[i]))
		}
	}
}
