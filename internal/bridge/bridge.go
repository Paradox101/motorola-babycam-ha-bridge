// Package bridge exposes the proven Magic WEB2 tunnel as a local TCP endpoint.
//
// It listens on a local address and, for every accepted connection, opens a
// fresh Magic WEB2 relay session to the camera's target port and copies bytes
// transparently in both directions. Because the Magic tunnel is byte-
// transparent (see internal/magic), a standard RTSP-over-TCP client such as
// go2rtc or ffmpeg can connect to the local address and speak to the camera as
// if it were on the LAN.
//
// Scope and honesty: this bridge only reconstructs the Magic transport layer,
// which is fully proven. It consumes credentials (device id, SID, device token,
// control host) that the app obtains earlier from the 5GenCare control flow.
// This package does NOT derive, refresh or fabricate those credentials, and it
// does NOT perform the 5GenCare-side authorization that signals the camera to
// attach to the relay session. Without that authorization the relay accepts the
// session but no camera bytes flow. See docs/missing-protocol-pieces.md.
package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/magic"
)

// DefaultTargetPort is the camera RTSP target port observed in the measured
// session.
const DefaultTargetPort = 6667

// DefaultDialTimeout bounds a single relay-open handshake.
const DefaultDialTimeout = 15 * time.Second

// Credentials are the per-device inputs the Magic tunnel needs. Every field is
// a value the app obtains from the 5GenCare control flow; this package never
// derives them.
type Credentials struct {
	DeviceID    uint32
	SID         string
	DeviceToken string
	ControlHost string
	ControlPort int // defaults to magic.ControlPortDefault when zero
	TargetPort  int // defaults to DefaultTargetPort when zero
}

// Config configures a Server.
type Config struct {
	Credentials

	// ListenAddr is the local address the bridge listens on, e.g.
	// "127.0.0.1:8554". A downstream RTSP client connects here.
	ListenAddr string

	// DialTimeout bounds each relay-open handshake; zero uses
	// DefaultDialTimeout.
	DialTimeout time.Duration

	// Dial is injected in tests to reach an in-memory relay; nil uses the real
	// network via magic.Dial's default dialer.
	Dial magic.DialFunc

	// Logf receives operational log lines. It must never be called with secret
	// values. nil disables logging.
	Logf func(format string, args ...any)
}

// Server is a running local-to-Magic TCP bridge.
type Server struct {
	cfg       Config
	magicUUID string

	mu       sync.Mutex
	listener net.Listener
}

// New validates the configuration and derives the stable magicUuid. It fails
// fast on missing credentials so misconfiguration surfaces before any relay
// traffic.
func New(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		return nil, errors.New("listen address is required")
	}
	if cfg.ControlHost == "" {
		return nil, errors.New("control host is required")
	}
	if cfg.DeviceToken == "" {
		return nil, errors.New("device token is required")
	}
	if cfg.TargetPort == 0 {
		cfg.TargetPort = DefaultTargetPort
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = DefaultDialTimeout
	}
	magicUUID, err := magic.GenerateMagicUUID(cfg.DeviceID, cfg.SID, cfg.DeviceToken)
	if err != nil {
		return nil, fmt.Errorf("derive magic uuid: %w", err)
	}
	return &Server{cfg: cfg, magicUUID: magicUUID}, nil
}

func (s *Server) logf(format string, args ...any) {
	if s.cfg.Logf != nil {
		s.cfg.Logf(format, args...)
	}
}

// Addr reports the address the server is listening on, or nil before Serve has
// bound the listener.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Serve binds the listen address and accepts connections until ctx is cancelled
// or a non-temporary accept error occurs. Each accepted connection is handled in
// its own goroutine. Serve returns nil on a clean ctx cancellation.
func (s *Server) Serve(ctx context.Context) error {
	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.ListenAddr, err)
	}
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	s.logf("bridge listening on %s -> Magic control %s:%d target %d",
		listener.Addr(), s.cfg.ControlHost, s.controlPort(), s.cfg.TargetPort)

	// Close the listener when ctx is cancelled so Accept unblocks.
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handle(ctx, conn)
		}()
	}
}

func (s *Server) controlPort() int {
	if s.cfg.ControlPort != 0 {
		return s.cfg.ControlPort
	}
	return magic.ControlPortDefault
}

// handle opens a fresh Magic tunnel for one downstream connection and bridges
// bytes both ways until either side closes.
func (s *Server) handle(ctx context.Context, downstream net.Conn) {
	defer downstream.Close()
	peer := downstream.RemoteAddr()
	s.logf("accepted %s, opening Magic relay session", peer)

	dialCtx, cancel := context.WithTimeout(ctx, s.cfg.DialTimeout)
	defer cancel()

	tunnel, err := magic.Dial(dialCtx, magic.TunnelConfig{
		ControlHost: s.cfg.ControlHost,
		ControlPort: s.cfg.ControlPort,
		MagicUUID:   s.magicUUID,
		TargetPort:  s.cfg.TargetPort,
		SessionName: magic.NewSessionName(),
		DeviceToken: s.cfg.DeviceToken,
		Dial:        s.cfg.Dial,
	})
	if err != nil {
		s.logf("relay open failed for %s: %v", peer, err)
		return
	}
	defer tunnel.Close()

	r := tunnel.Response
	s.logf("relay open for %s: num=%d streamHost=%s targetPort=%d mode=%d",
		peer, r.ConnectionNumber, r.StreamHost, r.TargetPort, r.Mode)

	pipe(downstream, tunnel)
	s.logf("session for %s closed", peer)
}

// pipe copies bytes in both directions and returns when either direction ends,
// then unblocks the other by closing both conns (via the callers' defers, plus
// an explicit close here to break a blocked Read).
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
	// One direction ended; force the other closed so its Copy returns and the
	// goroutine does not leak.
	a.Close()
	b.Close()
	<-done
}
