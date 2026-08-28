// Command vm65-bridge exposes a Motorola VM65 camera as a local RTSP-over-TCP
// endpoint, tunneling every byte through the reconstructed Magic WEB2 relay.
// Point any RTSP player, go2rtc or Home Assistant at the local address and it
// reaches the camera as if it were on the LAN.
//
// The bridge does not perform the 5GenCare control flow: that flow is the one
// part of the chain not reconstructable from an x86 host. Its outputs — device
// id, SID, device token and relay control host — are supplied to the bridge in
// a credentials file (see -creds). Obtaining and refreshing those credentials
// is out of scope for this tool; see docs/bridge.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/app"
	"github.com/local/motorola-vm65-bridge/internal/bridge"
	"github.com/local/motorola-vm65-bridge/internal/buildinfo"
	appconfig "github.com/local/motorola-vm65-bridge/internal/config"
	"github.com/local/motorola-vm65-bridge/internal/health"
	"github.com/local/motorola-vm65-bridge/internal/mqttdiscovery"
)

// credsFile is the on-disk shape of the credentials. It mirrors the fields
// cmd/tunnelcheck already reads, so the same local file works for both.
type credsFile struct {
	DeviceID    uint32 `json:"device_id"`
	DeviceUDID  string `json:"device_udid"`
	DeviceName  string `json:"device_name"`
	Model       string `json:"model"`
	SID         string `json:"sid"`
	DeviceToken string `json:"device_token"`
	ControlHost string `json:"control_host"`
	ControlPort int    `json:"control_port"`
	TargetPort  int    `json:"target_port"`
}

func main() {
	for _, argument := range os.Args[1:] {
		if argument == "-version" || argument == "--version" {
			fmt.Println("vm65-bridge", buildinfo.String())
			return
		}
	}
	cfg, err := appconfig.Load(os.Args[1:], os.LookupEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid configuration:", err)
		os.Exit(2)
	}

	level := slog.LevelInfo
	if cfg.Verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	logger.Info("Motorola Nursery bridge", "version", buildinfo.String())
	logger.Debug("configuration loaded", "config", cfg.Redacted())

	if err := run(cfg, logger); err != nil {
		logger.Error("bridge exited with error", "err", err)
		os.Exit(1)
	}
}

func run(cfg appconfig.Config, logger *slog.Logger) error {
	credentials, err := loadCredentialSet(cfg)
	if err != nil {
		return err
	}
	registry, err := app.BuildRegistry(cfg.ListenAddr, credentials)
	if err != nil {
		return err
	}
	if len(registry.Cameras) == 0 {
		return errors.New("camera registry is empty")
	}
	primary := registry.Cameras[0].Credentials

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	healthState := health.NewState(time.Now())
	healthState.SetLive(true)
	defer healthState.SetLive(false)
	healthState.SetCredentialsReady(true)
	healthState.SetGo2RTC(cfg.Go2RTCRequired, !cfg.Go2RTCRequired)
	if cfg.Go2RTCRequired {
		go monitorGo2RTC(ctx, &http.Client{Timeout: 2 * time.Second}, cfg.Go2RTCURL, time.Second, healthState)
	}

	if cfg.StatusAddr != "" {
		startHealthServer(ctx, cfg.StatusAddr, health.NewHandler(healthState), logger)
	}
	var discovery *mqttdiscovery.Service
	publisher := &discoveryPublisher{}
	healthState.SetMQTT(cfg.MQTT.Host != "", false)
	if cfg.MQTT.Host != "" && primary.DeviceUDID != "" && cfg.StreamURL != "" {
		discovery = mqttdiscovery.NewService(mqttdiscovery.Config{
			Host:             cfg.MQTT.Host,
			Port:             cfg.MQTT.Port,
			Username:         cfg.MQTT.Username,
			Password:         cfg.MQTT.Password,
			DiscoveryPrefix:  cfg.MQTT.DiscoveryPrefix,
			ClientID:         "vm65-bridge-" + primary.DeviceUDID,
			Version:          buildinfo.String(),
			ConfigurationURL: cfg.SnapshotBase,
			OnConnectionChange: func(connected bool) {
				healthState.SetMQTT(true, connected)
			},
		})
		err = discovery.Start(ctx)
		if err != nil {
			logger.Warn("MQTT discovery unavailable", "err", err)
			healthState.SetLastError(health.ErrorBroker)
			discovery = nil
		} else {
			publisher.service = discovery
			if publishErr := publisher.publish(ctx, cfg, registry, logger, healthState); publishErr != nil {
				return publishErr
			}
			defer func() {
				shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
				defer cancel()
				_ = discovery.Close(shutdownContext)
			}()
		}
	}

	logger.Info("starting Motorola Nursery bridge", "listen", cfg.ListenAddr, "cameras", len(registry.Cameras), "control_host", primary.ControlHost)
	logCameraURLs(cfg, registry, logger)
	runtime := app.New(app.RuntimeConfig{Registry: registry, Logger: logger, Health: healthState})

	// SIGHUP swaps in freshly written credentials. Cameras whose credentials did
	// not change keep streaming, so the periodic refresh no longer costs every
	// viewer their picture.
	go watchForReload(ctx, cfg, runtime, publisher, logger, healthState)

	// Keep the diagnostic entities and per-camera availability in step with the
	// runtime, so Home Assistant shows which camera is down instead of leaving
	// every entity looking healthy.
	if publisher.service != nil {
		go mirrorStateToMQTT(ctx, publisher.service, runtime, healthState, 5*time.Second)
	}

	return runtime.Run(ctx)
}

