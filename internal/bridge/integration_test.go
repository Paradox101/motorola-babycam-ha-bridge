package bridge

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/rtspmock"
)

// TestBridgeFullRTSPSession drives a complete RTSP dialogue — OPTIONS, DESCRIBE,
// SETUP, PLAY with interleaved RTP, TEARDOWN — from a client through the bridge,
// the Magic WEB2 tunnel and an in-process relay to a mock camera. It asserts the
// SDP and every binary RTP payload arrive byte-exact, proving the tunnel is
// transparent for a real session shape, not just a single OPTIONS line. No
// Android and no live credentials are involved.
func TestBridgeFullRTSPSession(t *testing.T) {
	// Mock camera with high-entropy RTP so text-only bugs cannot pass.
	want := rtspmock.SyntheticRTP(12)
	camListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer camListener.Close()
	cam := &rtspmock.Camera{RTPPackets: want}
	go cam.Serve(camListener)

	// Relay + camera behind the tunnel; bridge in front.
	relay := newRelayCamera(t, testToken, camListener.Addr().String())
	b, err := New(Config{ListenAddr: "127.0.0.1:0", Credentials: testCreds(), Dial: relay.dial})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = b.Serve(ctx) }()

	client, err := net.Dial("tcp", b.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(10 * time.Second))

	session, err := rtspmock.Dialogue(client, "rtsp://127.0.0.1/owner/streaming", len(want))
	if err != nil {
		t.Fatalf("dialogue through tunnel: %v", err)
	}

	if session.SDP != rtspmock.DefaultSDP {
		t.Fatalf("SDP corrupted through tunnel:\n got %q\nwant %q", session.SDP, rtspmock.DefaultSDP)
	}
	if len(session.RTP) != len(want) {
		t.Fatalf("got %d RTP packets, want %d", len(session.RTP), len(want))
	}
	for i := range want {
		if !bytes.Equal(session.RTP[i], want[i]) {
			t.Fatalf("RTP packet %d corrupted through tunnel (len got %d want %d)",
				i, len(session.RTP[i]), len(want[i]))
		}
	}
}
