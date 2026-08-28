package fivegencare

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrPairingRequired = errors.New("pairing required")

// ErrNoChallenge means a code was submitted before one was requested.
var ErrNoChallenge = errors.New("no code has been requested yet")

// ChallengeTTL is how long a requested email code stays usable here. Motorola
// expires the code on its side too; the point of expiring it locally is that a
// stored challenge the server will never accept again must not be retried
// forever. Before this existed, a code that expired before it was entered left
// the add-on asking for that same dead code on every start, and the only way
// out was deleting the state file.
const ChallengeTTL = 15 * time.Minute

type accountClient interface {
	RequestOTP(context.Context, string, string) (OTPChallenge, error)
	LoginOTP(context.Context, OTPChallenge, string, string, string) (Session, error)
	Devices(context.Context, Session) ([]Device, error)
}

type ProviderConfig struct {
	Client    accountClient
	Store     *Store
	Email     string
	OTPCode   string
	RelayHost string

	// AutoRequestCode lets a restore send a code by itself when none is
	// pending. The Web UI turns this off: a code should arrive because someone
	// asked for one, not because the add-on restarted.
	AutoRequestCode bool
	// Now is injectable so tests do not wait for a challenge to age.
	Now func() time.Time
}

// CameraCredentials contains all values needed to expose one compatible
// Motorola Nursery camera. Model is descriptive and never used as a filter.
type CameraCredentials struct {
	DeviceID      uint32 `json:"device_id"`
	DeviceUDID    string `json:"device_udid"`
	DeviceName    string `json:"device_name"`
	Model         string `json:"model"`
	SID           string `json:"sid"`
	DeviceToken   string `json:"device_token"`
	ControlHost   string `json:"control_host"`
	ControlPort   int    `json:"control_port"`
	TargetPort    int    `json:"target_port"`
	DeviceAPIHost string `json:"device_api_host"`
	DeviceAPIPort int    `json:"device_api_port"`
	AccessToken   string `json:"access_token"`
	RTSPUser      string `json:"rtsp_user"`
	RTSPPass      string `json:"rtsp_password"`
}

type Provider struct {
	cfg      ProviderConfig
	mu       sync.Mutex
	lastGood []CameraCredentials
}

func NewProvider(cfg ProviderConfig) *Provider {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Provider{cfg: cfg}
}

// PairingStatus is what the Web UI renders. It carries no secret: the code
// itself is never stored, and the session token never leaves this package.
type PairingStatus struct {
	// Paired means a session exists and cameras can be fetched.
	Paired bool `json:"paired"`
	// AwaitingCode means a code was sent and is still usable.
	AwaitingCode bool `json:"awaiting_code"`
	// Email is the address the last code went to, for display.
	Email string `json:"email,omitempty"`
	// CodeExpiresInSeconds counts down the pending challenge.
	CodeExpiresInSeconds int `json:"code_expires_in_seconds,omitempty"`
}

// Status reports where pairing stands, for the Web UI.
func (p *Provider) Status() (PairingStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, err := p.cfg.Store.Load()
	if err != nil {
		return PairingStatus{}, err
	}
	status := PairingStatus{Paired: state.Session != nil, Email: p.email(state)}
	if remaining, live := p.challengeRemaining(state); live {
		status.AwaitingCode = true
		status.CodeExpiresInSeconds = int(remaining.Seconds())
	}
	return status, nil
}