// logCameraURLs prints the URLs a person needs to add the live video by hand.
// Home Assistant cannot discover an RTSP stream over MQTT, so these are the
// values that go into the camera integration.
func logCameraURLs(cfg appconfig.Config, registry app.Registry, logger *slog.Logger) {
	if cfg.StreamURL == "" {
		return
	}
	for index, camera := range registry.Cameras {
		streamName := camera.StreamName
		if index == 0 {
			streamName = registry.LegacyAlias
		}
		streamURL, err := discoveryStreamURL(cfg.StreamURL, camera.StreamName, index == 0)
		if err != nil {
			continue
		}
		logger.Info("camera stream ready",
			"camera", camera.StreamName,
			"stream_source", streamURL,
			"still_image_url", snapshotURL(cfg.SnapshotBase, streamName))
	}
}

// mirrorStateToMQTT publishes runtime counters and per-camera availability
// until ctx is cancelled.
func mirrorStateToMQTT(ctx context.Context, service *mqttdiscovery.Service, runtime *app.Runtime, healthState *health.State, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshot := healthState.Snapshot()
			_ = service.PublishStatus(ctx, mqttdiscovery.Status{
				ActiveSessions: snapshot.ActiveSessions,
				Reconnects:     snapshot.ReconnectsTotal,
			})
			for id, available := range runtime.CameraAvailability() {
				_ = service.SetCameraAvailable(ctx, id, available)
			}
		}
	}
}

// watchForReload applies a new credential file on SIGHUP until ctx is done.
func watchForReload(ctx context.Context, cfg appconfig.Config, runtime *app.Runtime, publisher *discoveryPublisher, logger *slog.Logger, healthState *health.State) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP)
	defer signal.Stop(signals)

	for {
		select {
		case <-ctx.Done():
			return
		case <-signals:
			logger.Info("reloading camera credentials")
			credentials, err := loadCredentialSet(cfg)
			if err != nil {
				logger.Error("credential reload failed; keeping the running cameras", "err", err)
				healthState.SetLastError(health.ErrorConfiguration)
				continue
			}
			registry, err := app.BuildRegistry(cfg.ListenAddr, credentials)
			if err != nil {
				logger.Error("credential reload produced an invalid registry; keeping the running cameras", "err", err)
				healthState.SetLastError(health.ErrorConfiguration)
				continue
			}
			if err := runtime.Reload(registry); err != nil {
				logger.Error("some cameras failed to restart after reload", "err", err)
				healthState.SetLastError(health.ErrorNetwork)
			}
			if err := publisher.publish(ctx, cfg, registry, logger, healthState); err != nil {
				logger.Error("MQTT discovery refresh failed", "err", err)
				healthState.SetLastError(health.ErrorBroker)
			}
			logger.Info("credential reload complete", "cameras", len(registry.Cameras))
		}
	}
}

// discoveryPublisher keeps the retained MQTT discovery topics in step with the
// running registry, including retiring cameras that disappeared from it.
type discoveryPublisher struct {
	service   *mqttdiscovery.Service
	published []string
}

