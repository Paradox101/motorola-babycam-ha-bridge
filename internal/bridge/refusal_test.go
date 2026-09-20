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

// TestBridgeDoesNotRetryARelayRefusal pins the behaviour a night of "connection
// number must be positive" showed to be pointless: the relay answering the app
// request with a non-positive number is a complete answer (no camera to
// connect), and asking it twice more a second apart never changed it. One
// request per session, logged as the refusal it is, not as a dial that failed.
func TestBridgeDoesNotRetryARelayRefusal(t *testing.T) {
	relay := startRefusingControlHost(t)
	rec := &recordingHandler{}

	b, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Credentials: testCreds(),
		Dial:        relay.dial,
		// A retry would wait this long and blow the test's timeout.
		DialBackoff:     30 * time.Second,
		RefusalCooldown: 50 * time.Millisecond,
		Logger:          slog.New(rec),
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

	// The session has to have been accepted before "no active session" means
	// it ended.
	waitFor(t, func() bool {
		total, active := b.Stats()
		return total == 1 && active == 0
	})
	if got := relay.requests(); got != 1 {
		t.Fatalf("the relay saw %d app requests for one refused session, want 1", got)
	}
	if rec.has(slog.LevelWarn, "retrying relay dial") {
		t.Fatal("a refusal was retried")
	}
	if !rec.has(slog.LevelWarn, "relay refused the session") {
		t.Fatal("the refusal was not logged as one")
	}
	if rec.has(slog.LevelError, "relay dial failed") {
		t.Fatal("a refusal was logged as a failed dial")
	}
	// The client is told nothing it could mistake for the camera: the session
	// simply ends.
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if n, _ := client.Read(make([]byte, 1)); n != 0 {
		t.Fatal("a refused session sent bytes to the client")
	}
}

