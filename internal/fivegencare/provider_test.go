package fivegencare

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type fakeAccountClient struct {
	challenge     OTPChallenge
	session       Session
	devices       []Device
	devicesErr    error
	requestedOTPs int
}

func (f *fakeAccountClient) RequestOTP(context.Context, string, string) (OTPChallenge, error) {
	f.requestedOTPs++
	return f.challenge, nil
}

func (f *fakeAccountClient) LoginOTP(context.Context, OTPChallenge, string, string, string) (Session, error) {
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
