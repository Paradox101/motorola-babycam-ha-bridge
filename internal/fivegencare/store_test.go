package fivegencare

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStoreSaveUsesPrivatePermissionsAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	store := NewStore(path)
	want := State{DeviceUUID: "uuid", Session: &Session{UserID: 7, SessionToken: "secret", SessionID: "sid"}}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("permissions = %o, want 600", got)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceUUID != want.DeviceUUID || got.Session == nil || got.Session.SessionToken != "secret" {
		t.Fatalf("round trip = %#v", got)
	}
}

func TestStoreFailedRenamePreservesPreviousState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path)
	if err := store.Save(State{DeviceUUID: "old"}); err != nil {
		t.Fatal(err)
	}
	store.rename = func(string, string) error { return errors.New("injected rename failure") }
	if err := store.Save(State{DeviceUUID: "new"}); err == nil {
		t.Fatal("expected rename failure")
	}
	got, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceUUID != "old" {
		t.Fatalf("state after failed save = %q, want old", got.DeviceUUID)
	}
}
