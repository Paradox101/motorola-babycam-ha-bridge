// Command streamdemo runs the whole Home Assistant add-on media pipeline
// locally and prints, live, that real RTP media flows through it end to end.
//
// It wires a minimal real RTSP camera, a relay simulator (which speaks the real
// Magic control/relay-open/token-crypto wire codecs), the actual bridge that
// cmd/magicbridge ships, and a real RTSP client. Everything the bridge and the
// tunnel do is exercised for real; only the 5GenCare-side authorization that
// attaches a real camera to the relay is simulated. It needs no network, no
// credentials and no camera — it demonstrates the transport, not a live camera.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/bridge"
	"github.com/local/motorola-vm65-bridge/internal/relaysim"
	"github.com/local/motorola-vm65-bridge/internal/rtspmini"
)

const demoToken = "DEMOTOKEN012345678901234567"

func main() {
	frames := flag.Int("frames", 25, "number of RTP media frames to stream")
	flag.Parse()

	if err := run(*frames); err != nil {
		log.Fatalf("streamdemo: %v", err)
	}
}

func run(frames int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Local RTSP camera stand-in.
	cameraLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer cameraLn.Close()
	go func() {
		conn, err := cameraLn.Accept()
		if err != nil {
			return
		}
		_ = rtspmini.ServeCamera(conn, frames)
	}()
	fmt.Printf("[camera]  RTSP camera stand-in on %s (serves %d H.264 RTP frames after PLAY)\n", cameraLn.Addr(), frames)

	// 2. Relay simulator: real Magic wire codecs, bridges the tunnel to the camera.
	relayLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	relay := relaysim.New(relayLn, demoToken, func() (net.Conn, error) {
		return net.Dial("tcp", cameraLn.Addr().String())
	})
	go func() { _ = relay.Serve(ctx) }()
	fmt.Printf("[relay]   Magic WEB2 relay simulator on %s (app-discovery + relay-open + token crypto)\n", relay.Addr())

	// 3. The real bridge (identical to what cmd/magicbridge runs).
	srv, err := bridge.New(bridge.Config{
		Credentials: bridge.Credentials{
			DeviceID:    0x00123456,
			SID:         "SID01234567890123456",
			DeviceToken: demoToken,
			ControlHost: "relay.local",
			TargetPort:  6667,
		},
		ListenAddr: "127.0.0.1:0",
		Dial:       relay.Dial,
		Logf:       func(format string, args ...any) { fmt.Printf("[bridge]  "+format+"\n", args...) },
	})
	if err != nil {
		return err
	}
	go func() { _ = srv.Serve(ctx) }()

	addr := waitForAddr(srv)
	fmt.Printf("[bridge]  local camera endpoint ready on %s\n", addr)

	// 4. Real RTSP client pulls the stream through the bridge.
	fmt.Println("[client]  RTSP OPTIONS/DESCRIBE/SETUP/PLAY ...")
	got, err := rtspmini.PullFrames(addr.String(), frames, 20*time.Second)
	if err != nil {
		return fmt.Errorf("pull frames: %w", err)
	}

	// 5. Verify every frame arrived intact.
	for i, payload := range got {
		want := rtspmini.FramePayload(i)
		if string(payload) != string(want) {
			return fmt.Errorf("frame %d corrupted through the tunnel", i)
		}
	}
	fmt.Printf("[client]  received %d RTP frames; first payload tail: %q\n", len(got), tail(got[0]))
	fmt.Printf("\nSUCCESS: %d H.264 RTP media frames streamed intact through the bridge + Magic token tunnel.\n", len(got))
	fmt.Println("Note: only the 5GenCare-side camera authorization was simulated; the transport is the real reconstructed one.")
	return nil
}

func tail(p []byte) string {
	if len(p) <= 18 {
		return string(p)
	}
	return string(p[12:])
}

func waitForAddr(srv *bridge.Server) net.Addr {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if addr := srv.Addr(); addr != nil {
			return addr
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}
