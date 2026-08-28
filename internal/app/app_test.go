package app

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/bridge"
	"github.com/local/motorola-vm65-bridge/internal/health"
)

type fakeServer struct {
	listenAddr string
	serveErr   error

	startedOnce sync.Once
	started     chan struct{}
	closedOnce  sync.Once
	// stopped models a closed listener: the real server's Serve returns as
	// soon as its socket is closed, which is what a restart relies on.
	stopped chan struct{}

	mu     sync.Mutex
	closed bool

	active atomic.Int64
	total  atomic.Int64
}

func newFakeServer(listenAddr string, serveErr error) *fakeServer {
	return &fakeServer{
		listenAddr: listenAddr, serveErr: serveErr,
		started: make(chan struct{}), stopped: make(chan struct{}),
	}
}

func (s *fakeServer) Listen() error { return nil }

func (s *fakeServer) Serve(ctx context.Context) error {
	s.startedOnce.Do(func() { close(s.started) })
	if s.serveErr != nil {
		return s.serveErr
	}
	select {
	case <-ctx.Done():
	case <-s.stopped:
	}
	return nil
}

func (s *fakeServer) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.closedOnce.Do(func() { close(s.stopped) })
	return nil
}

func (s *fakeServer) Addr() net.Addr { return fakeAddr(s.listenAddr) }

func (s *fakeServer) Stats() (int64, int64) { return s.total.Load(), s.active.Load() }

func (s *fakeServer) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

// fakeFactory hands out one server per creation. serveErrs supplies the error
// the n-th created server returns from Serve; beyond that servers block until
// their context is cancelled.
type fakeFactory struct {
	mu        sync.Mutex
	created   []*fakeServer
	serveErrs []error
}

func (f *fakeFactory) new(cfg bridge.Config) (CameraServer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var serveErr error
	if len(f.created) < len(f.serveErrs) {
		serveErr = f.serveErrs[len(f.created)]
	}
	server := newFakeServer(cfg.ListenAddr, serveErr)
	f.created = append(f.created, server)
	return server, nil
}

func (f *fakeFactory) snapshot() []*fakeServer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeServer(nil), f.created...)
}

func (f *fakeFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created)
}

