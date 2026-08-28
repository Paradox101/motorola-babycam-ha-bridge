package fivegencare

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

// silentPeer returns a Client whose connections are accepted but never
// answered: the shape of a control host that is reachable but not responding.
func silentPeer(t *testing.T, timeout time.Duration) Client {
	t.Helper()
	return Client{
		Timeout: timeout,
		Dial: func(ctx context.Context, host string) (net.Conn, error) {
			client, server := net.Pipe()
			t.Cleanup(func() {
				_ = client.Close()
				_ = server.Close()
			})
			// The server end is never read from or written to.
			return client, nil
		},
	}
}

func TestExchangeFailsWhenTheHostAcceptsButNeverAnswers(t *testing.T) {
	client := silentPeer(t, 200*time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := client.RequestOTP(context.Background(), "device-uuid", "user@example.com")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the exchange succeeded against a silent host")
		}
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Logf("deadline surfaced as: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the exchange ignored Client.Timeout and is still blocked")
	}
}

func TestExchangeHonoursAStricterContextDeadline(t *testing.T) {
	// An hour of client timeout must not outlive a context that expires now.
	client := silentPeer(t, time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := client.RequestOTP(ctx, "device-uuid", "user@example.com")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the exchange succeeded against a silent host")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the exchange ignored the context deadline")
	}
}

func TestDevicesStopsAfterATransportFailureInsteadOfRetryingEveryToken(t *testing.T) {
	var dials int
	client := Client{
		Timeout: time.Second,
		Dial: func(ctx context.Context, host string) (net.Conn, error) {
			dials++
			return nil, errors.New("host unreachable")
		},
	}

	session := Session{UserID: 1, SessionToken: "session", MasterToken: "master", SessionID: "id", Domain: "host"}
	if _, err := client.Devices(context.Background(), session); err == nil {
		t.Fatal("Devices succeeded against an unreachable host")
	}
	if dials != 1 {
		t.Fatalf("dialled %d times for a transport failure, want 1", dials)
	}
}

func TestDevicesStillTriesTheMasterTokenAfterARejection(t *testing.T) {
	var dials int
	client := Client{
		Timeout: time.Second,
		Dial: func(ctx context.Context, host string) (net.Conn, error) {
			dials++
			client, server := net.Pipe()
			t.Cleanup(func() { _ = client.Close() })
			// Answer every command with an explicit rejection.
			go func() {
				defer server.Close()
				buffer := make([]byte, 256)
				for {
					if _, err := server.Read(buffer); err != nil {
						return
					}
					if _, err := server.Write([]byte("v3_session -1\n")); err != nil {
						return
					}
				}
			}()
			return client, nil
		},
	}

	session := Session{UserID: 1, SessionToken: "session", MasterToken: "master", SessionID: "id", Domain: "host"}
	_, err := client.Devices(context.Background(), session)
	if !errors.Is(err, ErrSessionRejected) {
		t.Fatalf("error = %v, want a session rejection", err)
	}
	if dials != 2 {
		t.Fatalf("dialled %d times for a rejection, want 2 (both token candidates)", dials)
	}
}
