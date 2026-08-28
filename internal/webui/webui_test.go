package webui

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/local/motorola-vm65-bridge/internal/ingress"
)

type fakeSource struct {
	mu        sync.Mutex
	overview  Overview
	restarted []string
	restartEr error
}

func (f *fakeSource) Overview() Overview {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.overview
}

func (f *fakeSource) Restart(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarted = append(f.restarted, id)
	return f.restartEr
}

func twoCameras() Overview {
	warm := 20.5
	return Overview{
		Version:     "v1.2.3",
		Go2RTCReady: true,
		MQTTEnabled: true,
		Cameras: []Camera{
			{ID: "b", Name: "Zoe", Stream: "zoe", Serving: false},
			{ID: "a", Name: "Ada", Model: "VM65CONNECT", Stream: "vm65",
				StreamURL: "rtsp://host:8555/vm65", Serving: true, ActiveSessions: 2,
				TemperatureCelsius: &warm},
		},
	}
}

func newServer(t *testing.T, source Source, media, stills http.Handler) http.Handler {
	t.Helper()
	server, err := NewServer(Config{
		Source:   source,
		Media:    media,
		Snapshot: stills,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
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

func TestEverythingNeedsAHomeAssistantSession(t *testing.T) {
	source := &fakeSource{overview: twoCameras()}
	handler := newServer(t, source, nil, nil)
	for _, target := range []string{"/", "/api/cameras", "/api/cameras/restart"} {
		if recorder := do(handler, http.MethodGet, target, "", false); recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d", target, recorder.Code, http.StatusUnauthorized)
		}
	}
}

func TestOverviewIsSortedByName(t *testing.T) {
	source := &fakeSource{overview: twoCameras()}
	handler := newServer(t, source, nil, nil)
	recorder := do(handler, http.MethodGet, "/api/cameras", "", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var overview Overview
	if err := json.Unmarshal(recorder.Body.Bytes(), &overview); err != nil {
		t.Fatal(err)
	}
	if len(overview.Cameras) != 2 || overview.Cameras[0].Name != "Ada" {
		t.Fatalf("cameras = %#v", overview.Cameras)
	}
	first := overview.Cameras[0]
	if !first.Serving || first.ActiveSessions != 2 || first.StreamURL != "rtsp://host:8555/vm65" {
		t.Fatalf("first camera = %#v", first)
	}
	if first.TemperatureCelsius == nil || *first.TemperatureCelsius != 20.5 {
		t.Fatalf("temperature = %#v", first.TemperatureCelsius)
	}
	// A camera without a reading must not report one.
	if overview.Cameras[1].TemperatureCelsius != nil {
		t.Fatalf("second camera temperature = %#v", overview.Cameras[1].TemperatureCelsius)
	}
}

func TestRestartReachesTheRuntime(t *testing.T) {
	source := &fakeSource{overview: twoCameras()}
	handler := newServer(t, source, nil, nil)
	recorder := do(handler, http.MethodPost, "/api/cameras/restart", `{"id":"a"}`, true)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s)", recorder.Code, recorder.Body)
	}
	if len(source.restarted) != 1 || source.restarted[0] != "a" {
		t.Fatalf("restarted = %v", source.restarted)
	}
}

func TestRestartingAnUnknownCameraIsRefused(t *testing.T) {
	source := &fakeSource{overview: twoCameras(), restartEr: errors.New("no such camera")}
	handler := newServer(t, source, nil, nil)
	recorder := do(handler, http.MethodPost, "/api/cameras/restart", `{"id":"nope"}`, true)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

// A cross-origin form cannot set this content type without a preflight this
// server never answers, so requiring JSON is what keeps another site out.
func TestRestartRefusesFormPosts(t *testing.T) {
	source := &fakeSource{overview: twoCameras()}
	handler := newServer(t, source, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/cameras/restart", strings.NewReader("id=a"))
	request.RemoteAddr = "172.30.32.2:41000"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set(ingress.UserIDHeader, "01HQ")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnsupportedMediaType)
	}
	if len(source.restarted) != 0 {
		t.Fatal("a form post restarted a camera")
	}
}

func TestGetOnRestartIsRefused(t *testing.T) {
	handler := newServer(t, &fakeSource{overview: twoCameras()}, nil, nil)
	if recorder := do(handler, http.MethodGet, "/api/cameras/restart", "", true); recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

// The player needs go2rtc for video and stills; nothing else about go2rtc is
// exposed, and both are reached only after the same authentication as the page.
func TestMediaAndStillsAreMountedForThePlayer(t *testing.T) {
	var mediaPaths []string
	media := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mediaPaths = append(mediaPaths, request.URL.Path)
		writer.WriteHeader(http.StatusOK)
	})
	stills := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "image/jpeg")
		_, _ = writer.Write([]byte{0xFF, 0xD8})
	})
	handler := newServer(t, &fakeSource{overview: twoCameras()}, media, stills)

	if recorder := do(handler, http.MethodPost, "/api/webrtc?src=vm65", `{"type":"offer"}`, true); recorder.Code != http.StatusOK {
		t.Fatalf("webrtc status = %d", recorder.Code)
	}
	if recorder := do(handler, http.MethodGet, "/camera-still?src=vm65", "", true); recorder.Code != http.StatusOK {
		t.Fatalf("still status = %d", recorder.Code)
	}
	// go2rtc's own page and configuration are not proxied any more.
	for _, target := range []string{"/api/config", "/api/streams", "/api/restart"} {
		if recorder := do(handler, http.MethodGet, target, "", true); recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d", target, recorder.Code, http.StatusNotFound)
		}
	}
	if len(mediaPaths) != 1 || mediaPaths[0] != "/api/webrtc" {
		t.Fatalf("media paths = %v", mediaPaths)
	}
}

func TestThePageIsSelfContained(t *testing.T) {
	handler := newServer(t, &fakeSource{overview: twoCameras()}, nil, nil)
	recorder := do(handler, http.MethodGet, "/", "", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{"http://", "https://", "//cdn"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the page loads something external: %q", forbidden)
		}
	}
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("content security policy = %q", recorder.Header().Get("Content-Security-Policy"))
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

func TestNewServerRequiresASource(t *testing.T) {
	if _, err := NewServer(Config{}); err == nil {
		t.Fatal("expected a missing source to be rejected")
	}
}
