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
	"syscall"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/app"
	"github.com/local/motorola-vm65-bridge/internal/bridge"
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
	healthState.SetMQTT(cfg.MQTT.Host != "", false)
	if cfg.MQTT.Host != "" && primary.DeviceUDID != "" && cfg.StreamURL != "" {
		discovery = mqttdiscovery.NewService(mqttdiscovery.Config{
			Host:            cfg.MQTT.Host,
			Port:            cfg.MQTT.Port,
			Username:        cfg.MQTT.Username,
			Password:        cfg.MQTT.Password,
			DiscoveryPrefix: cfg.MQTT.DiscoveryPrefix,
			ClientID:        "vm65-bridge-" + primary.DeviceUDID,
			OnConnectionChange: func(connected bool) {
				healthState.SetMQTT(true, connected)
			},
		})
		err = discovery.Start(ctx)
		if err != nil {
			logger.Warn("MQTT discovery unavailable", "err", err)
			healthState.SetLastError(health.ErrorBroker)
		} else {
			for index, camera := range registry.Cameras {
				name := camera.Credentials.DeviceName
				if name == "" {
					name = "Motorola Nursery Camera"
				}
				streamURL, urlErr := discoveryStreamURL(cfg.StreamURL, camera.StreamName, index == 0)
				if urlErr != nil {
					return urlErr
				}
				if publishErr := discovery.Upsert(ctx, mqttdiscovery.Camera{
					ID:        camera.Credentials.DeviceUDID,
					Name:      name,
					Model:     camera.Credentials.Model,
					StreamURL: streamURL,
				}); publishErr != nil {
					logger.Warn("MQTT camera discovery unavailable", "camera", camera.StreamName, "err", publishErr)
					healthState.SetLastError(health.ErrorBroker)
				}
			}
			defer func() {
				shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
				defer cancel()
				_ = discovery.Close(shutdownContext)
			}()
		}
	}

	logger.Info("starting Motorola Nursery bridge", "listen", cfg.ListenAddr, "cameras", len(registry.Cameras), "control_host", primary.ControlHost)
	runtime := app.New(app.RuntimeConfig{Registry: registry, Logger: logger, Health: healthState})
	return runtime.Run(ctx)
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
