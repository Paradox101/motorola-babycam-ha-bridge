package app

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/bridge"
	"github.com/local/motorola-vm65-bridge/internal/health"
)

type fakeServer struct {
	mu       sync.Mutex
	started  chan struct{}
	closed   bool
	serveErr error
}

func newFakeServer(serveErr error) *fakeServer {
	return &fakeServer{started: make(chan struct{}), serveErr: serveErr}
}

func (s *fakeServer) Listen() error { return nil }

func (s *fakeServer) Serve(ctx context.Context) error {
	close(s.started)
	if s.serveErr != nil {
		return s.serveErr
	}
	<-ctx.Done()
	return nil
}

func (s *fakeServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeServer) Addr() net.Addr { return fakeAddr("127.0.0.1:1") }

func (s *fakeServer) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

func TestRuntimeKeepsOtherCamerasRunningAndClosesAllOnCancellation(t *testing.T) {
	first := newFakeServer(nil)
	second := newFakeServer(errors.New("camera failed"))
	servers := []CameraServer{first, second}
	index := 0
	factory := func(bridge.Config) (CameraServer, error) {
		server := servers[index]
		index++
		return server, nil
	}
	registry, err := BuildRegistry("127.0.0.1:8554", []bridge.Credentials{
		{DeviceID: 1, DeviceUDID: "a", DeviceName: "A"},
		{DeviceID: 2, DeviceUDID: "b", DeviceName: "B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	healthState := health.NewState(time.Now())
	runtime := New(RuntimeConfig{Registry: registry, NewServer: factory, Health: healthState})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	<-first.started
	<-second.started
	select {
	case err := <-done:
		t.Fatalf("runtime stopped after one camera failure: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	snapshot := healthState.Snapshot()
	if snapshot.BridgesReady != 1 || snapshot.BridgesTotal != 2 {
		t.Fatalf("bridge health after failure = %d/%d", snapshot.BridgesReady, snapshot.BridgesTotal)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop after cancellation")
	}
	if !first.isClosed() || !second.isClosed() {
		t.Fatalf("servers not closed: first=%t second=%t", first.isClosed(), second.isClosed())
	}
}
