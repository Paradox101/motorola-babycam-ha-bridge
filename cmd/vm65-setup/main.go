// Command vm65-setup pairs a Motorola Nursery account and writes bridge
// credentials. It remains non-interactive for Home Assistant compatibility.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/app"
	"github.com/local/motorola-vm65-bridge/internal/bridge"
	"github.com/local/motorola-vm65-bridge/internal/buildinfo"
	"github.com/local/motorola-vm65-bridge/internal/fivegencare"
)

type cameraRegistryEntry struct {
	fivegencare.CameraCredentials
	StreamName string `json:"stream_name"`
	ListenAddr string `json:"listen_addr"`
}

type cameraRegistry struct {
	Cameras []cameraRegistryEntry `json:"cameras"`
}

type go2RTCListenConfig struct {
	Listen string `json:"listen"`
}

type go2RTCLogConfig struct {
	Level string `json:"level"`
}

type go2RTCConfig struct {
	Streams map[string][]string `json:"streams"`
	API     go2RTCListenConfig  `json:"api"`
	RTSP    go2RTCListenConfig  `json:"rtsp"`
	WebRTC  go2RTCListenConfig  `json:"webrtc"`
	Log     go2RTCLogConfig     `json:"log"`
}

// options are the validated inputs of one setup run.
type options struct {
	email        string
	code         string
	statePath    string
	outPath      string
	registryPath string
	go2RTCPath   string
	go2RTCWebRTC bool
	relayHost    string
	timeout      time.Duration
	verbose      bool
}

func main() {
	var opts options
	flag.StringVar(&opts.email, "email", "", "Motorola Nursery account email")
	flag.StringVar(&opts.code, "otp-code", os.Getenv("VM65_OTP_CODE"), "deprecated: use VM65_OTP_CODE")
	flag.StringVar(&opts.statePath, "state", "/data/5gencare-session.json", "persistent pairing state")
	flag.StringVar(&opts.outPath, "output", "/data/creds.json", "legacy first-camera credentials output")
	flag.StringVar(&opts.registryPath, "registry", "/data/cameras.json", "all compatible camera credentials output")
	flag.StringVar(&opts.go2RTCPath, "go2rtc-config", "", "optional generated go2rtc configuration output")
	flag.BoolVar(&opts.go2RTCWebRTC, "go2rtc-webrtc", true, "enable go2rtc API and WebRTC listeners")
	flag.StringVar(&opts.relayHost, "control-host", "vrelay-de0.5gen.care", "Magic relay control host")
	flag.DurationVar(&opts.timeout, "timeout", 30*time.Second, "overall timeout for the account exchange")
	flag.BoolVar(&opts.verbose, "v", false, "verbose protocol diagnostics (never logs credentials)")
	showVersion := flag.Bool("version", false, "print the build version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("vm65-setup", buildinfo.String())
		return
	}
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(opts options) error {
	level := slog.LevelInfo
	if opts.verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	logger.Info("pairing Motorola Nursery account", "version", buildinfo.String())

	if opts.timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	client := fivegencare.Client{Logger: logger.With("component", "5gencare"), Timeout: opts.timeout}
	provider := fivegencare.NewProvider(fivegencare.ProviderConfig{
		Client:    client,
		Store:     fivegencare.NewStore(opts.statePath),
		Email:     opts.email,
		OTPCode:   opts.code,
		RelayHost: opts.relayHost,
	})
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	cameras, err := provider.Restore(ctx)
	if err != nil {
		if errors.Is(err, fivegencare.ErrPairingRequired) {
			message := strings.TrimPrefix(err.Error(), fivegencare.ErrPairingRequired.Error()+": ")
			return fmt.Errorf("PAIRING_REQUIRED: %s", message)
		}
		return fmt.Errorf("load cameras: %w", err)
	}
	if err := writeCameraFiles(opts.outPath, opts.registryPath, cameras); err != nil {
		return err
	}
	if opts.go2RTCPath != "" {
		registry, err := buildCameraRegistry(cameras)
		if err != nil {
			return err
		}
		if err := writeGo2RTCConfig(opts.go2RTCPath, registry, opts.go2RTCWebRTC); err != nil {
			return fmt.Errorf("write go2rtc configuration: %w", err)
		}
	}
	logger.Info("camera credentials written", "cameras", len(cameras))
	return nil
}

func writeCameraFiles(outPath, registryPath string, cameras []fivegencare.CameraCredentials) error {
	if len(cameras) == 0 {
		return errors.New("cannot write an empty camera registry")
	}
	registry, err := buildCameraRegistry(cameras)
	if err != nil {
		return err
	}
	if err := fivegencare.WritePrivateJSON(registryPath, registry); err != nil {
		return fmt.Errorf("write camera registry: %w", err)
	}
	if err := fivegencare.WritePrivateJSON(outPath, cameras[0]); err != nil {
		return fmt.Errorf("write legacy camera credentials: %w", err)
	}
	return nil
}

func buildCameraRegistry(cameras []fivegencare.CameraCredentials) (cameraRegistry, error) {
	bridgeCredentials := make([]bridge.Credentials, 0, len(cameras))
	byUDID := make(map[string]fivegencare.CameraCredentials, len(cameras))
	for _, camera := range cameras {
		bridgeCredentials = append(bridgeCredentials, bridge.Credentials{
			DeviceID: camera.DeviceID, DeviceUDID: camera.DeviceUDID,
			DeviceName: camera.DeviceName, Model: camera.Model,
		})
		byUDID[camera.DeviceUDID] = camera
	}
	runtimeRegistry, err := app.BuildRegistry("127.0.0.1:8554", bridgeCredentials)
	if err != nil {
		return cameraRegistry{}, fmt.Errorf("build camera registry: %w", err)
	}
	entries := make([]cameraRegistryEntry, 0, len(runtimeRegistry.Cameras))
	for _, camera := range runtimeRegistry.Cameras {
		entries = append(entries, cameraRegistryEntry{
			CameraCredentials: byUDID[camera.Credentials.DeviceUDID],
			StreamName:        camera.StreamName,
			ListenAddr:        camera.ListenAddr,
		})
	}
	return cameraRegistry{Cameras: entries}, nil
}

func writeGo2RTCConfig(path string, registry cameraRegistry, enableWebRTC bool) error {
	config := go2RTCConfig{
		Streams: make(map[string][]string, len(registry.Cameras)+1),
		// go2rtc's API is unauthenticated and returns this very file, camera
		// access token and RTSP password included, so it stays on container
		// loopback. The bridge proxies it to Home Assistant Ingress after
		// checking the Supervisor's ingress user header.
		API:    go2RTCListenConfig{Listen: "127.0.0.1:1984"},
		RTSP:   go2RTCListenConfig{Listen: ":8555"},
		WebRTC: go2RTCListenConfig{Listen: ":8556"},
		Log:    go2RTCLogConfig{Level: "info"},
	}
	if !enableWebRTC {
		config.WebRTC.Listen = ""
	}
	for index, camera := range registry.Cameras {
		if camera.ListenAddr == "" {
			return fmt.Errorf("camera %q has no listen address", camera.StreamName)
		}
		userInfo := url.UserPassword(camera.RTSPUser, camera.RTSPPass).String()
		source := fmt.Sprintf("rtsp://%s@%s/owner/streaming?accessToken=%s#rtsp/tcp#backchannel=0",
			userInfo, camera.ListenAddr, url.QueryEscape(camera.AccessToken))
		config.Streams[camera.StreamName] = []string{source}
		if index == 0 {
			config.Streams["vm65"] = []string{source}
		}
	}
	return fivegencare.WritePrivateJSON(path, config)
}
