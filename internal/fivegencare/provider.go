package fivegencare

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var ErrPairingRequired = errors.New("pairing required")

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

func NewProvider(cfg ProviderConfig) *Provider { return &Provider{cfg: cfg} }

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
	if p.cfg.Email == "" {
		if err := p.cfg.Store.Save(*state); err != nil {
			return err
		}
		return fmt.Errorf("%w: set the email option", ErrPairingRequired)
	}
	if state.Challenge == nil {
		challenge, err := p.cfg.Client.RequestOTP(ctx, state.DeviceUUID, p.cfg.Email)
		if err != nil {
			return fmt.Errorf("request email code: %w", err)
		}
		state.Challenge = &challenge
		if err := p.cfg.Store.Save(*state); err != nil {
			return err
		}
		return fmt.Errorf("%w: code sent by email; set otp_code and restart", ErrPairingRequired)
	}
	if p.cfg.OTPCode == "" {
		return fmt.Errorf("%w: set otp_code to the code from your email", ErrPairingRequired)
	}
	session, err := p.cfg.Client.LoginOTP(ctx, *state.Challenge, state.DeviceUUID, p.cfg.Email, p.cfg.OTPCode)
	if err != nil {
		return fmt.Errorf("verify email code: %w", err)
	}
	state.Session = &session
	state.Challenge = nil
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