// TestBridgeLeavesTheRelayAloneAfterARefusal covers the cooldown. A media server
// with waiting consumers reconnects the instant a dial fails; without this,
// each of those reconnects was another app request to a relay that had just
// said no. The next client waits out the cooldown and then gets a real attempt
// — the camera may be back — while a client that leaves during the wait ends
// its session without the relay ever hearing about it.
func TestBridgeLeavesTheRelayAloneAfterARefusal(t *testing.T) {
	relay := startRefusingControlHost(t)
	rec := &recordingHandler{}

	b, err := New(Config{
		ListenAddr:      "127.0.0.1:0",
		Credentials:     testCreds(),
		Dial:            relay.dial,
		DialBackoff:     30 * time.Second,
		RefusalCooldown: 400 * time.Millisecond,
		Logger:          slog.New(rec),
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
	waitFor(t, func() bool { return relay.requests() == 1 })
	waitFor(t, func() bool {
		_, active := b.Stats()
		return active == 0
	})
	refusedAt := time.Now()

	// Inside the cooldown: this client is held, and the relay is not asked.
	second, err := net.Dial("tcp", b.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	waitFor(t, func() bool { return rec.has(slog.LevelInfo, "waiting before asking it again") })
	if got := relay.requests(); got != 1 {
		t.Fatalf("the relay was asked again %d times inside the cooldown", got-1)
	}

	// A client that gives up while held costs the relay nothing either.
	third, err := net.Dial("tcp", b.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		_, active := b.Stats()
		return active == 2
	})
	_ = third.Close()
	waitFor(t, func() bool {
		_, active := b.Stats()
		return active == 1
	})
	if got := relay.requests(); got != 1 {
		t.Fatalf("a client that left during the cooldown reached the relay: %d requests", got)
	}

	// Once the cooldown has passed, the client that stayed gets a real attempt.
	waitFor(t, func() bool { return relay.requests() == 2 })
	if since := time.Since(refusedAt); since < 350*time.Millisecond {
		t.Fatalf("the relay was asked again after %v, before the cooldown ended", since)
	}
	waitFor(t, func() bool {
		_, active := b.Stats()
		return active == 0
	})
}

// TestBridgeRefusalCooldownCanBeDisabled keeps the option honest: a negative
// value means every client asks the relay itself.
func TestBridgeRefusalCooldownCanBeDisabled(t *testing.T) {
	relay := startRefusingControlHost(t)

	b, err := New(Config{
		ListenAddr:      "127.0.0.1:0",
		Credentials:     testCreds(),
		Dial:            relay.dial,
		DialBackoff:     30 * time.Second,
		RefusalCooldown: -1,
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

	for i := 0; i < 2; i++ {
		client, err := net.Dial("tcp", b.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { return relay.requests() == i+1 })
		waitFor(t, func() bool {
			_, active := b.Stats()
			return active == 0
		})
		_ = client.Close()
	}
}

// refusingControlHost answers every app request the way the real relay did for
// hours on 2026-09-17: a non-positive connection number and nothing else.
type refusingControlHost struct {
	listener net.Listener
	mu       sync.Mutex
	count    int
	wg       sync.WaitGroup
}

func startRefusingControlHost(t *testing.T) *refusingControlHost {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := &refusingControlHost{listener: listener}
	t.Cleanup(func() {
		_ = listener.Close()
		h.wg.Wait()
	})
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			h.wg.Add(1)
			go func() {
				defer h.wg.Done()
				defer conn.Close()
				if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
					return
				}
				h.mu.Lock()
				h.count++
				h.mu.Unlock()
				_, _ = conn.Write([]byte("app 0\n"))
			}()
		}
	}()
	return h
}

func (h *refusingControlHost) dial(_ context.Context, _, _ string) (net.Conn, error) {
	return net.Dial("tcp", h.listener.Addr().String())
}

// requests counts the app requests answered so far.
func (h *refusingControlHost) requests() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

// TestBridgeWaitingClientsShareOneQuestion covers what a restart showed with a
// camera that was off: go2rtc's read timeout is shorter than the cooldown, so
// it reconnected while its previous connection was still held, three sessions
// reached the end of the same cooldown together, and all three asked the relay
// — three refusals per cooldown instead of one. Now one of them asks and the
// others take its answer.
func TestBridgeWaitingClientsShareOneQuestion(t *testing.T) {
	relay := startRefusingControlHost(t)
	rec := &recordingHandler{}

	b, err := New(Config{
		ListenAddr:      "127.0.0.1:0",
		Credentials:     testCreds(),
		Dial:            relay.dial,
		DialBackoff:     30 * time.Second,
		RefusalCooldown: 300 * time.Millisecond,
		Logger:          slog.New(rec),
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
	waitFor(t, func() bool {
		_, active := b.Stats()
		return relay.requests() == 1 && active == 0
	})

	// Three clients arrive inside the cooldown and all stay.
	for i := 0; i < 3; i++ {
		client, err := net.Dial("tcp", b.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
	}
	waitFor(t, func() bool { return rec.count(slog.LevelInfo, "waiting before asking it again") == 3 })
	waitFor(t, func() bool {
		total, active := b.Stats()
		return total == 4 && active == 0
	})

	if got := relay.requests(); got != 2 {
		t.Fatalf("the relay saw %d app requests; want 2: one before the cooldown and one for the three sessions that waited it out", got)
	}
	if got := rec.count(slog.LevelWarn, "relay refused the session"); got != 2 {
		t.Fatalf("%d sessions logged a refusal of their own, want 2", got)
	}
	if got := rec.count(slog.LevelInfo, "while this session waited"); got != 2 {
		t.Fatalf("%d sessions took the shared answer, want 2", got)
	}
}

// TestBridgeWaitingClientsDialOnceTheRelayAccepts is the other half: when the
// one session that asked gets a relay session, the camera is back, and the
// sessions that waited with it dial for themselves — each client needs a relay
// session of its own.
func TestBridgeWaitingClientsDialOnceTheRelayAccepts(t *testing.T) {
	relay := startRelayRefusingFirst(t)
	rec := &recordingHandler{}

	b, err := New(Config{
		ListenAddr:      "127.0.0.1:0",
		Credentials:     testCreds(),
		Dial:            relay.dial,
		DialBackoff:     30 * time.Second,
		RefusalCooldown: 300 * time.Millisecond,
		Logger:          slog.New(rec),
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
	waitFor(t, func() bool {
		_, active := b.Stats()
		return relay.requests() == 1 && active == 0
	})

	for i := 0; i < 3; i++ {
		client, err := net.Dial("tcp", b.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
	}
	waitFor(t, func() bool { return rec.count(slog.LevelInfo, "waiting before asking it again") == 3 })
	waitFor(t, func() bool { return rec.count(slog.LevelInfo, "relay session open") == 3 })
	if got := relay.requests(); got != 4 {
		t.Fatalf("the relay saw %d app requests; want 4: the refused one and one per session that waited", got)
	}
	if rec.has(slog.LevelInfo, "while this session waited") {
		t.Fatal("a session took a refusal as its answer after the relay had accepted")
	}
}

// count reports how many records at level carry substr in their message.
func (h *recordingHandler) count(level slog.Level, substr string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == level && strings.Contains(r.Message, substr) {
			n++
		}
	}
	return n
}

// relayRefusingFirst refuses the first app request and opens a relay session
// for every later one, with no camera attached: the stream is accepted, its
// relay-open frame read, and then closed.
type relayRefusingFirst struct {
	listener net.Listener
	mu       sync.Mutex
	count    int
	wg       sync.WaitGroup
}

func startRelayRefusingFirst(t *testing.T) *relayRefusingFirst {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, _, _ := net.SplitHostPort(listener.Addr().String())
	h := &relayRefusingFirst{listener: listener}
	t.Cleanup(func() {
		_ = listener.Close()
		h.wg.Wait()
	})
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			h.wg.Add(1)
			go func() {
				defer h.wg.Done()
				defer conn.Close()
				reader := bufio.NewReader(conn)
				// Control and stream connections arrive on the same
				// listener; an app request starts with "app", a relay-open
				// frame with its version letter.
				if lead, err := reader.Peek(1); err != nil || lead[0] != 'a' {
					_, _ = readRelayOpenFrame(reader)
					return
				}
				if _, err := reader.ReadBytes('\n'); err != nil {
					return
				}
				h.mu.Lock()
				h.count++
				n := h.count
				h.mu.Unlock()
				if n == 1 {
					_, _ = conn.Write([]byte("app 0\n"))
					return
				}
				_, _ = conn.Write([]byte("app 9 " + host + " relay.test 6667 192.0.2.20 77 2\n"))
				// Hold the control side until the bridge closes it.
				_, _ = reader.ReadByte()
			}()
		}
	}()
	return h
}

func (h *relayRefusingFirst) dial(_ context.Context, _, _ string) (net.Conn, error) {
	return net.Dial("tcp", h.listener.Addr().String())
}

func (h *relayRefusingFirst) requests() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}
