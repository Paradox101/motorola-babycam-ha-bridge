package ingress

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestHandler(t *testing.T, upstream string, streams ...string) http.Handler {
	t.Helper()
	handler, err := NewHandler(Config{Upstream: upstream, Streams: streams})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return handler
}

func request(method, target string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "172.30.32.2:45000"
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req
}

func authenticated(method, target string) *http.Request {
	return request(method, target, map[string]string{UserIDHeader: "01HQ"})
}

func TestNewHandlerRejectsRelativeUpstream(t *testing.T) {
	if _, err := NewHandler(Config{Upstream: "127.0.0.1:1984"}); err == nil {
		t.Fatal("expected a relative upstream to be rejected")
	}
}

func TestUnauthenticatedRequestIsRefused(t *testing.T) {
	handler := newTestHandler(t, "http://127.0.0.1:1", "vm65")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRequestFromOutsideTheSupervisorNetworkIsRefused(t *testing.T) {
	handler := newTestHandler(t, "http://127.0.0.1:1", "vm65")
	req := authenticated(http.MethodGet, "/")
	req.RemoteAddr = "192.168.1.20:5000"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestAuthenticatedRequestReachesUpstream(t *testing.T) {
	var seen *http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		seen = req.Clone(req.Context())
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"streams":{}}`)
	}))
	defer upstream.Close()

	handler := newTestHandler(t, upstream.URL, "vm65")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authenticated(http.MethodGet, "/api/streams"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if seen == nil || seen.URL.Path != "/api/streams" {
		t.Fatalf("upstream path = %v, want /api/streams", seen)
	}
	if got := seen.Header.Get(UserIDHeader); got != "01HQ" {
		t.Fatalf("forwarded user id = %q, want 01HQ", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

// The Supervisor is the only thing that should reach this port, so a client
// that forges the forwarding headers must not have them believed.
func TestForgedForwardingHeadersAreReplaced(t *testing.T) {
	var seen http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		seen = req.Header.Clone()
		writer.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	handler := newTestHandler(t, upstream.URL, "vm65")
	handler.ServeHTTP(httptest.NewRecorder(), request(http.MethodGet, "/", map[string]string{
		UserIDHeader:      "01HQ",
		"X-Forwarded-For": "10.9.9.9",
	}))
	if got := seen.Get("X-Forwarded-For"); got != "172.30.32.2" {
		t.Fatalf("X-Forwarded-For = %q, want the real peer address", got)
	}
	if got := seen.Get("X-Remote-User-Name"); got != "" {
		t.Fatalf("X-Remote-User-Name = %q, want empty", got)
	}
}

func TestConfigurationEndpointIsBlocked(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "rtsp://user:password@host/stream")
	}))
	defer upstream.Close()

	handler := newTestHandler(t, upstream.URL, "vm65")
	for _, path := range []string{"/api/config", "/api/config/", "/api/restart", "/api/exit", "/api/streams.dot"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, authenticated(http.MethodGet, path))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d, want %d", path, recorder.Code, http.StatusForbidden)
		}
		if strings.Contains(recorder.Body.String(), "rtsp://") {
			t.Fatalf("%s leaked the go2rtc configuration", path)
		}
	}
}

// go2rtc turns any src= into a stream, and "exec:" is a source scheme, so an
// unknown src is a command-execution attempt however innocent the path looks.
func TestUnknownStreamSourceIsBlocked(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be reached")
		writer.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	handler := newTestHandler(t, upstream.URL, "vm65")
	targets := []string{
		"/api/streams?src=" + url.QueryEscape("exec:/bin/sh -c id"),
		"/api/frame.jpeg?src=" + url.QueryEscape("rtsp://attacker.example/stream"),
		"/api/ws?src=other",
		"/api/stream.mp4?dst=" + url.QueryEscape("exec:touch /tmp/pwned"),
	}
	for _, target := range targets {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, authenticated(http.MethodGet, target))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d, want %d", target, recorder.Code, http.StatusForbidden)
		}
	}
}

func TestConfiguredStreamIsAllowed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "image/jpeg")
		_, _ = writer.Write([]byte{0xFF, 0xD8})
	}))
	defer upstream.Close()

	handler := newTestHandler(t, upstream.URL, "vm65", "nursery")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authenticated(http.MethodGet, "/api/frame.jpeg?src=nursery"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestWriteMethodsAreRefusedExceptNegotiation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	handler := newTestHandler(t, upstream.URL, "vm65")

	refused := []struct {
		method string
		target string
	}{
		{http.MethodPost, "/api/streams?name=x&src=vm65"},
		{http.MethodPut, "/api/config"},
		{http.MethodDelete, "/api/streams?src=vm65"},
		{http.MethodPatch, "/api/streams"},
	}
	for _, item := range refused {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, authenticated(item.method, item.target))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s %s status = %d, want %d", item.method, item.target, recorder.Code, http.StatusForbidden)
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authenticated(http.MethodPost, "/api/webrtc?src=vm65"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("WebRTC negotiation status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestUnavailableUpstreamReportsBadGateway(t *testing.T) {
	handler := newTestHandler(t, "http://127.0.0.1:1", "vm65")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authenticated(http.MethodGet, "/"))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
}
