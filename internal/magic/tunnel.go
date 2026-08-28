package magic

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// ControlPortDefault is the native default Magic control port (0x2260).
const ControlPortDefault = 8800

var _ net.Conn = (*Tunnel)(nil)

// DialFunc dials a raw TCP connection. It is injectable so callers can supply
// a custom resolver, proxy or test transport.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// TunnelConfig carries the inputs a WEB2 relay session needs. Every field is a
// value the app obtains earlier from the 5GenCare control flow; this package
// does not derive or fabricate them. MagicUUID can be produced with
// GenerateMagicUUID from deviceID/SID/deviceToken.
type TunnelConfig struct {
	ControlHost string   // Magic relay control host
	ControlPort int      // defaults to ControlPortDefault when zero
	MagicUUID   string   // 78-byte identifier from generate_sid_v1
	TargetPort  int      // camera target port (6667 in the measured session)
	SessionName string   // 36-byte session name
	DeviceToken string   // opaque device token; the tunnel crypto key
	Dial        DialFunc // defaults to a net.Dialer when nil
}

// Tunnel is a byte-transparent WEB2 relay connection. After Dial it implements
// net.Conn: Write encodes plaintext toward the relay and Read decodes relay
// bytes back, so an RTSP client can speak to it as if it were the camera.
type Tunnel struct {
	// Response is the parsed control-host answer that selected this session.
	Response AppResponse

	control net.Conn
	stream  net.Conn
	encoder *TokenEncoder
	decoder *TokenDecoder

	readBuf bytes.Buffer
	rawBuf  []byte
}

// Dial performs the proven WEB2 opening sequence:
//
//  1. connect to ControlHost:ControlPort and exchange the `app` discovery frame;
//  2. connect to the response stream host on RelayStreamPort and send RelayOpen;
//  3. initialise the per-direction device-token crypto.
//
// It does not read or play any media; it only establishes the transparent byte
// channel. The control connection is kept open for the tunnel's lifetime, since
// relay keepalive/close behaviour on the control socket is not yet confirmed.
func Dial(ctx context.Context, cfg TunnelConfig) (*Tunnel, error) {
	if cfg.DeviceToken == "" {
		return nil, errors.New("device token is required")
	}
	controlPort := cfg.ControlPort
	if controlPort == 0 {
		controlPort = ControlPortDefault
	}
	dial := cfg.Dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}

	request := AppRequest{
		MagicUUID:   cfg.MagicUUID,
		TargetPort:  cfg.TargetPort,
		Mode:        ConnectionModeWEB2,
		SessionName: cfg.SessionName,
	}
	requestBytes, err := request.MarshalText()
	if err != nil {
		return nil, fmt.Errorf("build app request: %w", err)
	}

	control, err := dial(ctx, "tcp", net.JoinHostPort(cfg.ControlHost, strconv.Itoa(controlPort)))
	if err != nil {
		return nil, fmt.Errorf("dial control host: %w", err)
	}
	tunnel := &Tunnel{control: control, rawBuf: make([]byte, 32*1024)}

	if _, err := control.Write(requestBytes); err != nil {
		return nil, tunnel.fail("send app request", err)
	}
	line, err := bufio.NewReader(control).ReadBytes('\n')
	if err != nil {
		return nil, tunnel.fail("read app response", err)
	}
	response, err := ParseAppResponse(line)
	if err != nil {
		return nil, tunnel.fail("parse app response", err)
	}
	tunnel.Response = response
	if response.Mode != ConnectionModeWEB2 {
		return nil, tunnel.fail("relay selected non-WEB2 mode",
			fmt.Errorf("mode %d is not reconstructed", response.Mode))
	}

	streamAddr := net.JoinHostPort(response.StreamHost, strconv.Itoa(RelayStreamPort))
	stream, err := dial(ctx, "tcp", streamAddr)
	if err != nil {
		return nil, tunnel.fail("dial stream host", err)
	}
	tunnel.stream = stream

	open := RelayOpen{
		Version:          RelayOpenVersion2,
		ConnectionNumber: response.ConnectionNumber,
		TargetPort:       response.TargetPort,
		MagicUUID:        cfg.MagicUUID,
		SessionName:      cfg.SessionName,
	}
	openBytes, err := open.MarshalText()
	if err != nil {
		return nil, tunnel.fail("build relay-open frame", err)
	}
	if _, err := stream.Write(openBytes); err != nil {
		return nil, tunnel.fail("send relay-open frame", err)
	}

	if tunnel.encoder, err = NewTokenEncoder(cfg.DeviceToken); err != nil {
		return nil, tunnel.fail("init token encoder", err)
	}
	if tunnel.decoder, err = NewTokenDecoder(cfg.DeviceToken); err != nil {
		return nil, tunnel.fail("init token decoder", err)
	}
	return tunnel, nil
}

func (t *Tunnel) fail(context string, err error) error {
	t.Close()
	return fmt.Errorf("%s: %w", context, err)
}

// Read returns decoded plaintext relay bytes. It transparently absorbs the
// one-time crypto bootstrap, which the native receiver allows to span reads.
func (t *Tunnel) Read(p []byte) (int, error) {
	if t.readBuf.Len() > 0 {
		return t.readBuf.Read(p)
	}
	for {
		n, err := t.stream.Read(t.rawBuf)
		if n > 0 {
			plain, decodeErr := t.decoder.Decode(t.rawBuf[:n])
			if decodeErr != nil {
				return 0, decodeErr
			}
			if len(plain) > 0 {
				t.readBuf.Write(plain)
				return t.readBuf.Read(p)
			}
			// Bytes were consumed into the bootstrap; keep reading.
		}
		if err != nil {
			return 0, err
		}
	}
}

// Write encodes p toward the relay. The first write also emits the crypto
// bootstrap prefix. It reports len(p) written on success so it behaves as an
// io.Writer over the plaintext stream.
func (t *Tunnel) Write(p []byte) (int, error) {
	cipher, err := t.encoder.Encode(p)
	if err != nil {
		return 0, err
	}
	if _, err := t.stream.Write(cipher); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close closes both the stream and control connections.
func (t *Tunnel) Close() error {
	var streamErr, controlErr error
	if t.stream != nil {
		streamErr = t.stream.Close()
	}
	if t.control != nil {
		controlErr = t.control.Close()
	}
	if streamErr != nil {
		return streamErr
	}
	return controlErr
}

// LocalAddr reports the stream connection's local address.
func (t *Tunnel) LocalAddr() net.Addr { return t.stream.LocalAddr() }

// RemoteAddr reports the stream (relay) connection's remote address.
func (t *Tunnel) RemoteAddr() net.Addr { return t.stream.RemoteAddr() }

// SetDeadline sets read and write deadlines on the stream connection.
func (t *Tunnel) SetDeadline(deadline time.Time) error { return t.stream.SetDeadline(deadline) }

// SetReadDeadline sets the read deadline on the stream connection.
func (t *Tunnel) SetReadDeadline(deadline time.Time) error {
	return t.stream.SetReadDeadline(deadline)
}

// SetWriteDeadline sets the write deadline on the stream connection.
func (t *Tunnel) SetWriteDeadline(deadline time.Time) error {
	return t.stream.SetWriteDeadline(deadline)
}
