package fivegencare

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

type DialFunc func(context.Context, string) (net.Conn, error)

// DefaultExchangeTimeout bounds one request/response exchange when neither
// Client.Timeout nor the caller's context supplies a deadline.
const DefaultExchangeTimeout = 10 * time.Second

type Client struct {
	Host string
	Port int
	// Timeout bounds each exchange end to end: connecting, writing the command
	// and reading the response line. Zero selects DefaultExchangeTimeout.
	Timeout time.Duration
	Dial    DialFunc
	// Logger receives protocol diagnostics at debug level. Credentials are never
	// logged; responses are summarised to their first two fields. Nil disables
	// diagnostics.
	Logger *slog.Logger
}

type OTPChallenge struct {
	UserID int64  `json:"user_id"`
	Domain string `json:"domain"`
}

var ErrSessionRejected = errors.New("5GenCare session rejected")

// SessionRejectedError identifies an authorization failure without exposing
// session credentials or the raw server response.
type SessionRejectedError struct {
	Status string
}

func (e *SessionRejectedError) Error() string {
	return fmt.Sprintf("server rejected session (status %q)", e.Status)
}

func (e *SessionRejectedError) Unwrap() error { return ErrSessionRejected }

func (c Client) RequestOTP(ctx context.Context, deviceUUID, email string) (OTPChallenge, error) {
	wire, err := OTPCommand(deviceUUID, email)
	if err != nil {
		return OTPChallenge{}, err
	}
	host := c.host()
	for redirects := 0; redirects < 3; redirects++ {
		line, err := c.exchange(ctx, host, wire)
		if err != nil {
			return OTPChallenge{}, err
		}
		if next, ok := ParseRedirect(line, "v3_otp"); ok {
			host = next
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "v3_otp" {
			return OTPChallenge{}, errors.New("unexpected OTP response")
		}
		uid, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil || uid <= 0 {
			return OTPChallenge{}, fmt.Errorf("OTP request failed with status %q", f[1])
		}
		return OTPChallenge{UserID: uid, Domain: host}, nil
	}
	return OTPChallenge{}, errors.New("too many OTP server redirects")
}

func (c Client) LoginOTP(ctx context.Context, challenge OTPChallenge, deviceUUID, email, code string) (Session, error) {
	wire, err := LoginSetCommand(challenge.UserID, deviceUUID, email, code)
	if err != nil {
		return Session{}, err
	}
	host := challenge.Domain
	if host == "" {
		host = c.host()
	}
	for redirects := 0; redirects < 3; redirects++ {
		line, err := c.exchange(ctx, host, wire)
		if err != nil {
			return Session{}, err
		}
		if next, ok := ParseRedirect(line, "v3_loginset"); ok {
			host = next
			continue
		}
		session, err := ParseLogin(line)
		if err != nil {
			return Session{}, err
		}
		if session.Domain == "" {
			session.Domain = host
		}
		return session, nil
	}
	return Session{}, errors.New("too many login server redirects")
}

// Devices restores a persisted session, performs the required secret
// negotiation and requests v3_dlist over one TLS connection.
func (c Client) Devices(ctx context.Context, session Session) ([]Device, error) {
	var lastErr error
	candidates := sessionTokenCandidates(session)
	c.debugf("restore: domain=%q user_id=%d session_id_present=%t token_candidates=%d", session.Domain, session.UserID, session.SessionID != "", len(candidates))
	for i, token := range candidates {
		candidate := session
		candidate.SessionToken = token
		c.debugf("restore: trying token candidate %d/%d", i+1, len(candidates))
		devices, err := c.devicesWithToken(ctx, candidate)
		if err == nil {
			c.debugf("restore: device list returned %d device(s)", len(devices))
			return devices, nil
		}
		c.debugf("restore: candidate %d failed: %v", i+1, err)
		lastErr = err
		// Only an explicit rejection says anything about this token. A transport
		// failure would fail identically for every candidate, so retrying it just
		// doubles the wait on an unreachable host.
		if !errors.Is(err, ErrSessionRejected) {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("session has no token")
	}
	return nil, lastErr
}

func sessionTokenCandidates(session Session) []string {
	result := make([]string, 0, 2)
	if session.SessionToken != "" {
		result = append(result, session.SessionToken)
	}
	if session.MasterToken != "" && session.MasterToken != session.SessionToken {
		result = append(result, session.MasterToken)
	}
	return result
}

func (c Client) devicesWithToken(ctx context.Context, session Session) ([]Device, error) {
	host := session.Domain
	if host == "" {
		host = c.host()
	}
	sessionWire, err := SessionCommand(session)
	if err != nil {
		return nil, err
	}
	for redirects := 0; redirects < 3; redirects++ {
		conn, err := c.dial(ctx, host)
		if err != nil {
			return nil, err
		}
		reader := bufio.NewReader(conn)
		line, err := c.writeRead(ctx, conn, reader, sessionWire)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("restore session: %w", err)
		}
		c.debugf("restore: host=%q response=%s", host, responseSummary(line))
		if next, ok := ParseRedirect(line, "v3_session"); ok {
			c.debugf("restore: redirecting to host=%q", next)
			conn.Close()
			host = next
			continue
		}
		if err := validateSessionResponse(line); err != nil {
			conn.Close()
			return nil, fmt.Errorf("restore session: %w", err)
		}

		secret, err := randomSecret()
		if err != nil {
			conn.Close()
			return nil, err
		}
		secretWire, _ := SecretCommand(secret)
		line, err = c.writeRead(ctx, conn, reader, secretWire)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("negotiate secret: %w", err)
		}
		c.debugf("secret: response=%s", responseSummary(line))
		if _, err := ParseSecret(line); err != nil {
			conn.Close()
			return nil, err
		}

		line, err = c.writeRead(ctx, conn, reader, []byte("v3_dlist\n"))
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("request device list: %w", err)
		}
		c.debugf("dlist: response=%s", responseSummary(line))
		devices, err := ParseDeviceList(line)
		conn.Close()
		if err != nil {
			return nil, err
		}
		return devices, nil
	}
	return nil, errors.New("too many session server redirects")
}

