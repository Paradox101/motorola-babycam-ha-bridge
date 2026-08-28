// Package bridge exposes the reconstructed Magic WEB2 tunnel as a local TCP
// endpoint. It plays the same role the Android app plays with its dynamic
// listen port (16667 in the measured session): a plain, local RTSP-over-TCP
// socket that any player, go2rtc or Home Assistant can point at, with every
// byte carried transparently to the camera through a magic.Tunnel.
//
// The bridge deliberately performs no 5GenCare control flow. That flow (fresh
// SID, device token, stream access token and relay parameters) is the one part
// of the chain not reconstructable from an x86 host, so its outputs are handed
// to the bridge as Credentials. Everything downstream of those credentials is
// the proven, tested Magic transport.
package bridge

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/magic"
)

// DefaultTargetPort is the camera target port observed in every measured VM65
// live-view session.
const DefaultTargetPort = 6667

// Credentials are the per-camera values the 5GenCare control flow produces.
// The bridge treats them as opaque inputs: it derives the magicUuid from them
// but never fabricates, refreshes or persists them.
type Credentials struct {
	DeviceID    uint32 // numeric device id
	SID         string // camera SID from device discovery
	DeviceToken string // opaque device token; also the tunnel crypto key
	DeviceUDID  string // stable camera identifier for integrations
	DeviceName  string // display name for integrations
	Model       string // device-reported model; informational, never filtered
	ControlHost string // Magic relay control host
	ControlPort int    // defaults to magic.ControlPortDefault when zero
	TargetPort  int    // defaults to DefaultTargetPort when zero
}

func (c Credentials) validate() error {
	switch {
	case c.SID == "":
		return errors.New("credentials: SID is required")
	case c.DeviceToken == "":
		return errors.New("credentials: device token is required")
	case c.ControlHost == "":
		return errors.New("credentials: control host is required")
	}
	return nil
}

// Config configures a Bridge. ListenAddr and Credentials are required; the rest
// have safe defaults.
type Config struct {
	// ListenAddr is the local address the bridge listens on, e.g.
	// "127.0.0.1:8554". Binding to loopback is strongly recommended: the tunnel
	// carries an unauthenticated RTSP stream.
	ListenAddr string

	Credentials Credentials

	// DialTimeout bounds the Magic WEB2 opening handshake for one client. Zero
	// selects a 15s default. It does not limit stream lifetime.
	DialTimeout time.Duration

	// DialRetries is the number of extra attempts to open the relay after the
	// first fails, per client connection. Zero selects a default of 2; a
	// negative value disables retrying.
	DialRetries int

	// DialBackoff is the base wait between relay-open attempts; it doubles each
	// attempt. Zero selects 1s.
	DialBackoff time.Duration

	// Logger receives structured lifecycle logs. Zero uses slog.Default.
	Logger *slog.Logger

	// Dial injects the raw TCP dialer used for the relay connections. Zero uses
	// a net.Dialer. Tests use it to point at an in-process fake relay.
	Dial magic.DialFunc
}

// Bridge accepts local TCP connections and tunnels each one to the camera
// through an independent Magic WEB2 relay session.
type Bridge struct {
	cfg         Config
	magicUUID   string
	dialTimeout time.Duration
	dialRetries int
	dialBackoff time.Duration
	log         *slog.Logger

	listener net.Listener
	sessions int64 // total accepted, atomic
	active   int64 // currently open, atomic

	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	closing bool
}

// New validates cfg and derives the stable magicUuid, but does not bind a
// socket. Call Listen (or Serve) to start accepting.
func New(cfg Config) (*Bridge, error) {
	if cfg.ListenAddr == "" {
		return nil, errors.New("bridge: listen address is required")
	}
	if err := cfg.Credentials.validate(); err != nil {
		return nil, err
	}
	magicUUID, err := magic.GenerateMagicUUID(cfg.Credentials.DeviceID, cfg.Credentials.SID, cfg.Credentials.DeviceToken)
	if err != nil {
		return nil, fmt.Errorf("bridge: derive magic uuid: %w", err)
	}

	dialTimeout := cfg.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 15 * time.Second
	}
	dialRetries := cfg.DialRetries
	if dialRetries == 0 {
		dialRetries = 2
	}
	if dialRetries < 0 {
		dialRetries = 0
	}
	dialBackoff := cfg.DialBackoff
	if dialBackoff == 0 {
		dialBackoff = time.Second
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Bridge{
		cfg:         cfg,
		magicUUID:   magicUUID,
		dialTimeout: dialTimeout,
		dialRetries: dialRetries,
		dialBackoff: dialBackoff,
		log:         log,
		conns:       make(map[net.Conn]struct{}),
	}, nil
}

// Listen binds the configured address so Addr reports the real bound port
// before any connection arrives. Serve calls it implicitly when needed.
func (b *Bridge) Listen() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listener != nil {
		return nil
	}
	listener, err := net.Listen("tcp", b.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("bridge: listen on %s: %w", b.cfg.ListenAddr, err)
	}
	b.listener = listener
	return nil
}

// Addr returns the bound listen address, or nil before Listen/Serve.
func (b *Bridge) Addr() net.Addr {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listener == nil {
		return nil
	}
	return b.listener.Addr()
}

// Serve accepts connections until ctx is cancelled or Close is called. It binds
// the listener if Listen has not already been called. Each accepted connection
// is handled in its own goroutine. Serve returns nil on a clean shutdown.
func (b *Bridge) Serve(ctx context.Context) error {
	if err := b.Listen(); err != nil {
		return err
	}
	b.log.Info("bridge listening",
		"addr", b.Addr().String(),
		"control_host", b.cfg.Credentials.ControlHost,
		"target_port", b.targetPort())

	// Unblock Accept when ctx is cancelled.
	stop := make(chan struct{})
	var once sync.Once
	closeStop := func() { once.Do(func() { close(stop) }) }
	defer closeStop()
	go func() {
		select {
		case <-ctx.Done():
			b.Close()
		case <-stop:
		}
	}()

	var wg sync.WaitGroup
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			closeStop()
			wg.Wait()
			if b.isClosing() || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("bridge: accept: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.handle(ctx, conn)
		}()
	}
}

