package bridge

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/magic"
)

// TestBridgeDropsIdleSession covers the fault that made the Web UI report a
// dozen "sessions" for one camera: a relay that stops sending without closing
// its socket. Nothing errors, both copies block forever, and the session stays
// counted, holding two sockets and a goroutine. The idle timeout ends it.
func TestBridgeDropsIdleSession(t *testing.T) {
	relayAddr := startSilentRelay(t, testToken)
	rec := &recordingHandler{}

	b, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Credentials: testCreds(),
		Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("tcp", relayAddr)
		},
		IdleTimeout: 150 * time.Millisecond,
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

	// The relay's one burst arrives, and then nothing: the client must be let
	// go rather than left hanging on a stream that stopped.
	if _, err := io.ReadFull(client, make([]byte, len(silentRelayGreeting))); err != nil {
		t.Fatalf("read camera bytes: %v", err)
	}
	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the idle session to be closed, got %v", err)
	}

	waitFor(t, func() bool {
		_, active := b.Stats()
		return active == 0
	})
	waitFor(t, func() bool {
		return rec.has(slog.LevelWarn, "dropped an idle session")
	})
}

// TestBridgeCapsConcurrentSessions proves a client that reconnects faster than
// its sessions end cannot pile up relay sessions the camera has to serve.
func TestBridgeCapsConcurrentSessions(t *testing.T) {
	backend := startRTSPBackend(t)
	relay := newRelayCamera(t, testToken, backend)
	rec := &recordingHandler{}

	b, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Credentials: testCreds(),
		Dial:        relay.dial,
		MaxSessions: 1,
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

	first, err := net.Dial("tcp", b.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	first.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := first.Write([]byte("OPTIONS rtsp://127.0.0.1/owner/streaming RTSP/1.0\r\nCSeq: 1\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if response := readAvailable(t, first); !strings.Contains(response, "RTSP/1.0 200 OK") {
		t.Fatalf("first session did not reach the camera:\n%q", response)
	}

	second, err := net.Dial("tcp", b.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("expected the session over the cap to be refused, got %v", err)
	}
	waitFor(t, func() bool {
		return rec.has(slog.LevelWarn, "too many concurrent sessions")
	})

	// The refused connection must not be counted, or the cap would lock the
	// camera out for good.
	_, active := b.Stats()
	if active != 1 {
		t.Fatalf("active sessions = %d, want 1", active)
	}
}

// TestNewDefaultsSessionLimits pins the defaults a zero Config selects, and
// that a negative value is the documented way to switch a limit off.
func TestNewDefaultsSessionLimits(t *testing.T) {
	defaults, err := New(Config{ListenAddr: "127.0.0.1:0", Credentials: testCreds()})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.idleTimeout != DefaultIdleTimeout ||
		defaults.keepAlive != DefaultKeepAlivePeriod ||
		defaults.maxSessions != DefaultMaxSessions {
		t.Fatalf("defaults = %v idle / %v keepalive / %d sessions",
			defaults.idleTimeout, defaults.keepAlive, defaults.maxSessions)
	}

	off, err := New(Config{
		ListenAddr:      "127.0.0.1:0",
		Credentials:     testCreds(),
		IdleTimeout:     -1,
		KeepAlivePeriod: -1,
		MaxSessions:     -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if off.idleTimeout > 0 || off.keepAlive > 0 || off.maxSessions > 0 {
		t.Fatalf("negative values did not disable the limits: %v / %v / %d",
			off.idleTimeout, off.keepAlive, off.maxSessions)
	}
	// With the cap off, reserving stays possible past any bound.
	for i := 0; i < DefaultMaxSessions+1; i++ {
		if !off.reserveSession() {
			t.Fatalf("reserveSession refused at %d with the cap disabled", i)
		}
	}
}

// silentRelayGreeting is the single burst startSilentRelay sends before going
// quiet. It stands in for a stream that started and then stopped arriving.
const silentRelayGreeting = "RTSP/1.0 200 OK\r\nCSeq: 1\r\n\r\n"

// startSilentRelay completes the WEB2 handshake, sends one encoded burst of
// camera data, and then neither sends nor closes anything. That is what a relay
// whose peer vanished looks like on the wire: no FIN, no reset, no data.
func startSilentRelay(t *testing.T, token string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, _, _ := net.SplitHostPort(listener.Addr().String())

	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = listener.Close()
	})

	go func() {
		control, err := listener.Accept()
		if err != nil {
			return
		}
		defer control.Close()
		if _, err := bufio.NewReader(control).ReadBytes('\n'); err != nil {
			return
		}
		if _, err := control.Write([]byte("app 9 " + host + " relay.test 6667 192.0.2.20 77 2\n")); err != nil {
			return
		}

		stream, err := listener.Accept()
		if err != nil {
			return
		}
		defer stream.Close()
		if _, err := readRelayOpenFrame(bufio.NewReader(stream)); err != nil {
			return
		}
		encoder, err := magic.NewTokenEncoder(token)
		if err != nil {
			return
		}
		cipher, err := encoder.Encode([]byte(silentRelayGreeting))
		if err != nil {
			return
		}
		if _, err := stream.Write(cipher); err != nil {
			return
		}
		// Go quiet. Only the test's cleanup closes these sockets.
		<-done
	}()
	return listener.Addr().String()
}
