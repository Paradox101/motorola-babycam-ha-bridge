package snapshot

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var jpeg = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}

// upstream is a controllable stand-in for go2rtc's still-image endpoint.
type upstream struct {
	server *httptest.Server
	calls  atomic.Int64

	mu     sync.Mutex
	delay  time.Duration
	status int
	body   []byte
	ctype  string
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	up := &upstream{status: http.StatusOK, body: jpeg, ctype: "image/jpeg"}
	up.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		up.calls.Add(1)
		up.mu.Lock()
		delay, status, body, ctype := up.delay, up.status, up.body, up.ctype
		up.mu.Unlock()
		if request.URL.Path != "/api/frame.jpeg" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-request.Context().Done():
				return
			}
		}
		writer.Header().Set("Content-Type", ctype)
		writer.WriteHeader(status)
		_, _ = writer.Write(body)
	}))
	t.Cleanup(up.server.Close)
	return up
}

func (u *upstream) set(delay time.Duration, status int, body []byte, ctype string) {
	u.mu.Lock()
	u.delay, u.status, u.body, u.ctype = delay, status, body, ctype
	u.mu.Unlock()
}

func newCache(t *testing.T, up *upstream, adjust func(*Config)) *Cache {
	t.Helper()
	cfg := Config{
		Upstream: up.server.URL,
		Streams:  []string{"vm65", "nursery"},
		Token:    "secret-token",
		FreshFor: time.Minute,
		StaleFor: time.Minute,
		Wait:     2 * time.Second,
		Fetch:    5 * time.Second,
	}
	if adjust != nil {
		adjust(&cfg)
	}
	cache, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(cache.Close)
	return cache
}

func get(handler http.Handler, target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.RemoteAddr = "172.30.32.1:40000"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestSnapshotIsServedAndThenCached(t *testing.T) {
	up := newUpstream(t)
	handler := newCache(t, up, nil).Handler()

	for attempt := 0; attempt < 3; attempt++ {
		recorder := get(handler, Path+"?src=vm65&token=secret-token")
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (%s)", recorder.Code, http.StatusOK, recorder.Body)
		}
		if got := recorder.Header().Get("Content-Type"); got != "image/jpeg" {
			t.Fatalf("Content-Type = %q", got)
		}
		if recorder.Body.String() != string(jpeg) {
			t.Fatal("body is not the upstream frame")
		}
	}
	if calls := up.calls.Load(); calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls)
	}
}

func TestTokenIsRequired(t *testing.T) {
	up := newUpstream(t)
	handler := newCache(t, up, nil).Handler()
	for _, target := range []string{Path + "?src=vm65", Path + "?src=vm65&token=wrong"} {
		recorder := get(handler, target)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d", target, recorder.Code, http.StatusUnauthorized)
		}
	}
	if calls := up.calls.Load(); calls != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls)
	}
}

func TestUnknownCameraIsRefused(t *testing.T) {
	up := newUpstream(t)
	handler := newCache(t, up, nil).Handler()
	recorder := get(handler, Path+"?src=exec:id&token=secret-token")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if calls := up.calls.Load(); calls != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls)
	}
}

func TestPeerOutsideTheSupervisorNetworkIsRefused(t *testing.T) {
	up := newUpstream(t)
	handler := newCache(t, up, nil).Handler()
	request := httptest.NewRequest(http.MethodGet, Path+"?src=vm65&token=secret-token", nil)
	request.RemoteAddr = "192.168.1.44:33000"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

// Home Assistant gives up after ten seconds and renders a 500. A frame from a
// moment ago is a better answer than that.
func TestASlowCameraServesThePreviousFrame(t *testing.T) {
	up := newUpstream(t)
	cache := newCache(t, up, func(cfg *Config) {
		cfg.FreshFor = time.Millisecond
		cfg.Wait = 50 * time.Millisecond
	})
	handler := cache.Handler()

	if recorder := get(handler, Path+"?src=vm65&token=secret-token"); recorder.Code != http.StatusOK {
		t.Fatalf("priming status = %d", recorder.Code)
	}
	up.set(time.Second, http.StatusOK, jpeg, "image/jpeg")
	time.Sleep(5 * time.Millisecond)

	start := time.Now()
	recorder := get(handler, Path+"?src=vm65&token=secret-token")
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("request waited %s; it must answer well inside the Home Assistant timeout", elapsed)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want the stale frame with %d", recorder.Code, http.StatusOK)
	}
	if recorder.Body.String() != string(jpeg) {
		t.Fatal("stale body is not the previous frame")
	}
}

