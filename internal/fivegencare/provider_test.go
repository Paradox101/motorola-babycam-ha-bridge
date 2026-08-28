package fivegencare

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type fakeAccountClient struct {
	challenge     OTPChallenge
	session       Session
	devices       []Device
	devicesErr    error
	requestedOTPs int
	loginErr      error
	lastEmail     string
	lastCode      string
}

func (f *fakeAccountClient) RequestOTP(_ context.Context, _, email string) (OTPChallenge, error) {
	f.requestedOTPs++
	f.lastEmail = email
	return f.challenge, nil
}

func (f *fakeAccountClient) LoginOTP(_ context.Context, _ OTPChallenge, _, email, code string) (Session, error) {
	f.lastEmail = email
	f.lastCode = code
	if f.loginErr != nil {
		return Session{}, f.loginErr
	}
	return f.session, nil
}

func (f *fakeAccountClient) Devices(context.Context, Session) ([]Device, error) {
	return f.devices, f.devicesErr
}

func TestProviderReturnsEveryEligibleCameraInStableOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	if err := store.Save(State{
		DeviceUUID: "device-uuid",
		Session:    &Session{UserID: 42, SessionToken: "token", SessionID: "session", Domain: "shard.example"},
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeAccountClient{devices: []Device{
		{ID: 2, UDID: "z-camera", Model: "MBP99", Name: "Nursery", DeviceToken: "token-z", SID: "sid-z"},
		{ID: 3, UDID: "invalid", Model: "VM65", Name: "Incomplete", SID: "sid"},
		{ID: 1, UDID: "a-camera", Model: "VM65CONNECT", Name: "Baby Room", DeviceToken: "token-a", SID: "sid-a"},
	}}
	provider := NewProvider(ProviderConfig{Client: client, Store: store, RelayHost: "relay.example"})

	cameras, err := provider.Restore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cameras) != 2 {
		t.Fatalf("got %d eligible cameras, want 2", len(cameras))
	}
	if cameras[0].DeviceUDID != "a-camera" || cameras[1].DeviceUDID != "z-camera" {
		t.Fatalf("camera order = %#v", cameras)
	}
	if cameras[1].Model != "MBP99" {
		t.Fatalf("non-VM65 model was not preserved: %#v", cameras[1])
	}
	if cameras[0].DeviceAPIHost != "shard.example" || cameras[0].DeviceAPIPort != 2288 {
		t.Fatalf("device API endpoint = %s:%d", cameras[0].DeviceAPIHost, cameras[0].DeviceAPIPort)
	}
}