func testRuntime(t *testing.T, registry Registry, factory *fakeFactory, state *health.State) (*Runtime, context.CancelFunc, chan error) {
	t.Helper()
	runtime := New(RuntimeConfig{
		Registry:          registry,
		NewServer:         factory.new,
		Health:            state,
		RestartBackoff:    time.Millisecond,
		RestartBackoffMax: 2 * time.Millisecond,
		StatsInterval:     2 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// stopped is closed rather than sent to, so the cleanup can observe the
	// shutdown even when the test body already consumed the error.
	stopped := make(chan struct{})
	go func() {
		done <- runtime.Run(ctx)
		close(stopped)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("runtime did not stop after cancellation")
		}
	})
	return runtime, cancel, done
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func twoCameraRegistry(t *testing.T) Registry {
	t.Helper()
	registry, err := BuildRegistry("127.0.0.1:8554", []bridge.Credentials{
		{DeviceID: 1, DeviceUDID: "a", DeviceName: "A", SID: "sid-a", DeviceToken: "tok-a", ControlHost: "relay"},
		{DeviceID: 2, DeviceUDID: "b", DeviceName: "B", SID: "sid-b", DeviceToken: "tok-b", ControlHost: "relay"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestRuntimeRestartsAFailedCameraBridgeAndCountsTheReconnect(t *testing.T) {
	registry := twoCameraRegistry(t)
	// The first server created fails immediately; every later one blocks.
	factory := &fakeFactory{serveErrs: []error{errors.New("camera failed")}}
	state := health.NewState(time.Now())
	runtime, _, done := testRuntime(t, registry, factory, state)

	waitFor(t, "the failed bridge to be replaced", func() bool { return factory.count() >= 3 })
	waitFor(t, "both bridges to serve again", func() bool {
		snapshot := state.Snapshot()
		return snapshot.BridgesReady == 2 && snapshot.BridgesTotal == 2
	})
	if runtime.Reconnects() == 0 {
		t.Fatal("restart was not counted as a reconnect")
	}
	waitFor(t, "the reconnect count to reach the status endpoint", func() bool {
		return state.Snapshot().ReconnectsTotal > 0
	})

	select {
	case err := <-done:
		t.Fatalf("runtime stopped after a camera failure: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRuntimeClosesEveryBridgeOnCancellation(t *testing.T) {
	registry := twoCameraRegistry(t)
	factory := &fakeFactory{}
	state := health.NewState(time.Now())
	_, cancel, done := testRuntime(t, registry, factory, state)

	waitFor(t, "both bridges to serve", func() bool { return state.Snapshot().BridgesReady == 2 })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not stop after cancellation")
	}
	for index, server := range factory.snapshot() {
		if !server.isClosed() {
			t.Fatalf("server %d was not closed on shutdown", index)
		}
	}
	if ready := state.Snapshot().BridgesReady; ready != 0 {
		t.Fatalf("bridges ready after shutdown = %d", ready)
	}
}

func TestRuntimeReportsActiveSessionsFromTheBridges(t *testing.T) {
	registry := twoCameraRegistry(t)
	factory := &fakeFactory{}
	state := health.NewState(time.Now())
	testRuntime(t, registry, factory, state)

	waitFor(t, "both bridges to serve", func() bool { return state.Snapshot().BridgesReady == 2 })
	servers := factory.snapshot()
	servers[0].active.Store(2)
	servers[1].active.Store(3)

	waitFor(t, "active sessions to reach the status endpoint", func() bool {
		return state.Snapshot().ActiveSessions == 5
	})
}

func TestReloadKeepsUnchangedCamerasServingAndReplacesChangedOnes(t *testing.T) {
	registry := twoCameraRegistry(t)
	factory := &fakeFactory{}
	state := health.NewState(time.Now())
	runtime, _, _ := testRuntime(t, registry, factory, state)

	waitFor(t, "both bridges to serve", func() bool { return state.Snapshot().BridgesReady == 2 })
	before := factory.snapshot()
	if len(before) != 2 {
		t.Fatalf("created %d servers, want 2", len(before))
	}

	// Camera "a" keeps its credentials; camera "b" gets a fresh device token,
	// exactly what a credential refresh produces.
	refreshed, err := BuildRegistry("127.0.0.1:8554", []bridge.Credentials{
		{DeviceID: 1, DeviceUDID: "a", DeviceName: "A", SID: "sid-a", DeviceToken: "tok-a", ControlHost: "relay"},
		{DeviceID: 2, DeviceUDID: "b", DeviceName: "B", SID: "sid-b", DeviceToken: "tok-b-rotated", ControlHost: "relay"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Reload(refreshed); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if before[0].isClosed() {
		t.Fatal("the unchanged camera's bridge was restarted by the reload")
	}
	if !before[1].isClosed() {
		t.Fatal("the rotated camera's bridge was not restarted by the reload")
	}
	waitFor(t, "the replacement bridge to serve", func() bool {
		snapshot := state.Snapshot()
		return snapshot.BridgesReady == 2 && snapshot.BridgesTotal == 2
	})
	if factory.count() != 3 {
		t.Fatalf("created %d servers, want 3 (one replacement only)", factory.count())
	}
}

func TestReloadStartsAddedCamerasAndStopsRemovedOnes(t *testing.T) {
	registry := twoCameraRegistry(t)
	factory := &fakeFactory{}
	state := health.NewState(time.Now())
	runtime, _, _ := testRuntime(t, registry, factory, state)

	waitFor(t, "both bridges to serve", func() bool { return state.Snapshot().BridgesReady == 2 })

	single, err := BuildRegistry("127.0.0.1:8554", []bridge.Credentials{
		{DeviceID: 1, DeviceUDID: "a", DeviceName: "A", SID: "sid-a", DeviceToken: "tok-a", ControlHost: "relay"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Reload(single); err != nil {
		t.Fatalf("reload: %v", err)
	}
	waitFor(t, "the registry to shrink to one camera", func() bool {
		snapshot := state.Snapshot()
		return snapshot.BridgesTotal == 1 && snapshot.BridgesReady == 1
	})
}

func TestReloadRejectsAnEmptyRegistry(t *testing.T) {
	registry := twoCameraRegistry(t)
	factory := &fakeFactory{}
	state := health.NewState(time.Now())
	runtime, _, _ := testRuntime(t, registry, factory, state)

	waitFor(t, "both bridges to serve", func() bool { return state.Snapshot().BridgesReady == 2 })
	if err := runtime.Reload(Registry{}); err == nil {
		t.Fatal("reload accepted an empty registry")
	}
	if ready := state.Snapshot().BridgesReady; ready != 2 {
		t.Fatalf("bridges ready after a rejected reload = %d, want 2", ready)
	}
}

// The Web UI reads this to render each camera and to say which one is down.
func TestCamerasReportLiveStateAndRestartOnDemand(t *testing.T) {
	registry := Registry{Cameras: []Camera{{
		Credentials: bridge.Credentials{DeviceUDID: "a", DeviceName: "Ada", Model: "VM65CONNECT"},
		StreamName:  "ada",
		ListenAddr:  "127.0.0.1:0",
	}}}
	factory := &fakeFactory{}
	runtime := New(RuntimeConfig{
		Registry:       registry,
		NewServer:      factory.new,
		RestartBackoff: time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()

	waitFor(t, "the camera to serve", func() bool {
		cameras := runtime.Cameras()
		return len(cameras) == 1 && cameras[0].Serving
	})
	state := runtime.Cameras()[0]
	if state.ID != "a" || state.Name != "Ada" || state.Model != "VM65CONNECT" || state.StreamName != "ada" {
		t.Fatalf("state = %#v", state)
	}

	// Restarting one camera drops its server; the supervisor rebuilds it.
	if err := runtime.RestartCamera("a"); err != nil {
		t.Fatalf("RestartCamera: %v", err)
	}
	waitFor(t, "the camera to come back", func() bool {
		cameras := runtime.Cameras()
		return runtime.Reconnects() >= 1 && len(cameras) == 1 && cameras[0].Serving
	})

	if err := runtime.RestartCamera("missing"); err == nil {
		t.Fatal("expected an unknown camera to be refused")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}
