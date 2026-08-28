// Package app supervises all per-camera bridge listeners as one process.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"github.com/local/motorola-vm65-bridge/internal/bridge"
	"github.com/local/motorola-vm65-bridge/internal/health"
)

type CameraServer interface {
	Listen() error
	Serve(context.Context) error
	Close() error
	Addr() net.Addr
}

type ServerFactory func(bridge.Config) (CameraServer, error)

type RuntimeConfig struct {
	Registry  Registry
	Logger    *slog.Logger
	NewServer ServerFactory
	Health    *health.State
}

type Runtime struct {
	cfg RuntimeConfig
}

type runningCamera struct {
	camera Camera
	server CameraServer
}

func New(cfg RuntimeConfig) *Runtime {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.NewServer == nil {
		cfg.NewServer = func(cfg bridge.Config) (CameraServer, error) {
			return bridge.New(cfg)
		}
	}
	return &Runtime{cfg: cfg}
}

func (r *Runtime) Run(ctx context.Context) error {
	running := make([]runningCamera, 0, len(r.cfg.Registry.Cameras))
	var ready int32
	if r.cfg.Health != nil {
		r.cfg.Health.SetBridges(0, len(r.cfg.Registry.Cameras))
	}
	for _, camera := range r.cfg.Registry.Cameras {
		server, err := r.cfg.NewServer(bridge.Config{
			ListenAddr:  camera.ListenAddr,
			Credentials: camera.Credentials,
			Logger:      r.cfg.Logger.With("camera", camera.StreamName),
		})
		if err != nil {
			closeServers(running)
			return err
		}
		if err := server.Listen(); err != nil {
			_ = server.Close()
			closeServers(running)
			return err
		}
		running = append(running, runningCamera{camera: camera, server: server})
		ready++
		if r.cfg.Health != nil {
			r.cfg.Health.SetBridges(int(ready), len(r.cfg.Registry.Cameras))
		}
	}

	errorsChannel := make(chan struct {
		camera string
		err    error
	}, len(running))
	var waitGroup sync.WaitGroup
	for _, item := range running {
		item := item
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := item.server.Serve(ctx); ctx.Err() == nil {
				remaining := atomic.AddInt32(&ready, -1)
				if r.cfg.Health != nil {
					r.cfg.Health.SetBridges(int(remaining), len(r.cfg.Registry.Cameras))
				}
				if err == nil {
					err = errors.New("camera bridge stopped unexpectedly")
				}
				errorsChannel <- struct {
					camera string
					err    error
				}{item.camera.StreamName, err}
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			closeServers(running)
			waitGroup.Wait()
			if r.cfg.Health != nil {
				r.cfg.Health.SetBridges(0, len(r.cfg.Registry.Cameras))
			}
			return nil
		case failure := <-errorsChannel:
			r.cfg.Logger.Error("camera bridge stopped", "camera", failure.camera, "err", failure.err)
		}
	}
}

func closeServers(servers []runningCamera) {
	for _, item := range servers {
		_ = item.server.Close()
	}
}