// A frame older than StaleFor is not a thumbnail any more; say so instead.
func TestAFrameOlderThanStaleForIsNotServed(t *testing.T) {
	up := newUpstream(t)
	cache := newCache(t, up, func(cfg *Config) {
		cfg.FreshFor = time.Millisecond
		cfg.StaleFor = 20 * time.Millisecond
		cfg.Wait = 50 * time.Millisecond
	})
	handler := cache.Handler()
	if recorder := get(handler, Path+"?src=vm65&token=secret-token"); recorder.Code != http.StatusOK {
		t.Fatalf("priming status = %d", recorder.Code)
	}
	up.set(time.Second, http.StatusOK, jpeg, "image/jpeg")
	time.Sleep(40 * time.Millisecond)

	recorder := get(handler, Path+"?src=vm65&token=secret-token")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

// The fetch a timed-out request started keeps running, so the cold start is
// paid once rather than on every attempt.
func TestAFetchOutlivesTheRequestThatStartedIt(t *testing.T) {
	up := newUpstream(t)
	up.set(150*time.Millisecond, http.StatusOK, jpeg, "image/jpeg")
	cache := newCache(t, up, func(cfg *Config) { cfg.Wait = 10 * time.Millisecond })
	handler := cache.Handler()

	if recorder := get(handler, Path+"?src=vm65&token=secret-token"); recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("cold status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	time.Sleep(300 * time.Millisecond)
	recorder := get(handler, Path+"?src=vm65&token=secret-token")
	if recorder.Code != http.StatusOK {
		t.Fatalf("warm status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if calls := up.calls.Load(); calls != 1 {
		t.Fatalf("upstream calls = %d, want 1: the cold fetch must not be repeated", calls)
	}
}

func TestConcurrentRequestsShareOneFetch(t *testing.T) {
	up := newUpstream(t)
	up.set(80*time.Millisecond, http.StatusOK, jpeg, "image/jpeg")
	handler := newCache(t, up, nil).Handler()

	var group sync.WaitGroup
	codes := make([]int, 8)
	for index := range codes {
		group.Add(1)
		go func() {
			defer group.Done()
			codes[index] = get(handler, Path+"?src=vm65&token=secret-token").Code
		}()
	}
	group.Wait()
	for index, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("request %d status = %d, want %d", index, code, http.StatusOK)
		}
	}
	if calls := up.calls.Load(); calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls)
	}
}

func TestUpstreamFailuresAreReported(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   []byte
		ctype  string
	}{
		{"error status", http.StatusInternalServerError, []byte("streams: unknown source"), "text/plain"},
		{"not an image", http.StatusOK, []byte("<html>"), "text/html"},
		{"not a JPEG", http.StatusOK, []byte("nope"), "image/jpeg"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			up := newUpstream(t)
			up.set(0, item.status, item.body, item.ctype)
			handler := newCache(t, up, nil).Handler()
			recorder := get(handler, Path+"?src=vm65&token=secret-token")
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
			}
		})
	}
}

func TestWarmFillsTheCacheBeforeTheFirstRequest(t *testing.T) {
	up := newUpstream(t)
	cache := newCache(t, up, func(cfg *Config) { cfg.Warm = []string{"vm65", "nursery"} })
	cache.Warm()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && up.calls.Load() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	if calls := up.calls.Load(); calls != 2 {
		t.Fatalf("upstream calls = %d, want one per warm stream", calls)
	}
}

// Every distinct name go2rtc is asked for opens its own RTSP session, and so
// its own relay tunnel to a camera that has few to spare. Only the streams this
// add-on actually publishes may be warmed, never the whole allowed list.
func TestWarmTouchesOnlyTheWarmStreams(t *testing.T) {
	up := newUpstream(t)
	cache := newCache(t, up, func(cfg *Config) {
		cfg.Streams = []string{"vm65", "vm65-mjpeg", "nursery", "nursery-mjpeg"}
		cfg.Warm = []string{"vm65"}
	})
	cache.Warm()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && up.calls.Load() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if calls := up.calls.Load(); calls != 1 {
		t.Fatalf("upstream calls = %d, want exactly the one warm stream", calls)
	}
}

func TestWarmingAnUnknownStreamIsRefused(t *testing.T) {
	up := newUpstream(t)
	_, err := New(Config{Upstream: up.server.URL, Streams: []string{"vm65"}, Warm: []string{"nursery"}})
	if err == nil {
		t.Fatal("New accepted a warm stream that is not configured")
	}
}

// A frame at the size cap is a frame that was cut off. Serving it would put a
// half JPEG in front of a camera for the whole stale window.
func TestATruncatedFrameIsNotCached(t *testing.T) {
	oversized := make([]byte, maxImageBytes+64)
	oversized[0], oversized[1] = 0xFF, 0xD8
	up := newUpstream(t)
	up.set(0, http.StatusOK, oversized, "image/jpeg")
	handler := newCache(t, up, nil).Handler()
	recorder := get(handler, Path+"?src=vm65&token=secret-token")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestWriteMethodsAreRefused(t *testing.T) {
	up := newUpstream(t)
	handler := newCache(t, up, nil).Handler()
	request := httptest.NewRequest(http.MethodPost, Path+"?src=vm65&token=secret-token", nil)
	request.RemoteAddr = "172.30.32.1:40000"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestNewRejectsBadConfiguration(t *testing.T) {
	if _, err := New(Config{Upstream: "127.0.0.1:1984", Streams: []string{"vm65"}}); err == nil {
		t.Fatal("expected a relative upstream to be rejected")
	}
	if _, err := New(Config{Upstream: "http://127.0.0.1:1984"}); err == nil {
		t.Fatal("expected a missing stream list to be rejected")
	}
}

func TestURL(t *testing.T) {
	got := URL("http://local-vm65-bridge:8099", "vm65", "abc")
	want := "http://local-vm65-bridge:8099/snapshot?src=vm65&token=abc"
	if got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
	if URL("", "vm65", "abc") != "" || URL("http://host", "", "abc") != "" {
		t.Fatal("an incomplete URL must be empty rather than wrong")
	}
	if strings.Contains(URL("http://host/", "vm65", ""), "token") {
		t.Fatal("no token must mean no token parameter")
	}
}

func TestLoadOrCreateTokenPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "snapshot-token")
	first, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	if len(first) != 64 {
		t.Fatalf("token length = %d, want 64", len(first))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("token file mode = %v, want 0600", mode)
	}
	second, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	if second != first {
		t.Fatal("the token must survive a restart so a published URL keeps working")
	}
}

func TestLoadOrCreateTokenWithoutPathIsEphemeral(t *testing.T) {
	first, err := LoadOrCreateToken("")
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	second, _ := LoadOrCreateToken("")
	if first == second || first == "" {
		t.Fatal("an unpersisted token must be freshly random each time")
	}
}
