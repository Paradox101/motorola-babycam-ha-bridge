package magic

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

// fakeRelay accepts the control connection then the stream connection on one
// listener, mirroring the WEB2 opening sequence. It speaks the real codecs so
// the test exercises Dial, the app exchange, the relay-open frame and the
// token crypto end to end. serverReply is what the relay sends back after the
// client's first plaintext write; gotRequest receives the decoded client bytes.
type fakeRelay struct {
	listener    net.Listener
	token       string
	serverReply []byte
	gotOpen     chan RelayOpen
	gotRequest  chan []byte
	serverErr   chan error
}

func newFakeRelay(t *testing.T, token string, serverReply []byte) *fakeRelay {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relay := &fakeRelay{
		listener:    listener,
		token:       token,
		serverReply: serverReply,
		gotOpen:     make(chan RelayOpen, 1),
		gotRequest:  make(chan []byte, 1),
		serverErr:   make(chan error, 1),
	}
	go relay.serve()
	return relay
}

func (r *fakeRelay) dial(_ context.Context, _, _ string) (net.Conn, error) {
	return net.Dial("tcp", r.listener.Addr().String())
}

func (r *fakeRelay) fail(err error) { r.serverErr <- err }

func (r *fakeRelay) serve() {
	// 1. Control connection: read the app request, answer with an 8-field
	//    WEB2 response whose stream host routes back to this same listener.
	control, err := r.listener.Accept()
	if err != nil {
		r.fail(err)
		return
	}
	defer control.Close()
	if _, err := bufio.NewReader(control).ReadBytes('\n'); err != nil {
		r.fail(err)
		return
	}
	if _, err := control.Write([]byte("app 48 127.0.0.1 relay.test 6667 192.0.2.20 77 2\n")); err != nil {
		r.fail(err)
		return
	}

	// 2. Stream connection: frame the relay-open by length, then act as the
	//    peer. The frame carries no delimiter and may share a TCP segment with
	//    the first stream bytes, so read it via ReadRelayOpenFrame rather than a
	//    single naive Read (which was the cause of intermittent hangs).
	stream, err := r.listener.Accept()
	if err != nil {
		r.fail(err)
		return
	}
	defer stream.Close()
	br := bufio.NewReader(stream)
	open, err := ReadRelayOpenFrame(br)
	if err != nil {
		r.fail(err)
		return
	}
	r.gotOpen <- open

	decoder, err := NewTokenDecoder(r.token)
	if err != nil {
		r.fail(err)
		return
	}
	encoder, err := NewTokenEncoder(r.token)
	if err != nil {
		r.fail(err)
		return
	}

	// Read the client's plaintext request. The token bootstrap and the request
	// may span multiple reads, so decode in a loop until the full request (an
	// RTSP message terminated by a blank line) is recovered.
	var request []byte
	buf := make([]byte, 4096)
	for !bytes.HasSuffix(request, []byte("\r\n\r\n")) {
		n, err := br.Read(buf)
		if err != nil {
			r.fail(err)
			return
		}
		plain, err := decoder.Decode(buf[:n])
		if err != nil {
			r.fail(err)
			return
		}
		request = append(request, plain...)
	}
	r.gotRequest <- request

	// Reply through the inverse direction's crypto.
	cipher, err := encoder.Encode(r.serverReply)
	if err != nil {
		r.fail(err)
		return
	}
	if _, err := stream.Write(cipher); err != nil {
		r.fail(err)
		return
	}
	r.serverErr <- nil
}

func TestTunnelDialAndByteTransparentRoundTrip(t *testing.T) {
	const token = "TOK012345678901234567890123"
	const magicUUID = "0012345600aabbccddeeff00112233445566778899aabbccddeeff0011223344556677889900"
	const sessionName = "SESSION0123456789abcdef0123456789ab"
	clientRequest := []byte("OPTIONS rtsp://127.0.0.1/owner/streaming RTSP/1.0\r\nCSeq: 1\r\n\r\n")
	serverReply := []byte("RTSP/1.0 200 OK\r\nCSeq: 1\r\nPublic: OPTIONS, DESCRIBE, PLAY\r\n\r\n")

	relay := newFakeRelay(t, token, serverReply)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tunnel, err := Dial(ctx, TunnelConfig{
		ControlHost: "control.test",
		MagicUUID:   magicUUID,
		TargetPort:  6667,
		SessionName: sessionName,
		DeviceToken: token,
		Dial:        relay.dial,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	// The parsed control response must carry the selected relay parameters.
	if tunnel.Response.ConnectionNumber != 48 || tunnel.Response.StreamHost != "127.0.0.1" || tunnel.Response.Mode != ConnectionModeWEB2 {
		t.Fatalf("unexpected response: %+v", tunnel.Response)
	}

	if _, err := tunnel.Write(clientRequest); err != nil {
		t.Fatal(err)
	}

	// The relay-open frame the server saw must reuse the response's num/port.
	open := <-relay.gotOpen
	if open.ConnectionNumber != 48 || open.TargetPort != 6667 || open.MagicUUID != magicUUID || open.SessionName != sessionName {
		t.Fatalf("unexpected relay-open frame: %+v", open)
	}

	// The server must have recovered the exact plaintext the client wrote.
	if got := <-relay.gotRequest; string(got) != string(clientRequest) {
		t.Fatalf("relay decoded client bytes wrong:\n got %q\nwant %q", got, clientRequest)
	}

	// And the client must recover the exact server reply.
	received := make([]byte, 0, len(serverReply))
	tunnel.SetReadDeadline(time.Now().Add(5 * time.Second))
	for len(received) < len(serverReply) {
		chunk := make([]byte, 512)
		n, err := tunnel.Read(chunk)
		if err != nil {
			t.Fatalf("read reply: %v (got %q)", err, received)
		}
		received = append(received, chunk[:n]...)
	}
	if string(received) != string(serverReply) {
		t.Fatalf("client decoded server bytes wrong:\n got %q\nwant %q", received, serverReply)
	}

	if err := <-relay.serverErr; err != nil {
		t.Fatalf("relay error: %v", err)
	}
}

func TestTunnelDialRejectsMissingToken(t *testing.T) {
	_, err := Dial(context.Background(), TunnelConfig{ControlHost: "h", MagicUUID: "abc", SessionName: "s"})
	if err == nil {
		t.Fatal("expected error for missing device token")
	}
}