func (p *discoveryPublisher) publish(ctx context.Context, cfg appconfig.Config, registry app.Registry, logger *slog.Logger, healthState *health.State) error {
	if p.service == nil {
		return nil
	}
	current := make(map[string]struct{}, len(registry.Cameras))
	for index, camera := range registry.Cameras {
		name := camera.Credentials.DeviceName
		if name == "" {
			name = "Motorola Nursery Camera"
		}
		streamName := camera.StreamName
		if index == 0 {
			// The first camera keeps the historical vm65 alias in go2rtc.
			streamName = registry.LegacyAlias
		}
		streamURL, err := discoveryStreamURL(cfg.StreamURL, camera.StreamName, index == 0)
		if err != nil {
			return err
		}
		if err := p.service.Upsert(ctx, mqttdiscovery.Camera{
			ID:          camera.Credentials.DeviceUDID,
			Name:        name,
			Model:       camera.Credentials.Model,
			StreamURL:   streamURL,
			SnapshotURL: snapshotURL(cfg.SnapshotBase, streamName),
		}); err != nil {
			logger.Warn("MQTT camera discovery unavailable", "camera", camera.StreamName, "err", err)
			healthState.SetLastError(health.ErrorBroker)
		}
		current[camera.Credentials.DeviceUDID] = struct{}{}
	}
	for _, id := range p.published {
		if _, kept := current[id]; kept {
			continue
		}
		if err := p.service.Remove(ctx, id); err != nil {
			logger.Warn("could not retire the discovery entry of a removed camera", "camera_id", id, "err", err)
		}
	}
	p.published = p.published[:0]
	for id := range current {
		p.published = append(p.published, id)
	}
	return nil
}

// snapshotURL points Home Assistant at go2rtc's still-frame endpoint so the
// camera entity has a thumbnail. Empty base means no snapshot is advertised.
func snapshotURL(base, streamName string) string {
	if base == "" || streamName == "" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + "/api/frame.jpeg"
	parsed.RawQuery = url.Values{"src": []string{streamName}}.Encode()
	return parsed.String()
}

func monitorGo2RTC(ctx context.Context, client *http.Client, endpoint string, interval time.Duration, state *health.State) {
	check := func() {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			state.SetGo2RTC(true, false)
			return
		}
		response, err := client.Do(request)
		if err != nil {
			state.SetGo2RTC(true, false)
			return
		}
		_ = response.Body.Close()
		state.SetGo2RTC(true, response.StatusCode >= 200 && response.StatusCode < 400)
	}
	check()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			state.SetGo2RTC(true, false)
			return
		case <-ticker.C:
			check()
		}
	}
}

func discoveryStreamURL(base, streamName string, legacy bool) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse discovery stream URL: %w", err)
	}
	if !legacy {
		parsed.Path = "/" + streamName
	}
	return parsed.String(), nil
}

// startHealthServer runs the JSON health endpoint until ctx is cancelled.
func startHealthServer(ctx context.Context, addr string, handler http.Handler, logger *slog.Logger) {
	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		logger.Info("health endpoint listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("health endpoint failed", "err", err)
		}
	}()
}

func loadCreds(path string) (bridge.Credentials, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return bridge.Credentials{}, fmt.Errorf("read credentials %q: %w", path, err)
	}
	var f credsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return bridge.Credentials{}, fmt.Errorf("parse credentials %q: %w", path, err)
	}
	return f.credentials()
}

func loadCredentialSet(cfg appconfig.Config) ([]bridge.Credentials, error) {
	if cfg.RegistryPath == "" {
		credentials, err := loadCreds(cfg.CredentialsPath)
		if err != nil {
			return nil, err
		}
		return []bridge.Credentials{credentials}, nil
	}
	raw, err := os.ReadFile(cfg.RegistryPath)
	if err != nil {
		return nil, fmt.Errorf("read camera registry %q: %w", cfg.RegistryPath, err)
	}
	var registry struct {
		Cameras []credsFile `json:"cameras"`
	}
	if err := json.Unmarshal(raw, &registry); err != nil {
		return nil, fmt.Errorf("parse camera registry %q: %w", cfg.RegistryPath, err)
	}
	if len(registry.Cameras) == 0 {
		return nil, errors.New("camera registry must contain at least one camera")
	}
	credentials := make([]bridge.Credentials, 0, len(registry.Cameras))
	for index, camera := range registry.Cameras {
		value, err := camera.credentials()
		if err != nil {
			return nil, fmt.Errorf("camera registry entry %d: %w", index, err)
		}
		credentials = append(credentials, value)
	}
	return credentials, nil
}

func (f credsFile) credentials() (bridge.Credentials, error) {
	if f.SID == "" || f.DeviceToken == "" || f.ControlHost == "" {
		return bridge.Credentials{}, errors.New("credentials file must set sid, device_token and control_host")
	}
	return bridge.Credentials{
		DeviceID:    f.DeviceID,
		DeviceUDID:  f.DeviceUDID,
		DeviceName:  f.DeviceName,
		Model:       f.Model,
		SID:         f.SID,
		DeviceToken: f.DeviceToken,
		ControlHost: f.ControlHost,
		ControlPort: f.ControlPort,
		TargetPort:  f.TargetPort,
	}, nil
}
