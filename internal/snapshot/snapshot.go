// Package snapshot serves camera still images to Home Assistant.
//
// Home Assistant fetches an image entity's URL with a ten-second timeout and
// turns anything else into a 500 from its image proxy. Pointing it straight at
// go2rtc could not meet that: a still frame requires the relay tunnel, the
// camera and a transcode to start from cold, which regularly takes longer, and
// each attempt started the whole chain again.
//
// So the bridge serves the still image itself. One fetch runs at a time per
// camera and keeps running after the requester gave up, a recent frame answers
// immediately, and a slightly stale frame is preferred over an error — a
// picture from a moment ago is what a thumbnail is for. The endpoint is
// restricted to the Supervisor network and carries a token, because the URL is
// published over MQTT and an unauthenticated nursery camera image is not
// something to leave on a network.
package snapshot

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/netguard"
)

// Path is where the handler expects to be mounted.
const Path = "/snapshot"

const (
	// DefaultFreshFor is how long a frame answers without contacting go2rtc.
	DefaultFreshFor = 5 * time.Second
	// DefaultStaleFor is how old a frame may be and still beat an error.
	DefaultStaleFor = 5 * time.Minute
	// DefaultWait is the budget for one request. It stays under the ten
	// seconds Home Assistant allows so a slow camera returns the previous
	// frame instead of a 500.
	DefaultWait = 7 * time.Second
	// DefaultFetch is how long a background fetch may take. Starting the relay
	// tunnel, the camera stream and the transcode from cold is slow, and the
	// result is worth keeping even if the request that triggered it is gone.
	DefaultFetch = 45 * time.Second

	maxImageBytes = 8 << 20
)

type Config struct {
	// Upstream is the go2rtc base URL.
	Upstream string
	// Streams are the go2rtc stream names that may be requested.
	Streams []string
	// Token must be presented as the `token` query parameter. Empty disables
	// the check, which only tests do.
	Token string
	// TrustedCIDRs restricts who may connect. Nil selects
	// netguard.SupervisorCIDRs; an explicitly empty slice disables the check.
	TrustedCIDRs []string

	FreshFor time.Duration
	StaleFor time.Duration
	Wait     time.Duration
	Fetch    time.Duration

	Client *http.Client
	Logger *slog.Logger
	now    func() time.Time
}

type frame struct {
	data []byte
	at   time.Time
}

type entry struct {
	frame    *frame
	inFlight chan struct{}
	err      error
}

// Cache is the snapshot handler and its per-camera frame cache.
type Cache struct {
	cfg     Config
	guard   *netguard.Guard
	streams map[string]bool
	client  *http.Client
	now     func() time.Time

	mu      sync.Mutex
	entries map[string]*entry

	ctx    context.Context
	cancel context.CancelFunc
}

func New(cfg Config) (*Cache, error) {
	upstream, err := url.Parse(cfg.Upstream)
	if err != nil || (upstream.Scheme != "http" && upstream.Scheme != "https") || upstream.Host == "" {
		return nil, errors.New("snapshot upstream must be an absolute http or https URL")
	}
	guard, err := netguard.New(cfg.TrustedCIDRs)
	if err != nil {
		return nil, err
	}
	streams := make(map[string]bool, len(cfg.Streams))
	for _, name := range cfg.Streams {
		if name != "" {
			streams[name] = true
		}
	}
	if len(streams) == 0 {
		return nil, errors.New("snapshot service needs at least one stream name")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.FreshFor <= 0 {
		cfg.FreshFor = DefaultFreshFor
	}
	if cfg.StaleFor <= 0 {
		cfg.StaleFor = DefaultStaleFor
	}
	if cfg.Wait <= 0 {
		cfg.Wait = DefaultWait
	}
	if cfg.Fetch <= 0 {
		cfg.Fetch = DefaultFetch
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: cfg.Fetch}
	}
	now := cfg.now
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Cache{
		cfg: cfg, guard: guard, streams: streams, client: client, now: now,
		entries: make(map[string]*entry), ctx: ctx, cancel: cancel,
	}, nil
}

// Close abandons any fetch still running.
func (c *Cache) Close() {
	if c != nil {
		c.cancel()
	}
}

// Token is the value a request must present. It is empty for a nil Cache, so a
// build without snapshots publishes no snapshot URL at all.
func (c *Cache) Token() string {
	if c == nil {
		return ""
	}
	return c.cfg.Token
}

// Warm starts one background fetch per stream so the first dashboard that asks
// for a thumbnail does not pay for the cold start.
func (c *Cache) Warm() {
	if c == nil {
		return
	}
	for name := range c.streams {
		c.mu.Lock()
		c.startLocked(name)
		c.mu.Unlock()
	}
}

