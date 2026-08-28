package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLivenessAndReadinessTransitions(t *testing.T) {
	state := NewState(time.Unix(100, 0))
	handler := NewHandler(state)

	assertStatus(t, handler, "/healthz", http.StatusServiceUnavailable)
	assertStatus(t, handler, "/readyz", http.StatusServiceUnavailable)

	state.SetLive(true)
	state.SetCredentialsReady(true)
	state.SetBridges(2, 2)
	state.SetGo2RTC(true, false)
	assertStatus(t, handler, "/healthz", http.StatusOK)
	assertStatus(t, handler, "/readyz", http.StatusServiceUnavailable)

	state.SetGo2RTC(true, true)
	state.SetMQTT(true, false)
	assertStatus(t, handler, "/readyz", http.StatusOK)
}

func TestLivenessFailsWhenNoBridgeIsServing(t *testing.T) {
	state := NewState(time.Unix(100, 0))
	handler := NewHandler(state)
	state.SetLive(true)
	state.SetCredentialsReady(true)

	// Every configured camera bridge is down: this is what the Home Assistant
	// watchdog has to be able to see.
	state.SetBridges(0, 2)
	assertStatus(t, handler, "/healthz", http.StatusServiceUnavailable)

	// A partial outage stays healthy — the runtime restarts that bridge itself,
	// and restarting the add-on would drop the camera that still works.
	state.SetBridges(1, 2)
	assertStatus(t, handler, "/healthz", http.StatusOK)
}

func TestStatusReportsSessionCounters(t *testing.T) {
	state := NewState(time.Unix(100, 0))
	state.SetLive(true)
	state.SetCounters(3, 2)
	snapshot := state.Snapshot()
	if snapshot.ReconnectsTotal != 3 || snapshot.ActiveSessions != 2 {
		t.Fatalf("counters = %d reconnects / %d active", snapshot.ReconnectsTotal, snapshot.ActiveSessions)
	}
}

func TestStatusContainsOnlyCategorizedErrors(t *testing.T) {
	state := NewState(time.Unix(100, 0))
	state.SetLive(true)
	state.SetLastError(ErrorAuthorization)
	handler := NewHandler(state)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "token") || strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("status body contains secret field name: %s", recorder.Body.String())
	}
	var snapshot Snapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.LastError != ErrorAuthorization {
		t.Fatalf("last error = %q", snapshot.LastError)
	}
}

func assertStatus(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != want {
		t.Fatalf("%s status = %d, want %d; body=%s", path, recorder.Code, want, recorder.Body.String())
	}
}
