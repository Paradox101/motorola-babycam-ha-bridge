package bridge

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/magic"
)

// The credentials below are non-secret test fixtures shaped like the real
// inputs. They are sufficient for GenerateMagicUUID and the codecs.
const (
	testDeviceID = 0x00123456
	testSID      = "SID0123456789"
	testToken    = "TOK012345678901234567890123"
)

// relayCamera is an in-process stand-in for the relay plus the camera behind
// it. It speaks the real Magic WEB2 wire format (app discovery, relay-open,
// device-token crypto) and then relays plaintext to and from a local TCP
// server, so a bridge client reaches that server with no Android in the loop.
type relayCamera struct {
	listener net.Listener
	token    string
	backend  string // address of the plaintext camera backend
	wg       sync.WaitGroup
}

func newRelayCamera(t *testing.T, token, backend string) *relayCamera {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rc := &relayCamera{listener: listener, token: token, backend: backend}
	// Close the listener and join every handler goroutine before the test's
	// other cleanups finish, so none survives to log on a completed test.
	t.Cleanup(func() {
		_ = listener.Close()
		rc.wg.Wait()
	})
	rc.wg.Add(1)
	go rc.serve(t)
	return rc
}

// dial is the magic.DialFunc the bridge uses: every relay connection lands on
// this in-process listener.
func (rc *relayCamera) dial(_ context.Context, _, _ string) (net.Conn, error) {
	return net.Dial("tcp", rc.listener.Addr().String())
}

func (rc *relayCamera) serve(t *testing.T) {
	defer rc.wg.Done()
	for {
		conn, err := rc.listener.Accept()
		if err != nil {
			return
		}
		rc.wg.Add(1)
		go func() {
			defer rc.wg.Done()
			rc.handleControlOrStream(t, conn)
		}()
	}
}

// The bridge opens the control connection first, then the stream connection,
// on the same address. We distinguish them by first byte: the app request
// starts with "app ", the relay-open frame starts with 'v'.
func (rc *relayCamera) handleControlOrStream(t *testing.T, conn net.Conn) {
	reader := bufio.NewReader(conn)
	first, err := reader.Peek(1)
	if err != nil {
		_ = conn.Close()
		return
	}
	if first[0] == 'v' {
		rc.handleStream(t, conn, reader)
		return
	}
	rc.handleControl(t, conn, reader)
}

func (rc *relayCamera) handleControl(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	defer conn.Close()
	if _, err := reader.ReadBytes('\n'); err != nil {
		return
	}
	// Route the stream host back to this same listener.
	host, _, _ := net.SplitHostPort(rc.listener.Addr().String())
	_, _ = conn.Write([]byte("app 7 " + host + " relay.test 6667 192.0.2.20 77 2\n"))
	// Hold the control connection open for the session lifetime.
	_, _ = io.Copy(io.Discard, reader)
}

