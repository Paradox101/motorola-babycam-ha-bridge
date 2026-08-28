package netguard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowDefaultsToTheSupervisorNetwork(t *testing.T) {
	guard, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	allowed := []string{"172.30.32.1:1", "172.30.33.200:9", "127.0.0.1:8", "[::1]:8", "172.30.32.2"}
	for _, address := range allowed {
		if !guard.Allow(address) {
			t.Errorf("Allow(%q) = false, want true", address)
		}
	}
	refused := []string{"192.168.1.5:1", "172.30.34.1:1", "10.0.0.1:1", "", "not-an-address:1"}
	for _, address := range refused {
		if guard.Allow(address) {
			t.Errorf("Allow(%q) = true, want false", address)
		}
	}
}

func TestEmptyListDisablesTheCheck(t *testing.T) {
	guard, err := New([]string{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !guard.Allow("203.0.113.7:100") {
		t.Fatal("an empty network list must allow every peer")
	}
}

func TestInvalidCIDRIsRejected(t *testing.T) {
	if _, err := New([]string{"172.30.32.0"}); err == nil {
		t.Fatal("expected a bare address to be rejected as a CIDR")
	}
}

func TestWrapRefusesUntrustedPeers(t *testing.T) {
	guard, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	handler := guard.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTeapot)
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.168.1.9:1000"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}
