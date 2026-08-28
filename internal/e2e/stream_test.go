// Package e2e proves the Home Assistant add-on's media path works end to end:
// a real RTSP/RTP dialog is carried, byte for byte, through the actual bridge
// and the device-token Magic tunnel, with a relay simulator standing in for the
// relay plus an already-authorized camera peer.
//
// What is real here: the RTSP OPTIONS/DESCRIBE/SETUP/PLAY handshake, the
// interleaved RTP media frames, the bridge (the exact code cmd/magicbridge
// runs), the relay-open framing and the stateful token crypto. The only thing
// simulated is the 5GenCare-side authorization that attaches the camera to the
// session — the documented external blocker.
package e2e

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/bridge"
	"github.com/local/motorola-vm65-bridge/internal/relaysim"
	"github.com/local/motorola-vm65-bridge/internal/rtspmini"
)

const (
	deviceToken = "TOK012345678901234567890123"
	frameCount  = 25
)

// TestRTSPMediaFlowsThroughBridge streams real RTP frames from a local RTSP
// camera, through the relay simulator, through the bridge, to a real RTSP
// client, and verifies every frame arrives intact.
func TestRTSPMediaFlowsThroughBridge(t *testing.T) {
	// 1. The camera: a minimal but real RTSP-over-TCP server.
	cameraLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer cameraLn.Close()
	go func() {
		conn, err := cameraLn.Accept()
		if err != nil {
			return
		}
		_ = rtspmini.ServeCamera(conn, frameCount)
	}()

	// 2. The relay simulator, whose backend dials the camera per session.
	relayLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relay := relaysim.New(relayLn, deviceToken, func() (net.Conn, error) {
		return net.Dial("tcp", cameraLn.Addr().String())
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	relayErr := make(chan error, 1)
	go func() { relayErr <- relay.Serve(ctx) }()

	// 3. The real bridge (the code cmd/magicbridge runs), dialing the relay.
	srv, err := bridge.New(bridge.Config{
		Credentials: bridge.Credentials{
			DeviceID:    0x00123456,
			SID:         "SID01234567890123456",
			DeviceToken: deviceToken,
			ControlHost: "relay.local",
			TargetPort:  6667,
		},
		ListenAddr: "127.0.0.1:0",
		Dial:       relay.Dial,
	})
	if err != nil {
		t.Fatal(err)
	}
	bridgeErr := make(chan error, 1)
	go func() { bridgeErr <- srv.Serve(ctx) }()

	bridgeAddr := waitForAddr(t, srv)

	// 4. A real RTSP client pulls the stream through the bridge.
	frames, err := rtspmini.PullFrames(bridgeAddr.String(), frameCount, 15*time.Second)
	if err != nil {
		t.Fatalf("pull frames: %v", err)
	}

	if len(frames) != frameCount {
		t.Fatalf("received %d frames, want %d", len(frames), frameCount)
	}
	for i, payload := range frames {
		if want := rtspmini.FramePayload(i); !bytes.Equal(payload, want) {
			t.Fatalf("frame %d mismatch:\n got %q\nwant %q", i, payload, want)
		}
	}
	t.Logf("verified %d RTP media frames streamed intact through the Magic tunnel + bridge", len(frames))

	cancel()
	if err := <-bridgeErr; err != nil {
		t.Errorf("bridge serve: %v", err)
	}
	if err := <-relayErr; err != nil {
		t.Errorf("relay serve: %v", err)
	}
}

func waitForAddr(t *testing.T, srv *bridge.Server) net.Addr {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if addr := srv.Addr(); addr != nil {
			return addr
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("bridge did not bind in time")
	return nil
}