func TestProviderRequestsNewOTPAfterRejectedPersistedSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	if err := store.Save(State{
		DeviceUUID: "device-uuid",
		Session:    &Session{UserID: 42, SessionToken: "expired", SessionID: "session"},
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeAccountClient{
		challenge:  OTPChallenge{UserID: 42, Domain: "shard.example"},
		devicesErr: &SessionRejectedError{Status: "-9"},
	}
	provider := NewProvider(ProviderConfig{
		Client: client,
		Store:  store,
		Email:  "owner@example.test",
		// The command-line path still sends a code by itself; the Web UI path
		// leaves that to the person pressing the button.
		AutoRequestCode: true,
	})

	_, err := provider.Restore(context.Background())
	if !errors.Is(err, ErrPairingRequired) {
		t.Fatalf("error = %v, want pairing required", err)
	}
	if client.requestedOTPs != 1 {
		t.Fatalf("requested OTPs = %d, want 1", client.requestedOTPs)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Session != nil || state.Challenge == nil {
		t.Fatalf("persisted state = %#v", state)
	}
}

func TestProviderRefreshReturnsLastKnownGoodCredentialsOnTransientFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	if err := store.Save(State{
		DeviceUUID: "device-uuid",
		Session:    &Session{UserID: 42, SessionToken: "token", SessionID: "session"},
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeAccountClient{devices: []Device{
		{ID: 1, UDID: "camera", Model: "MBP99", DeviceToken: "token", SID: "sid"},
	}}
	provider := NewProvider(ProviderConfig{Client: client, Store: store, RelayHost: "relay.example"})
	if _, err := provider.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.devices = nil
	client.devicesErr = errors.New("temporary network failure")

	cameras, err := provider.Refresh(context.Background(), RefreshAfterTransportFailure)
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if len(cameras) != 1 || cameras[0].DeviceUDID != "camera" {
		t.Fatalf("last known good cameras = %#v", cameras)
	}
}

// The Web UI drives pairing with these three calls; none of them needs a
// restart or an add-on option.
func TestRequestAndSubmitCodePairWithoutARestart(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	client := &fakeAccountClient{
		challenge: OTPChallenge{UserID: 42, Domain: "shard.example"},
		session:   Session{UserID: 42, SessionToken: "token", SessionID: "session"},
	}
	provider := NewProvider(ProviderConfig{Client: client, Store: store})

	status, err := provider.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Paired || status.AwaitingCode {
		t.Fatalf("initial status = %#v", status)
	}

	if err := provider.RequestCode(context.Background(), " owner@example.test "); err != nil {
		t.Fatalf("RequestCode: %v", err)
	}
	if client.lastEmail != "owner@example.test" {
		t.Fatalf("email = %q, want it trimmed", client.lastEmail)
	}
	status, err = provider.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Paired || !status.AwaitingCode || status.Email != "owner@example.test" {
		t.Fatalf("status after request = %#v", status)
	}
	if status.CodeExpiresInSeconds <= 0 {
		t.Fatalf("code expiry = %d, want a countdown", status.CodeExpiresInSeconds)
	}

	if err := provider.SubmitCode(context.Background(), " 123456 "); err != nil {
		t.Fatalf("SubmitCode: %v", err)
	}
	if client.lastCode != "123456" {
		t.Fatalf("code = %q, want it trimmed", client.lastCode)
	}
	status, err = provider.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Paired || status.AwaitingCode {
		t.Fatalf("status after submit = %#v", status)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Session == nil || state.Challenge != nil || state.Email != "owner@example.test" {
		t.Fatalf("persisted state = %#v", state)
	}
}

// A typo must cost a retry, not a new email.
func TestAWrongCodeKeepsTheChallenge(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	client := &fakeAccountClient{challenge: OTPChallenge{UserID: 42}, loginErr: errors.New("bad code")}
	provider := NewProvider(ProviderConfig{Client: client, Store: store})
	if err := provider.RequestCode(context.Background(), "owner@example.test"); err != nil {
		t.Fatal(err)
	}
	if err := provider.SubmitCode(context.Background(), "000000"); err == nil {
		t.Fatal("expected a rejected code to error")
	}
	status, err := provider.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.AwaitingCode {
		t.Fatalf("status = %#v, want the challenge kept for another try", status)
	}
}

// Before challenges expired, a code that aged out left the add-on retrying that
// same dead code on every start, with no way out but deleting the state file.
func TestAnExpiredChallengeIsReplacedInsteadOfRetried(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	client := &fakeAccountClient{challenge: OTPChallenge{UserID: 42}}
	now := time.Now()
	provider := NewProvider(ProviderConfig{
		Client: client, Store: store,
		Now: func() time.Time { return now },
	})
	if err := provider.RequestCode(context.Background(), "owner@example.test"); err != nil {
		t.Fatal(err)
	}

	now = now.Add(ChallengeTTL + time.Minute)
	status, err := provider.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.AwaitingCode {
		t.Fatal("an expired code must not be reported as usable")
	}
	if err := provider.SubmitCode(context.Background(), "123456"); !errors.Is(err, ErrNoChallenge) {
		t.Fatalf("error = %v, want ErrNoChallenge", err)
	}

	// The command-line path replaces it with a fresh one rather than looping.
	autoProvider := NewProvider(ProviderConfig{
		Client: client, Store: store, Email: "owner@example.test",
		AutoRequestCode: true,
		Now:             func() time.Time { return now },
	})
	if _, err := autoProvider.Restore(context.Background()); !errors.Is(err, ErrPairingRequired) {
		t.Fatalf("error = %v, want pairing required", err)
	}
	if client.requestedOTPs != 2 {
		t.Fatalf("requested OTPs = %d, want a replacement code", client.requestedOTPs)
	}
}

// Restore must not send mail on its own when the Web UI owns the flow.
func TestRestoreDoesNotSendACodeWhenTheWebUIOwnsPairing(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	client := &fakeAccountClient{challenge: OTPChallenge{UserID: 42}}
	provider := NewProvider(ProviderConfig{Client: client, Store: store, Email: "owner@example.test"})
	_, err := provider.Restore(context.Background())
	if !errors.Is(err, ErrPairingRequired) {
		t.Fatalf("error = %v, want pairing required", err)
	}
	if client.requestedOTPs != 0 {
		t.Fatalf("requested OTPs = %d, want none until someone asks", client.requestedOTPs)
	}
}

// A cleared email option must not lose the account the add-on paired with.
func TestTheRememberedEmailSurvivesAClearedOption(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	client := &fakeAccountClient{challenge: OTPChallenge{UserID: 42}, session: Session{UserID: 42, SessionToken: "t"}}
	provider := NewProvider(ProviderConfig{Client: client, Store: store})
	if err := provider.RequestCode(context.Background(), "owner@example.test"); err != nil {
		t.Fatal(err)
	}
	if err := provider.SubmitCode(context.Background(), "123456"); err != nil {
		t.Fatal(err)
	}
	status, err := provider.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Email != "owner@example.test" {
		t.Fatalf("email = %q, want the one pairing used", status.Email)
	}
}
