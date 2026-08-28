package bridge

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBridgeDialRetry proves the bridge recovers from transient relay-open
// failures: the first two dial attempts error, the third succeeds, and the
// client then completes an RTSP exchange.
func TestBridgeDialRetry(t *testing.T) {
	backend := startRTSPBackend(t)
	relay := newRelayCamera(t, testToken, backend)

	var mu sync.Mutex
	failsLeft := 2
	flakyDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		fail := failsLeft > 0
		if fail {
			failsLeft--
		}
		mu.Unlock()
		if fail {
			return nil, &net.OpError{Op: "dial", Err: errTransient{}}
		}
		return relay.dial(ctx, network, addr)
	}

	b, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Credentials: testCreds(),
		Dial:        flakyDial,
		DialBackoff: time.Millisecond, // keep the test fast
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
	resp := readAvailable(t, client)
	if !strings.Contains(resp, "RTSP/1.0 200 OK") {
		t.Fatalf("expected 200 after retry, got:\n%q", resp)
	}
	mu.Lock()
	defer mu.Unlock()
	if failsLeft != 0 {
		t.Fatalf("expected all injected failures to be consumed, %d left", failsLeft)
	}
}

type errTransient struct{}

func (errTransient) Error() string   { return "transient dial failure" }
func (errTransient) Temporary() bool { return true }

// TestBridgeNoCameraPeerDiagnostic verifies that when the relay accepts the
// session but no camera attaches (the stream closes with zero bytes), the
// bridge logs the explicit 5GenCare-authorization diagnostic.
func TestBridgeNoCameraPeerDiagnostic(t *testing.T) {
	relayAddr := startNoPeerRelay(t)
	dialToRelay := func(_ context.Context, _, _ string) (net.Conn, error) {
		return net.Dial("tcp", relayAddr)
	}

	rec := &recordingHandler{}
	logger := slog.New(rec)

	b, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Credentials: testCreds(),
		Dial:        dialToRelay,
		Logger:      logger,
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

	// The relay closes the stream with no data; the bridge should observe zero
	// camera bytes and warn. Wait for the diagnostic.
	waitFor(t, func() bool {
		return rec.has(slog.LevelWarn, "camera did not attach")
	})
}

// startNoPeerRelay accepts the control connection (answering the app request),
// then the stream connection (reading relay-open), then closes the stream
// immediately without sending any bytes — the "session held open, no camera
// peer" case observed against the real relay.
func startNoPeerRelay(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	host, _, _ := net.SplitHostPort(listener.Addr().String())

	go func() {
		control, err := listener.Accept()
		if err != nil {
			return
		}
		if _, err := bufio.NewReader(control).ReadBytes('\n'); err != nil {
			_ = control.Close()
			return
		}
		_, _ = control.Write([]byte("app 9 " + host + " relay.test 6667 192.0.2.20 77 2\n"))

		stream, err := listener.Accept()
		if err != nil {
			_ = control.Close()
			return
		}
		// Read the relay-open frame, then drop the stream with no media.
		_, _ = readRelayOpenFrame(bufio.NewReader(stream))
		_ = stream.Close()
		// Hold the control side briefly so the bridge tears down via the stream.
		time.Sleep(50 * time.Millisecond)
		_ = control.Close()
	}()
	return listener.Addr().String()
}

// recordingHandler is a minimal slog.Handler capturing records for assertions.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) has(level slog.Level, substr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Level == level && strings.Contains(r.Message, substr) {
			return true
		}
	}
	return false
}
