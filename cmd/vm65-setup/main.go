// Command vm65-setup pairs a Motorola Nursery account and writes bridge
// credentials. It remains non-interactive for Home Assistant compatibility.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/app"
	"github.com/local/motorola-vm65-bridge/internal/bridge"
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

func main() {
	email := flag.String("email", "", "Motorola Nursery account email")
	code := flag.String("otp-code", os.Getenv("VM65_OTP_CODE"), "deprecated: use VM65_OTP_CODE")
	statePath := flag.String("state", "/data/5gencare-session.json", "persistent pairing state")
	outPath := flag.String("output", "/data/creds.json", "legacy first-camera credentials output")
	registryPath := flag.String("registry", "/data/cameras.json", "all compatible camera credentials output")
	go2RTCPath := flag.String("go2rtc-config", "", "optional generated go2rtc configuration output")
	go2RTCWebRTC := flag.Bool("go2rtc-webrtc", true, "enable go2rtc API and WebRTC listeners")
	relayHost := flag.String("control-host", "vrelay-de0.5gen.care", "Magic relay control host")
	flag.Parse()
	if err := run(*email, *code, *statePath, *outPath, *registryPath, *go2RTCPath, *go2RTCWebRTC, *relayHost); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(email, code, statePath, outPath, registryPath, go2RTCPath string, go2RTCWebRTC bool, relayHost string) error {
	client := fivegencare.Client{Debug: true}
	provider := fivegencare.NewProvider(fivegencare.ProviderConfig{
		Client:    client,
		Store:     fivegencare.NewStore(statePath),
		Email:     email,
		OTPCode:   code,
		RelayHost: relayHost,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cameras, err := provider.Restore(ctx)
	if err != nil {
		if errors.Is(err, fivegencare.ErrPairingRequired) {
			message := strings.TrimPrefix(err.Error(), fivegencare.ErrPairingRequired.Error()+": ")
			return fmt.Errorf("PAIRING_REQUIRED: %s", message)
		}
		return fmt.Errorf("load cameras: %w", err)
	}
	if err := writeCameraFiles(outPath, registryPath, cameras); err != nil {
		return err
	}
	if go2RTCPath != "" {
		registry, err := buildCameraRegistry(cameras)
		if err != nil {
			return err
		}
		if err := writeGo2RTCConfig(go2RTCPath, registry, go2RTCWebRTC); err != nil {
			return fmt.Errorf("write go2rtc configuration: %w", err)
		}
	}
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
		API:     go2RTCListenConfig{Listen: ":1984"},
		RTSP:    go2RTCListenConfig{Listen: ":8555"},
		WebRTC:  go2RTCListenConfig{Listen: ":8556"},
		Log:     go2RTCLogConfig{Level: "info"},
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
