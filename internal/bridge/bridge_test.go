package bridge

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/magic"
)

// fakeRelay speaks the real WEB2 codecs so the bridge is exercised end to end:
// it accepts the control connection, answers the app-discovery with an 8-field
// WEB2 response, accepts the stream connection, reads the relay-open frame, then
// acts as the camera peer using the device-token crypto.
type fakeRelay struct {
	listener    net.Listener
	token       string
	serverReply []byte
	serverErr   chan error
}

func newFakeRelay(t *testing.T, token string, serverReply []byte) *fakeRelay {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRelay{listener: listener, token: token, serverReply: serverReply, serverErr: make(chan error, 1)}
	go r.serve()
	t.Cleanup(func() { listener.Close() })
	return r
}

func (r *fakeRelay) dial(_ context.Context, _, _ string) (net.Conn, error) {
	return net.Dial("tcp", r.listener.Addr().String())
}

func (r *fakeRelay) serve() {
	control, err := r.listener.Accept()
	if err != nil {
		r.serverErr <- err
		return
	}
	defer control.Close()
	if _, err := bufio.NewReader(control).ReadBytes('\n'); err != nil {
		r.serverErr <- err
		return
	}
	if _, err := control.Write([]byte("app 48 127.0.0.1 relay.test 6667 192.0.2.20 77 2\n")); err != nil {
		r.serverErr <- err
		return
	}

	stream, err := r.listener.Accept()
	if err != nil {
		r.serverErr <- err
		return
	}
	defer stream.Close()
	// Frame the relay-open by length; it may share a TCP segment with the first
	// stream bytes, so a single naive Read would intermittently mis-parse it.
	br := bufio.NewReader(stream)
	if _, err := magic.ReadRelayOpenFrame(br); err != nil {
		r.serverErr <- err
		return
	}

	decoder, err := magic.NewTokenDecoder(r.token)
	if err != nil {
		r.serverErr <- err
		return
	}
	encoder, err := magic.NewTokenEncoder(r.token)
	if err != nil {
		r.serverErr <- err
		return
	}
	// Consume one chunk of the client's request (content not asserted here); the
	// reply direction has independent crypto, so a partial decode is harmless.
	buf := make([]byte, 4096)
	n, err := br.Read(buf)
	if err != nil {
		r.serverErr <- err
		return
	}
	if _, err := decoder.Decode(buf[:n]); err != nil {
		r.serverErr <- err
		return
	}
	cipher, err := encoder.Encode(r.serverReply)
	if err != nil {
		r.serverErr <- err
		return
	}
	if _, err := stream.Write(cipher); err != nil {
		r.serverErr <- err
		return
	}
	r.serverErr <- nil
}

func TestBridgeEndToEnd(t *testing.T) {
	const token = "TOK012345678901234567890123"
	clientRequest := []byte("OPTIONS rtsp://127.0.0.1/owner/streaming RTSP/1.0\r\nCSeq: 1\r\n\r\n")
	serverReply := []byte("RTSP/1.0 200 OK\r\nCSeq: 1\r\nPublic: OPTIONS, DESCRIBE, PLAY\r\n\r\n")

	relay := newFakeRelay(t, token, serverReply)

	srv, err := New(Config{
		Credentials: Credentials{
			DeviceID:    0x00123456,
			SID:         "SID01234567890123456",
			DeviceToken: token,
			ControlHost: "control.test",
			TargetPort:  6667,
		},
		ListenAddr: "127.0.0.1:0",
		Dial:       relay.dial,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ctx) }()

	addr := waitForAddr(t, srv)

	client, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if _, err := client.Write(clientRequest); err != nil {
		t.Fatal(err)
	}

	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	received := make([]byte, 0, len(serverReply))
	for len(received) < len(serverReply) {
		chunk := make([]byte, 256)
		n, err := client.Read(chunk)
		if err != nil {
			t.Fatalf("client read: %v (got %q)", err, received)
		}
		received = append(received, chunk[:n]...)
	}
	if string(received) != string(serverReply) {
		t.Fatalf("client got %q, want %q", received, serverReply)
	}

	if err := <-relay.serverErr; err != nil {
		t.Fatalf("relay error: %v", err)
	}

	cancel()
	if err := <-serveErr; err != nil {
		t.Fatalf("serve returned error: %v", err)
	}
}

func TestNewRejectsMissingCredentials(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"no listen", Config{Credentials: Credentials{ControlHost: "h", DeviceToken: "TOK012345678901234567890123", SID: "SID01234567890123456"}}},
		{"no control host", Config{ListenAddr: "127.0.0.1:0", Credentials: Credentials{DeviceToken: "TOK012345678901234567890123", SID: "SID01234567890123456"}}},
		{"no token", Config{ListenAddr: "127.0.0.1:0", Credentials: Credentials{ControlHost: "h"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func waitForAddr(t *testing.T, srv *Server) net.Addr {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if addr := srv.Addr(); addr != nil {
			return addr
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server did not bind in time")
	return nil
}