func (c Client) debugf(format string, args ...any) {
	if c.Logger == nil {
		return
	}
	c.Logger.Debug(fmt.Sprintf(format, args...))
}

func responseSummary(line string) string {
	fields := strings.Fields(line)
	if len(fields) > 2 {
		return strings.Join(fields[:2], " ") + " (...fields redacted)"
	}
	return strings.Join(fields, " ")
}

// validateSessionResponse follows SocketImplement::sessionV3Handler: a
// positive status identifies a restored authenticated session. It is normally
// the account id, not the literal success value 1.
func validateSessionResponse(line string) error {
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "v3_session" {
		return errors.New("unexpected response")
	}
	status, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil || status <= 0 {
		return &SessionRejectedError{Status: f[1]}
	}
	return nil
}

func (c Client) exchange(ctx context.Context, host string, wire []byte) (string, error) {
	conn, err := c.dial(ctx, host)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return c.writeRead(ctx, conn, bufio.NewReader(conn), wire)
}

// exchangeDeadline bounds one write/read round trip. The caller's context wins
// when it is stricter, so a shutdown is never delayed by the client timeout.
func (c Client) exchangeDeadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(c.timeout())
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}

func (c Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultExchangeTimeout
}

func (c Client) dial(ctx context.Context, host string) (net.Conn, error) {
	if c.Dial != nil {
		return c.Dial(ctx, host)
	}
	timeout := c.timeout()
	port := c.Port
	if port == 0 {
		port = DefaultPort
	}
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: timeout},
		Config:    &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
	}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("dial 5GenCare host %q: %w", host, err)
	}
	return conn, nil
}

func (c Client) host() string {
	if c.Host != "" {
		return c.Host
	}
	return DefaultHost
}

// writeRead sends one command and reads one response line under a deadline. A
// server that accepts the TLS connection and then goes silent must fail the
// exchange, not block the caller: the dial timeout does not cover the read.
func (c Client) writeRead(ctx context.Context, conn io.Writer, reader *bufio.Reader, wire []byte) (string, error) {
	if deadliner, ok := conn.(interface{ SetDeadline(time.Time) error }); ok {
		if err := deadliner.SetDeadline(c.exchangeDeadline(ctx)); err != nil {
			return "", fmt.Errorf("set exchange deadline: %w", err)
		}
	}
	if _, err := conn.Write(wire); err != nil {
		return "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func randomSecret() (string, error) {
	const alphabet = "AaBbCcDdEeFfGgHhIiJjKkLlMmNnOoPpQqRrSsTtUuVvWwXxYyZz1234567890"
	b := make([]byte, 6)
	for i := range b {
		var sample [1]byte
		for {
			if _, err := rand.Read(sample[:]); err != nil {
				return "", fmt.Errorf("generate secret: %w", err)
			}
			// Rejection avoids modulo bias while matching the app's 62 symbols.
			if sample[0] < 248 {
				b[i] = alphabet[int(sample[0])%len(alphabet)]
				break
			}
		}
	}
	return string(b), nil
}
