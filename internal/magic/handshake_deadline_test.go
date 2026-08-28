package magic

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// silentRelay accepts connections and then never sends a byte. It is the exact
// shape of a relay that is reachable but not answering.
func silentRelay(t *testing.T) (host string, port int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	var mu sync.Mutex
	var accepted []net.Conn
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			accepted = append(accepted, conn)
			mu.Unlock()
		}
	}()
	// Registered after the listener cleanup, so it runs first (LIFO) and closes
	// the accepted sockets while the accept loop is still draining.
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range accepted {
			_ = conn.Close()
		}
	})

	address := listener.Addr().(*net.TCPAddr)
	return address.IP.String(), address.Port
}

func silentRelayConfig(host string, port int) TunnelConfig {
	return TunnelConfig{
		ControlHost: host,
		ControlPort: port,
		MagicUUID:   "0000000babcde" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TargetPort:  6667,
		SessionName: "11111111-2222-3333-4444-555555555555",
		DeviceToken: "devicetokenvalue",
	}
}

func TestDialFailsWhenControlHostAcceptsButStaysSilent(t *testing.T) {
	host, port := silentRelay(t)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := Dial(ctx, silentRelayConfig(host, port))
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Dial succeeded against a silent relay")
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("Dial took %s to honour a 300ms deadline", elapsed)
		}
		if !errors.Is(err, os.ErrDeadlineExceeded) && !errors.Is(err, net.ErrClosed) {
			t.Logf("deadline surfaced as: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Dial ignored the context deadline and is still blocked")
	}
}

func TestDialUsesHandshakeTimeoutWhenContextHasNoDeadline(t *testing.T) {
	host, port := silentRelay(t)

	config := silentRelayConfig(host, port)
	config.HandshakeTimeout = 250 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		_, err := Dial(context.Background(), config)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Dial succeeded against a silent relay")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Dial blocked forever on a context without a deadline")
	}
}

func TestDialAbortsWhenContextIsCancelled(t *testing.T) {
	host, port := silentRelay(t)

	// A deadline far beyond the test: only cancellation can end this dial.
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := Dial(ctx, silentRelayConfig(host, port))
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Dial succeeded against a silent relay")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the context did not abort the handshake")
	}
}
