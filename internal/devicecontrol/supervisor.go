package devicecontrol

import (
	"context"
	"sync"
	"time"
)

// Sink receives the small temperature state machine without coupling the
// control protocol to MQTT.
type Sink interface {
	SetTemperatureSupported(context.Context, string, bool) error
	SetTemperatureAvailable(context.Context, string, bool) error
	PublishTemperature(context.Context, string, float64) error
}

type SupervisorConfig struct {
	Client       Client
	Sink         Sink
	PollInterval time.Duration
	RetryDelay   time.Duration
}

type Supervisor struct {
	ctx    context.Context
	cancel context.CancelFunc
	config SupervisorConfig

	mu      sync.Mutex
	workers map[string]worker
}

type worker struct {
	camera Camera
	cancel context.CancelFunc
}

func NewSupervisor(parent context.Context, config SupervisorConfig) *Supervisor {
	if config.PollInterval <= 0 {
		config.PollInterval = 30 * time.Second
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = 10 * time.Second
	}
	ctx, cancel := context.WithCancel(parent)
	return &Supervisor{ctx: ctx, cancel: cancel, config: config, workers: make(map[string]worker)}
}

func (s *Supervisor) Reconcile(cameras []Camera) {
	s.mu.Lock()
	defer s.mu.Unlock()
	wanted := make(map[string]Camera, len(cameras))
	for _, camera := range cameras {
		if camera.ID == "" || camera.DeviceID == 0 || camera.Token == "" || camera.Host == "" || camera.Port < 1 {
			continue
		}
		wanted[camera.ID] = camera
	}
	for id, existing := range s.workers {
		camera, keep := wanted[id]
		if keep && camera == existing.camera {
			delete(wanted, id)
			continue
		}
		existing.cancel()
		delete(s.workers, id)
	}
	for id, camera := range wanted {
		workerCtx, cancel := context.WithCancel(s.ctx)
		s.workers[id] = worker{camera: camera, cancel: cancel}
		go s.run(workerCtx, camera)
	}
}

func (s *Supervisor) Close() {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, worker := range s.workers {
		worker.cancel()
	}
	s.workers = make(map[string]worker)
}

func (s *Supervisor) run(ctx context.Context, camera Camera) {
	defer s.config.Sink.SetTemperatureAvailable(context.Background(), camera.ID, false)
	for {
		_ = s.config.Sink.SetTemperatureAvailable(ctx, camera.ID, false)
		connection, err := s.config.Client.Connect(ctx, camera)
		if err == nil {
			supported, capabilityErr := connection.SupportsTemperature(ctx)
			if capabilityErr == nil && !supported {
				_ = connection.Close()
				_ = s.config.Sink.SetTemperatureSupported(ctx, camera.ID, false)
				return
			}
			if capabilityErr == nil {
				_ = s.config.Sink.SetTemperatureSupported(ctx, camera.ID, true)
				for {
					temperature, readErr := connection.Temperature(ctx)
					if readErr != nil {
						break
					}
					_ = s.config.Sink.PublishTemperature(ctx, camera.ID, temperature)
					if !wait(ctx, s.config.PollInterval) {
						_ = connection.Close()
						return
					}
				}
			}
			_ = connection.Close()
		}
		if !wait(ctx, s.config.RetryDelay) {
			return
		}
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