func (rc *relayCamera) handleStream(t *testing.T, conn net.Conn, reader *bufio.Reader) {
	defer conn.Close()
	// Read exactly the relay-open frame; the first ciphertext write may have
	// coalesced behind it on the wire, so leave the remainder buffered.
	frame, err := readRelayOpenFrame(reader)
	if err != nil {
		t.Errorf("read relay-open: %v", err)
		return
	}
	if _, err := magic.ParseRelayOpen(frame); err != nil {
		t.Errorf("relay-open parse: %v", err)
		return
	}
	buf := make([]byte, 4096)

	encoder, err := magic.NewTokenEncoder(rc.token)
	if err != nil {
		t.Errorf("encoder: %v", err)
		return
	}
	decoder, err := magic.NewTokenDecoder(rc.token)
	if err != nil {
		t.Errorf("decoder: %v", err)
		return
	}

	backend, err := net.Dial("tcp", rc.backend)
	if err != nil {
		t.Errorf("dial backend: %v", err)
		return
	}
	defer backend.Close()

	var inner sync.WaitGroup
	inner.Add(2)
	// relay -> backend: decode ciphertext, forward plaintext.
	go func() {
		defer inner.Done()
		defer backend.Close() // unblock the other direction
		for {
			m, err := reader.Read(buf)
			if m > 0 {
				plain, derr := decoder.Decode(buf[:m])
				if derr != nil {
					return
				}
				if len(plain) > 0 {
					if _, werr := backend.Write(plain); werr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
	// backend -> relay: encode plaintext toward the bridge.
	go func() {
		defer inner.Done()
		defer conn.Close() // unblock the other direction
		b := make([]byte, 4096)
		for {
			m, err := backend.Read(b)
			if m > 0 {
				cipher, eerr := encoder.Encode(b[:m])
				if eerr != nil {
					return
				}
				if _, werr := conn.Write(cipher); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	inner.Wait()
}

// readRelayOpenFrame reads exactly one relay-open frame from r, using its fixed
// header widths and embedded length prefixes to know where it ends. Any bytes
// read past the frame stay buffered in r for the crypto stream that follows.
// Frame: v<3>' '<3>' '<5>' '<mLen 3>' '<magic mLen>' '<sLen 4>' '<session sLen>.
func readRelayOpenFrame(r *bufio.Reader) ([]byte, error) {
	header := make([]byte, 19) // through the 3-digit magic-UUID length + its space
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	magicLen := atoiField(header[15:18])
	// magicUUID + ' ' + 4-digit session length + ' '
	mid := make([]byte, magicLen+6)
	if _, err := io.ReadFull(r, mid); err != nil {
		return nil, err
	}
	sessionLen := atoiField(mid[magicLen+1 : magicLen+5])
	session := make([]byte, sessionLen)
	if _, err := io.ReadFull(r, session); err != nil {
		return nil, err
	}
	frame := append(append(append([]byte{}, header...), mid...), session...)
	return frame, nil
}

func atoiField(b []byte) int {
	n := 0
	for _, c := range b {
		n = n*10 + int(c-'0')
	}
	return n
}

// startRTSPBackend is a tiny stand-in camera that answers RTSP OPTIONS, so the
// test asserts a real request/response crossing the tunnel, not just bytes.
func startRTSPBackend(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if strings.HasPrefix(line, "OPTIONS") {
						// Drain to blank line, then answer.
						for {
							l, err := r.ReadString('\n')
							if err != nil || l == "\r\n" || l == "\n" {
								break
							}
						}
						_, _ = conn.Write([]byte("RTSP/1.0 200 OK\r\nCSeq: 1\r\nPublic: OPTIONS, DESCRIBE, PLAY\r\n\r\n"))
					}
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func testCreds() Credentials {
	return Credentials{
		DeviceID:    testDeviceID,
		SID:         testSID,
		DeviceToken: testToken,
		ControlHost: "control.test",
		TargetPort:  6667,
	}
}

// TestBridgeEndToEndRTSP proves the whole local path without Android: an RTSP
// client connects to the bridge's loopback port, and its OPTIONS request and
// the camera's response cross an actual Magic WEB2 tunnel intact.
func TestBridgeEndToEndRTSP(t *testing.T) {
	backend := startRTSPBackend(t)
	relay := newRelayCamera(t, testToken, backend)

	b, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		Credentials: testCreds(),
		Dial:        relay.dial,
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

	req := "OPTIONS rtsp://127.0.0.1/owner/streaming RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: bridge-test\r\n\r\n"
	if _, err := client.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}

	resp := readAvailable(t, client)
	if !strings.Contains(resp, "RTSP/1.0 200 OK") || !strings.Contains(resp, "Public: OPTIONS") {
		t.Fatalf("unexpected RTSP response through tunnel:\n%q", resp)
	}

	if total, _ := b.Stats(); total != 1 {
		t.Fatalf("expected 1 accepted session, got %d", total)
	}
}

// TestBridgeCloseStopsServe verifies Serve returns cleanly after Close and that
// active sessions are torn down.
func TestBridgeCloseStopsServe(t *testing.T) {
	backend := startRTSPBackend(t)
	relay := newRelayCamera(t, testToken, backend)
	b, err := New(Config{ListenAddr: "127.0.0.1:0", Credentials: testCreds(), Dial: relay.dial})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Listen(); err != nil {
		t.Fatal(err)
	}

	served := make(chan error, 1)
	go func() { served <- b.Serve(context.Background()) }()

	// Open a client so there is an active session to tear down.
	client, err := net.Dial("tcp", b.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	waitFor(t, func() bool { _, active := b.Stats(); return active == 1 })

	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve returned error after close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after close")
	}
	waitFor(t, func() bool { _, active := b.Stats(); return active == 0 })
}

func TestNewValidatesInput(t *testing.T) {
	cases := map[string]Config{
		"no listen addr":  {Credentials: testCreds()},
		"no sid":          {ListenAddr: "127.0.0.1:0", Credentials: Credentials{DeviceToken: testToken, ControlHost: "h"}},
		"no token":        {ListenAddr: "127.0.0.1:0", Credentials: Credentials{SID: testSID, ControlHost: "h"}},
		"no control host": {ListenAddr: "127.0.0.1:0", Credentials: Credentials{SID: testSID, DeviceToken: testToken}},
	}
	for name, cfg := range cases {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func readAvailable(t *testing.T, conn net.Conn) string {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil && n == 0 {
		t.Fatalf("read: %v", err)
	}
	return string(buf[:n])
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
