package bridge

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBridgeAbandonsDialWhenClientLeaves covers what an unreachable relay did
// to the concurrency cap in 0.11.0: a media server gives up after a few
// seconds and reconnects, while the bridge kept dialling for the peer that had
// left — for the whole retry sequence, holding its session slot the entire
// time. Enough of those and the cap refuses clients that would have worked.
func TestBridgeAbandonsDialWhenClientLeaves(t *testing.T) {
	relayAddr := startSilentControlHost(t)
	rec := &recordingHandler{}

	b, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Credentials: testCreds(),
		Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("tcp", relayAddr)
		},
		// Far longer than this test may take: the point is that the session
		// ends when the client leaves, not when the dial gives up.
		DialTimeout: 30 * time.Second,
		DialBackoff: 30 * time.Second,
		Logger:      slog.New(rec),
	})
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
	// An RTSP client writes its first request and then waits; this one gives up
	// while the relay is still being dialled.
	if _, err := client.Write([]byte("OPTIONS rtsp://127.0.0.1/owner/streaming RTSP/1.0\r\nCSeq: 1\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		_, active := b.Stats()
		return active == 1
	})
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		_, active := b.Stats()
		return active == 0
	})
	waitFor(t, func() bool {
		return rec.has(slog.LevelInfo, "client left before the relay was ready")
	})
}

// TestBridgeBoundsTheDialBudget pins the second half of that fix: a client that
// stays connected must not hold its slot for the whole retry sequence either.
func TestBridgeBoundsTheDialBudget(t *testing.T) {
	relayAddr := startSilentControlHost(t)
	rec := &recordingHandler{}

	b, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Credentials: testCreds(),
		Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("tcp", relayAddr)
		},
		DialTimeout: 30 * time.Second,
		DialBackoff: 30 * time.Second,
		DialBudget:  200 * time.Millisecond,
		Logger:      slog.New(rec),
	})
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

	// The client stays put, so only the budget can end this.
	waitFor(t, func() bool {
		_, active := b.Stats()
		return active == 0
	})
	waitFor(t, func() bool {
		return rec.has(slog.LevelError, "relay dial failed")
	})
	if rec.has(slog.LevelInfo, "client left before the relay was ready") {
		t.Fatal("a client that stayed connected was reported as having left")
	}
}

// TestBridgeReplaysTheRequestSentWhileDialling proves the watch does not eat
// the client's opening request: whatever it read before the tunnel existed has
// to reach the camera first, and in order.
func TestBridgeReplaysTheRequestSentWhileDialling(t *testing.T) {
	backend := startRTSPBackend(t)
	relay := newRelayCamera(t, testToken, backend)

	// A dial slow enough that the client's request is certainly read by the
	// watch rather than by the session's own copy loop.
	slowDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		time.Sleep(150 * time.Millisecond)
		return relay.dial(ctx, network, addr)
	}

	b, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Credentials: testCreds(),
		Dial:        slowDial,
	})
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
	client.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := client.Write([]byte("OPTIONS rtsp://127.0.0.1/owner/streaming RTSP/1.0\r\nCSeq: 1\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if response := readAvailable(t, client); !strings.Contains(response, "RTSP/1.0 200 OK") {
		t.Fatalf("the request sent while dialling did not reach the camera:\n%q", response)
	}
}

// startSilentControlHost accepts connections and answers nothing, which is how
// an unreachable relay behaves once the TCP connect still succeeds: the dial
// hangs until its timeout instead of failing outright.
func startSilentControlHost(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var accepted []net.Conn
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range accepted {
			_ = conn.Close()
		}
	})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Hold it open and say nothing.
			mu.Lock()
			accepted = append(accepted, conn)
			mu.Unlock()
		}
	}()
	return listener.Addr().String()
}
