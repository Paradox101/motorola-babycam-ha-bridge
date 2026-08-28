package fivegencare

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State is the persisted account pairing/session state.
type State struct {
	DeviceUUID string        `json:"device_uuid"`
	Challenge  *OTPChallenge `json:"challenge,omitempty"`
	Session    *Session      `json:"session,omitempty"`

	// Email is the account the last code was requested for. Remembering it
	// means a re-pair after a rejected session does not depend on the add-on
	// option still holding the same address.
	Email string `json:"email,omitempty"`
	// ChallengeAt dates the stored challenge. A code the user did not get to
	// in time has to be replaced rather than retried forever: without this the
	// only way out of an expired challenge was deleting this file.
	ChallengeAt time.Time `json:"challenge_at,omitempty"`
}

// Store atomically persists account state in one private JSON file.
type Store struct {
	Path   string
	rename func(string, string) error
}

func NewStore(path string) *Store {
	return &Store{Path: path, rename: os.Rename}
}

func (s *Store) Load() (State, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read account state: %w", err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}, fmt.Errorf("parse account state: %w", err)
	}
	return state, nil
}

func (s *Store) Save(state State) error {
	return writePrivateJSON(s.Path, state, s.rename)
}

// WritePrivateJSON atomically writes JSON with owner-only permissions on
// Unix-like systems (the supported runtime environment).
func WritePrivateJSON(path string, value any) error {
	return writePrivateJSON(path, value, os.Rename)
}

func writePrivateJSON(path string, value any, rename func(string, string) error) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create account state directory: %w", err)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode account state: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary account state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect temporary account state: %w", err)
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary account state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary account state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary account state: %w", err)
	}
	if err := rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace account state: %w", err)
	}
	return nil
}
