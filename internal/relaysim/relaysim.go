// Package relaysim is a local stand-in for the Magic WEB2 relay plus an
// already-authorized camera peer. It speaks the real, reconstructed wire codecs
// (app-discovery response, relay-open framing, device-token tunnel crypto) and
// bridges the decrypted tunnel to a plaintext backend connection, e.g. an
// ordinary local RTSP camera.
//
// It exists to prove the transport end to end without the real service: the
// only thing it fakes is the 5GenCare-side authorization that attaches the
// camera to the session (see docs/missing-protocol-pieces.md). Everything the
// bridge and the tunnel do is exercised for real, including a full RTSP/RTP
// media flow when the backend serves one.
package relaysim

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/local/motorola-vm65-bridge/internal/magic"
)

// BackendDialer opens a fresh plaintext connection to the camera behind the
// relay for one tunnel session.
type BackendDialer func() (net.Conn, error)

// RelaySim serves Magic WEB2 sessions on a single listener and pipes each one,
// decrypted, to a backend connection.
type RelaySim struct {
	listener net.Listener
	token    string
	backend  BackendDialer

	wg sync.WaitGroup
}

// New creates a RelaySim over listener. token is the device token used as the
// tunnel crypto key; backend dials the camera for each session.
func New(listener net.Listener, token string, backend BackendDialer) *RelaySim {
	return &RelaySim{listener: listener, token: token, backend: backend}
}

// Addr reports the listener address.
func (s *RelaySim) Addr() net.Addr { return s.listener.Addr() }

// Dial returns a magic.DialFunc that reaches this simulator regardless of the
// requested address, for injection into bridge/tunnel configuration in tests.
func (s *RelaySim) Dial(_ context.Context, _, _ string) (net.Conn, error) {
	return net.Dial("tcp", s.listener.Addr().String())
}

// Serve accepts connections until ctx is cancelled or the listener is closed.
// The bridge opens a control connection (app-discovery) and a stream connection
// (relay-open) per session, and blocks on the control response before dialing
// the stream, so each connection is handled independently and distinguished by
// its first byte ('a' = app-discovery control, 'v' = relay-open stream) rather
// than by accept order.
func (s *RelaySim) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.listener.Close()
	}()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.wg.Wait()
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(conn)
		}()
	}
}

func (s *RelaySim) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	first, err := br.Peek(1)
	if err != nil {
		return
	}
	switch first[0] {
	case 'a':
		s.handleControl(conn, br)
	case 'v':
		s.handleStream(conn, br)
	}
}

// handleControl answers the app-discovery request and holds the control socket
// open for the tunnel's lifetime, as the real relay does. The stream host is
// cosmetic here because the bridge dials the stream via the injected Dial (or a
// real host:port in a live demo).
func (s *RelaySim) handleControl(conn net.Conn, br *bufio.Reader) {
	if _, err := br.ReadBytes('\n'); err != nil {
		return
	}
	if _, err := conn.Write([]byte("app 48 127.0.0.1 127.0.0.1 6667 192.0.2.20 77 2\n")); err != nil {
		return
	}
	// Keep the control connection open until the bridge closes it.
	_, _ = io.Copy(io.Discard, br)
}

// handleStream frames the relay-open, then bridges the decrypted tunnel to the
// backend camera in both directions.
func (s *RelaySim) handleStream(stream net.Conn, br *bufio.Reader) {
	if _, err := magic.ReadRelayOpenFrame(br); err != nil {
		return
	}
	decoder, err := magic.NewTokenDecoder(s.token)
	if err != nil {
		return
	}
	encoder, err := magic.NewTokenEncoder(s.token)
	if err != nil {
		return
	}
	camera, err := s.backend()
	if err != nil {
		return
	}
	defer camera.Close()

	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			stream.Close()
			camera.Close()
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// client -> camera: decrypt cipher from the tunnel, write plaintext.
	go func() {
		defer wg.Done()
		defer closeAll()
		buf := make([]byte, 32*1024)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				plain, decErr := decoder.Decode(buf[:n])
				if decErr != nil {
					return
				}
				if len(plain) > 0 {
					if _, werr := camera.Write(plain); werr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// camera -> client: encrypt plaintext toward the tunnel.
	go func() {
		defer wg.Done()
		defer closeAll()
		buf := make([]byte, 32*1024)
		for {
			n, err := camera.Read(buf)
			if n > 0 {
				cipher, encErr := encoder.Encode(buf[:n])
				if encErr != nil {
					return
				}
				if _, werr := stream.Write(cipher); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	wg.Wait()
}