// RequestCode sends a fresh email code, replacing any pending one. It is the
// action behind the Web UI's send button, and it always requests: a user who
// pressed it is telling us the previous code did not arrive or no longer works.
func (p *Provider) RequestCode(ctx context.Context, email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("an email address is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state, err := p.cfg.Store.Load()
	if err != nil {
		return err
	}
	if state.DeviceUUID == "" {
		if state.DeviceUUID, err = newDeviceUUID(); err != nil {
			return err
		}
	}
	challenge, err := p.cfg.Client.RequestOTP(ctx, state.DeviceUUID, email)
	if err != nil {
		return fmt.Errorf("request email code: %w", err)
	}
	state.Challenge = &challenge
	state.ChallengeAt = p.cfg.Now()
	state.Email = email
	return p.cfg.Store.Save(state)
}

// SubmitCode verifies a code against the pending challenge and stores the
// session. A wrong code keeps the challenge, so a typo costs a retry rather
// than a new email.
func (p *Provider) SubmitCode(ctx context.Context, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("the code from the email is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state, err := p.cfg.Store.Load()
	if err != nil {
		return err
	}
	if _, live := p.challengeRemaining(state); !live {
		return ErrNoChallenge
	}
	session, err := p.cfg.Client.LoginOTP(ctx, *state.Challenge, state.DeviceUUID, p.email(state), code)
	if err != nil {
		return fmt.Errorf("verify email code: %w", err)
	}
	state.Session = &session
	state.Challenge = nil
	state.ChallengeAt = time.Time{}
	return p.cfg.Store.Save(state)
}

// challengeRemaining reports how long a stored challenge is still worth trying.
func (p *Provider) challengeRemaining(state State) (time.Duration, bool) {
	if state.Challenge == nil {
		return 0, false
	}
	if state.ChallengeAt.IsZero() {
		// Written before challenges were dated. Treat it as usable once more
		// rather than discarding a code the user may be holding.
		return ChallengeTTL, true
	}
	remaining := ChallengeTTL - p.cfg.Now().Sub(state.ChallengeAt)
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}

// email prefers the configured address and falls back to the one pairing last
// used, so a cleared option does not lose the account.
func (p *Provider) email(state State) string {
	if p.cfg.Email != "" {
		return p.cfg.Email
	}
	return state.Email
}

func (p *Provider) Restore(ctx context.Context) ([]CameraCredentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.restore(ctx)
}

func (p *Provider) restore(ctx context.Context) ([]CameraCredentials, error) {
	if p.cfg.Client == nil {
		return nil, errors.New("account client is required")
	}
	if p.cfg.Store == nil || p.cfg.Store.Path == "" {
		return nil, errors.New("account state store is required")
	}
	state, err := p.cfg.Store.Load()
	if err != nil {
		return nil, err
	}
	if state.DeviceUUID == "" {
		state.DeviceUUID, err = newDeviceUUID()
		if err != nil {
			return nil, err
		}
	}
	if state.Session == nil {
		if err := p.pair(ctx, &state); err != nil {
			return nil, err
		}
	}

	devices, err := p.cfg.Client.Devices(ctx, *state.Session)
	if err != nil {
		if !errors.Is(err, ErrSessionRejected) {
			return nil, fmt.Errorf("restore account session: %w", err)
		}
		// The stored session is gone and its challenge with it, so pairing
		// always needs a fresh email code: pair reports what the user has to do
		// and that instruction is the useful error here, not the rejection.
		state.Session = nil
		state.Challenge = nil
		state.ChallengeAt = time.Time{}
		if pairErr := p.pair(ctx, &state); pairErr != nil {
			return nil, pairErr
		}
		return nil, fmt.Errorf("restore account session: %w", err)
	}

	deviceAPIHost := state.Session.Domain
	if deviceAPIHost == "" {
		deviceAPIHost = DefaultHost
	}
	cameras := make([]CameraCredentials, 0, len(devices))
	for _, device := range devices {
		if device.ID == 0 || device.UDID == "" || device.SID == "" || device.DeviceToken == "" {
			continue
		}
		cameras = append(cameras, CameraCredentials{
			DeviceID:      device.ID,
			DeviceUDID:    device.UDID,
			DeviceName:    device.Name,
			Model:         device.Model,
			SID:           device.SID,
			DeviceToken:   device.DeviceToken,
			ControlHost:   p.cfg.RelayHost,
			ControlPort:   8800,
			TargetPort:    TargetPort,
			DeviceAPIHost: deviceAPIHost,
			DeviceAPIPort: DeviceAPIPort,
			AccessToken:   OwnerAccessToken(device.DeviceToken),
			RTSPUser:      "Pascal",
			RTSPPass:      "5GenCare.com",
		})
	}
	sort.Slice(cameras, func(i, j int) bool { return cameras[i].DeviceUDID < cameras[j].DeviceUDID })
	if len(cameras) == 0 {
		return nil, errors.New("account has no compatible Motorola Nursery camera")
	}
	p.lastGood = append(p.lastGood[:0], cameras...)
	return append([]CameraCredentials(nil), cameras...), nil
}

type RefreshReason string

const (
	RefreshAfterTransportFailure RefreshReason = "transport_failure"
	RefreshAfterAuthorization    RefreshReason = "authorization_failure"
)

// Refresh obtains a current device list. On failure it returns a copy of the
// last known good credentials together with the sanitized refresh error.
func (p *Provider) Refresh(ctx context.Context, reason RefreshReason) ([]CameraCredentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cameras, err := p.restore(ctx)
	if err == nil {
		return cameras, nil
	}
	lastGood := append([]CameraCredentials(nil), p.lastGood...)
	return lastGood, fmt.Errorf("refresh after %s: %w", reason, err)
}

func (p *Provider) pair(ctx context.Context, state *State) error {
	email := p.email(*state)
	if email == "" {
		if err := p.cfg.Store.Save(*state); err != nil {
			return err
		}
		return fmt.Errorf("%w: open the add-on Web UI, or set the email option", ErrPairingRequired)
	}
	if _, live := p.challengeRemaining(*state); !live {
		// Drop a challenge that has aged out before deciding what to do, so a
		// dead code is never the thing that is retried.
		state.Challenge = nil
		state.ChallengeAt = time.Time{}
		if !p.cfg.AutoRequestCode {
			if err := p.cfg.Store.Save(*state); err != nil {
				return err
			}
			return fmt.Errorf("%w: open the add-on Web UI to receive a code", ErrPairingRequired)
		}
		challenge, err := p.cfg.Client.RequestOTP(ctx, state.DeviceUUID, email)
		if err != nil {
			return fmt.Errorf("request email code: %w", err)
		}
		state.Challenge = &challenge
		state.ChallengeAt = p.cfg.Now()
		state.Email = email
		if err := p.cfg.Store.Save(*state); err != nil {
			return err
		}
		return fmt.Errorf("%w: code sent by email; enter it in the add-on Web UI", ErrPairingRequired)
	}
	if p.cfg.OTPCode == "" {
		return fmt.Errorf("%w: enter the code from your email in the add-on Web UI", ErrPairingRequired)
	}
	session, err := p.cfg.Client.LoginOTP(ctx, *state.Challenge, state.DeviceUUID, email, p.cfg.OTPCode)
	if err != nil {
		return fmt.Errorf("verify email code: %w", err)
	}
	state.Session = &session
	state.Challenge = nil
	state.ChallengeAt = time.Time{}
	state.Email = email
	return p.cfg.Store.Save(*state)
}

func newDeviceUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate device UUID: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(bytes[:])
	return hexValue[0:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:32], nil
}