// URL is the address Home Assistant should fetch for one stream, under base.
func URL(base, stream, token string) string {
	if base == "" || stream == "" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + Path
	query := url.Values{"src": []string{stream}}
	if token != "" {
		query.Set("token", token)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (c *Cache) Handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !c.guard.Allow(request.RemoteAddr) {
			http.Error(writer, "forbidden", http.StatusForbidden)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		query := request.URL.Query()
		if c.cfg.Token != "" {
			presented := query.Get("token")
			if subtle.ConstantTimeCompare([]byte(presented), []byte(c.cfg.Token)) != 1 {
				http.Error(writer, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		source := query.Get("src")
		if !c.streams[source] {
			http.Error(writer, "unknown camera", http.StatusNotFound)
			return
		}

		image, age, err := c.get(request.Context(), source)
		if err != nil {
			c.cfg.Logger.Warn("snapshot unavailable", "camera", source, "err", err)
			http.Error(writer, "snapshot unavailable", http.StatusServiceUnavailable)
			return
		}
		header := writer.Header()
		header.Set("Content-Type", "image/jpeg")
		header.Set("Content-Length", strconv.Itoa(len(image)))
		header.Set("Cache-Control", "no-store")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Snapshot-Age", strconv.FormatInt(int64(age.Seconds()), 10))
		writer.WriteHeader(http.StatusOK)
		if request.Method != http.MethodHead {
			_, _ = writer.Write(image)
		}
	})
}

// get returns the best frame it can produce within the request budget: a fresh
// one, else a newly fetched one, else the most recent frame that is not older
// than StaleFor.
func (c *Cache) get(ctx context.Context, source string) ([]byte, time.Duration, error) {
	c.mu.Lock()
	current := c.entries[source]
	if current != nil && current.frame != nil && c.now().Sub(current.frame.at) < c.cfg.FreshFor {
		image, at := current.frame.data, current.frame.at
		c.mu.Unlock()
		return image, c.now().Sub(at), nil
	}
	done := c.startLocked(source)
	c.mu.Unlock()

	timer := time.NewTimer(c.cfg.Wait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	case <-ctx.Done():
	}

	c.mu.Lock()
	current = c.entries[source]
	var image []byte
	var age time.Duration
	var err error
	if current != nil {
		err = current.err
		if current.frame != nil {
			age = c.now().Sub(current.frame.at)
			if age <= c.cfg.StaleFor {
				image = current.frame.data
			}
		}
	}
	c.mu.Unlock()

	if image != nil {
		return image, age, nil
	}
	if err == nil {
		err = errors.New("no frame available yet")
	}
	return nil, 0, err
}

// startLocked makes sure exactly one fetch per camera is running and returns
// the channel that closes when it finishes. The fetch deliberately outlives the
// request that started it, so a cold start that took too long this time still
// fills the cache for the next one.
func (c *Cache) startLocked(source string) chan struct{} {
	current := c.entries[source]
	if current == nil {
		current = &entry{}
		c.entries[source] = current
	}
	if current.inFlight != nil {
		return current.inFlight
	}
	done := make(chan struct{})
	current.inFlight = done
	go func() {
		image, err := c.fetch(source)
		c.mu.Lock()
		current.inFlight = nil
		current.err = err
		if err == nil {
			current.frame = &frame{data: image, at: c.now()}
		}
		c.mu.Unlock()
		close(done)
	}()
	return done
}

func (c *Cache) fetch(source string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(c.ctx, c.cfg.Fetch)
	defer cancel()

	endpoint, err := url.Parse(c.cfg.Upstream)
	if err != nil {
		return nil, err
	}
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/") + "/api/frame.jpeg"
	endpoint.RawQuery = url.Values{"src": []string{source}}.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch snapshot: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		// go2rtc answers a still-image request it cannot serve with a plain
		// text reason; keeping a short prefix of it makes the add-on log say
		// what is actually wrong.
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 256))
		return nil, fmt.Errorf("go2rtc returned %d: %s", response.StatusCode, strings.TrimSpace(string(detail)))
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("go2rtc returned %q instead of an image", contentType)
	}
	image, err := io.ReadAll(io.LimitReader(response.Body, maxImageBytes))
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	if len(image) < 2 || image[0] != 0xFF || image[1] != 0xD8 {
		return nil, errors.New("go2rtc did not return a JPEG frame")
	}
	return image, nil
}

// LoadOrCreateToken reads the snapshot token from path, creating a new random
// one when there is none. Persisting it means the URL Home Assistant already
// holds keeps working across a restart, so a restart never leaves a broken
// thumbnail behind while the retained MQTT message catches up.
func LoadOrCreateToken(path string) (string, error) {
	if path == "" {
		return NewToken()
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		if token := strings.TrimSpace(string(raw)); len(token) >= 32 {
			return token, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read snapshot token: %w", err)
	}
	token, err := NewToken()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create snapshot token directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write snapshot token: %w", err)
	}
	return token, nil
}

// NewToken returns a fresh random token.
func NewToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate snapshot token: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}
