package pairing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/local/motorola-vm65-bridge/internal/fivegencare"
	"github.com/local/motorola-vm65-bridge/internal/ingress"
)

type fakeProvider struct {
	mu        sync.Mutex
	status    fivegencare.PairingStatus
	requestEr error
	submitEr  error
	emails    []string
	codes     []string
}

func (f *fakeProvider) Status() (fivegencare.PairingStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *fakeProvider) RequestCode(_ context.Context, email string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emails = append(f.emails, email)
	if f.requestEr != nil {
		return f.requestEr
	}
	f.status = fivegencare.PairingStatus{AwaitingCode: true, Email: email, CodeExpiresInSeconds: 900}
	return nil
}

func (f *fakeProvider) SubmitCode(_ context.Context, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes = append(f.codes, code)
	if f.submitEr != nil {
		return f.submitEr
	}
	f.status = fivegencare.PairingStatus{Paired: true, Email: f.status.Email}
	return nil
}

func newServer(t *testing.T, provider Provider, onPaired func()) http.Handler {
	t.Helper()
	server, err := NewServer(Config{
		Provider: provider,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnPaired: onPaired,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return server.Handler()
}

func do(handler http.Handler, method, target, body string, authenticated bool) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	request.RemoteAddr = "172.30.32.2:41000"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		request.Header.Set(ingress.UserIDHeader, "01HQ")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestPairingNeedsAHomeAssistantSession(t *testing.T) {
	provider := &fakeProvider{}
	handler := newServer(t, provider, nil)
	for _, target := range []string{"/", "/api/pairing/status", "/api/pairing/code"} {
		recorder := do(handler, http.MethodGet, target, "", false)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d", target, recorder.Code, http.StatusUnauthorized)
		}
	}
	if len(provider.emails) != 0 {
		t.Fatal("an unauthenticated request reached the account service")
	}
}

func TestPairingRefusesPeersOutsideTheSupervisorNetwork(t *testing.T) {
	handler := newServer(t, &fakeProvider{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.168.1.30:5000"
	request.Header.Set(ingress.UserIDHeader, "01HQ")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

// The whole point: email in, code in, paired — no restart, no configuration.
func TestPairingCompletesInOneVisit(t *testing.T) {
	provider := &fakeProvider{}
	var pairedOnce sync.WaitGroup
	pairedOnce.Add(1)
	handler := newServer(t, provider, pairedOnce.Done)

	recorder := do(handler, http.MethodGet, "/api/pairing/status", "", true)
	var status fivegencare.PairingStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Paired || status.AwaitingCode {
		t.Fatalf("initial status = %#v", status)
	}

	recorder = do(handler, http.MethodPost, "/api/pairing/code", `{"email":"owner@example.test"}`, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code request status = %d (%s)", recorder.Code, recorder.Body)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.AwaitingCode || status.Email != "owner@example.test" {
		t.Fatalf("status after request = %#v", status)
	}

	recorder = do(handler, http.MethodPost, "/api/pairing/verify", `{"code":"123456"}`, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("verify status = %d (%s)", recorder.Code, recorder.Body)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Paired {
		t.Fatalf("status after verify = %#v", status)
	}
	if len(provider.codes) != 1 || provider.codes[0] != "123456" {
		t.Fatalf("submitted codes = %v", provider.codes)
	}
	pairedOnce.Wait()
}

func TestAnExpiredCodeSaysToRequestANewOne(t *testing.T) {
	provider := &fakeProvider{submitEr: fivegencare.ErrNoChallenge}
	handler := newServer(t, provider, nil)
	recorder := do(handler, http.MethodPost, "/api/pairing/verify", `{"code":"123456"}`, true)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.Error, "expired") {
		t.Fatalf("message = %q, want it to explain the code expired", body.Error)
	}
}

// A rejected code must not leak the reason the account service gave, and the
// code itself must never reach the response.
func TestARejectedCodeIsReportedWithoutDetail(t *testing.T) {
	provider := &fakeProvider{submitEr: errors.New("server rejected login for owner@example.test with 987654")}
	handler := newServer(t, provider, nil)
	recorder := do(handler, http.MethodPost, "/api/pairing/verify", `{"code":"987654"}`, true)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if strings.Contains(recorder.Body.String(), "987654") ||
		strings.Contains(recorder.Body.String(), "owner@example.test") {
		t.Fatalf("response leaked the upstream detail: %s", recorder.Body)
	}
}

// Requiring JSON is what stops another site posting here: a cross-origin form
// cannot set this content type without a preflight this server never answers.
func TestFormPostsAreRefused(t *testing.T) {
	provider := &fakeProvider{}
	handler := newServer(t, provider, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/pairing/code",
		strings.NewReader("email=attacker@example.test"))
	request.RemoteAddr = "172.30.32.2:41000"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set(ingress.UserIDHeader, "01HQ")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnsupportedMediaType)
	}
	if len(provider.emails) != 0 {
		t.Fatal("a form post reached the account service")
	}
}

func TestUnknownFieldsAndOversizedBodiesAreRefused(t *testing.T) {
	handler := newServer(t, &fakeProvider{}, nil)
	recorder := do(handler, http.MethodPost, "/api/pairing/code", `{"email":"a@b.test","admin":true}`, true)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	recorder = do(handler, http.MethodPost, "/api/pairing/code",
		`{"email":"`+strings.Repeat("a", 8<<10)+`"}`, true)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestThePageIsSelfContained(t *testing.T) {
	handler := newServer(t, &fakeProvider{}, nil)
	recorder := do(handler, http.MethodGet, "/", "", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{"http://", "https://", "//cdn", "<img"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the page loads something external: %q", forbidden)
		}
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("missing content security policy: %q", recorder.Header().Get("Content-Security-Policy"))
	}
}

func TestUnknownPathsAreNotFound(t *testing.T) {
	handler := newServer(t, &fakeProvider{}, nil)
	if recorder := do(handler, http.MethodGet, "/something", "", true); recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestNewServerRequiresAProvider(t *testing.T) {
	if _, err := NewServer(Config{}); err == nil {
		t.Fatal("expected a missing provider to be rejected")
	}
}