// Close stops accepting and tears down the listener and all live client
// connections. Tunnels close as their copy loops observe the closed sockets.
func (b *Bridge) Close() error {
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return nil
	}
	b.closing = true
	listener := b.listener
	conns := make([]net.Conn, 0, len(b.conns))
	for c := range b.conns {
		conns = append(conns, c)
	}
	b.mu.Unlock()

	var err error
	if listener != nil {
		err = listener.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	return err
}

// Stats reports lifetime counters: total sessions accepted and currently active.
func (b *Bridge) Stats() (total, active int64) {
	return atomic.LoadInt64(&b.sessions), atomic.LoadInt64(&b.active)
}

func (b *Bridge) handle(ctx context.Context, client net.Conn) {
	id := atomic.AddInt64(&b.sessions, 1)
	atomic.AddInt64(&b.active, 1)
	b.trackConn(client, true)
	log := b.log.With("session", id, "client", client.RemoteAddr().String())
	log.Info("client connected")

	defer func() {
		_ = client.Close()
		b.trackConn(client, false)
		atomic.AddInt64(&b.active, -1)
		log.Info("client disconnected")
	}()

	tunnel, err := b.dialWithRetry(ctx, log)
	if err != nil {
		log.Error("relay dial failed", "err", err)
		return
	}
	defer tunnel.Close()
	log.Info("relay session open",
		"stream_host", tunnel.Response.StreamHost,
		"connection_num", tunnel.Response.ConnectionNumber,
		"mode", tunnel.Response.Mode)

	// When the outer context is cancelled, drop both ends so the copies return.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
			_ = tunnel.Close()
		case <-stop:
		}
	}()

	fromClient, fromRelay := pipe(client, tunnel, log)
	log.Info("relay session closed", "bytes_to_relay", fromClient, "bytes_from_camera", fromRelay)
	// A session that opened but never carried a single camera byte is the exact
	// signature of a relay session without an attached camera peer. In the wild
	// this means the 5GenCare-authorized session is missing or expired; make
	// that legible instead of a silent empty stream. Skipped on context cancel,
	// where the empty read is our own shutdown.
	if fromRelay == 0 && ctx.Err() == nil {
		log.Warn("relay opened but camera sent no data; the camera did not attach. "+
			"This is expected without a valid 5GenCare-authorized session "+
			"(fresh SID / device token / stream accessToken). See docs/bridge.md",
			"bytes_to_relay", fromClient)
	}
}

// dialWithRetry opens a relay session, retrying transient failures with an
// exponential backoff bounded by the outer context. Each attempt gets its own
// dial timeout.
func (b *Bridge) dialWithRetry(ctx context.Context, log *slog.Logger) (*magic.Tunnel, error) {
	backoff := b.dialBackoff
	var lastErr error
	for attempt := 0; attempt <= b.dialRetries; attempt++ {
		if attempt > 0 {
			log.Warn("retrying relay dial", "attempt", attempt, "max", b.dialRetries, "prev_err", lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
		dialCtx, cancel := context.WithTimeout(ctx, b.dialTimeout)
		tunnel, err := magic.Dial(dialCtx, magic.TunnelConfig{
			ControlHost: b.cfg.Credentials.ControlHost,
			ControlPort: b.cfg.Credentials.ControlPort,
			MagicUUID:   b.magicUUID,
			TargetPort:  b.targetPort(),
			SessionName: freshSessionName(),
			DeviceToken: b.cfg.Credentials.DeviceToken,
			Dial:        b.cfg.Dial,
		})
		cancel()
		if err == nil {
			return tunnel, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

// pipe copies bytes in both directions until either side closes, then returns
// the number of bytes carried from the client to the relay and from the relay
// (camera) back to the client.
func pipe(client net.Conn, tunnel *magic.Tunnel, log *slog.Logger) (fromClient, fromRelay int64) {
	var wg sync.WaitGroup
	wg.Add(2)

	// Each goroutine writes only its own counter, exactly once; wg.Wait below
	// establishes the happens-before edge for reading them.
	copyDir := func(dst io.Writer, src io.Reader, dir string, count *int64, closeDst func()) {
		defer wg.Done()
		n, err := io.Copy(dst, src)
		*count = n
		if err != nil && !isExpectedClose(err) {
			log.Debug("copy ended", "dir", dir, "err", err)
		}
		// Closing the destination unblocks the opposite direction's Read.
		closeDst()
	}

	go copyDir(tunnel, client, "client->relay", &fromClient, func() { _ = tunnel.Close() })
	go copyDir(client, tunnel, "relay->client", &fromRelay, func() { _ = client.Close() })
	wg.Wait()
	return fromClient, fromRelay
}

func (b *Bridge) targetPort() int {
	if b.cfg.Credentials.TargetPort != 0 {
		return b.cfg.Credentials.TargetPort
	}
	return DefaultTargetPort
}

func (b *Bridge) trackConn(c net.Conn, add bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if add {
		b.conns[c] = struct{}{}
	} else {
		delete(b.conns, c)
	}
}

func (b *Bridge) isClosing() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closing
}

func isExpectedClose(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF)
}

// freshSessionName returns a canonical 36-char UUID used as the client session
// label in the app-discovery request, one per relay session.
func freshSessionName() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
