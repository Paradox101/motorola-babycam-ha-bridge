package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/local/motorola-vm65-bridge/internal/fivegencare"
)

func TestWriteCameraFilesPreservesLegacyFirstCameraAndFullRegistry(t *testing.T) {
	directory := t.TempDir()
	legacyPath := filepath.Join(directory, "creds.json")
	registryPath := filepath.Join(directory, "cameras.json")
	cameras := []fivegencare.CameraCredentials{
		{DeviceUDID: "a", Model: "VM65CONNECT"},
		{DeviceUDID: "b", Model: "MBP99"},
	}
	if err := writeCameraFiles(legacyPath, registryPath, cameras); err != nil {
		t.Fatal(err)
	}
	var legacy fivegencare.CameraCredentials
	raw, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.DeviceUDID != "a" {
		t.Fatalf("legacy camera = %q, want a", legacy.DeviceUDID)
	}
	var registry cameraRegistry
	raw, err = os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &registry); err != nil {
		t.Fatal(err)
	}
	if len(registry.Cameras) != 2 || registry.Cameras[1].Model != "MBP99" ||
		registry.Cameras[0].StreamName != "camera-a" || registry.Cameras[1].ListenAddr != "127.0.0.1:9554" {
		t.Fatalf("registry = %#v", registry)
	}
}

func TestWriteGo2RTCConfigPublishesEveryCameraAndLegacyAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	registry := cameraRegistry{Cameras: []cameraRegistryEntry{
		{
			CameraCredentials: fivegencare.CameraCredentials{
				RTSPUser: "owner", RTSPPass: "p@ss", AccessToken: "token one",
			},
			StreamName: "nursery", ListenAddr: "127.0.0.1:8554",
		},
		{
			CameraCredentials: fivegencare.CameraCredentials{
				RTSPUser: "owner", RTSPPass: "secret", AccessToken: "token-two",
			},
			StreamName: "play-room", ListenAddr: "127.0.0.1:9554",
		},
	}}
	if err := writeGo2RTCConfig(path, registry, true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config go2RTCConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("config is not valid JSON-compatible YAML: %v", err)
	}
	if len(config.Streams) != 3 || config.Streams["vm65"][0] != config.Streams["nursery"][0] {
		t.Fatalf("streams = %#v", config.Streams)
	}
	if !strings.Contains(config.Streams["nursery"][0], "p%40ss") ||
		!strings.Contains(config.Streams["nursery"][0], "accessToken=token+one") {
		t.Fatalf("credentials were not URL encoded: %q", config.Streams["nursery"][0])
	}
}

func TestWriteGo2RTCConfigCanDisableBundledWebRTC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	registry := cameraRegistry{Cameras: []cameraRegistryEntry{{
		CameraCredentials: fivegencare.CameraCredentials{RTSPUser: "u", RTSPPass: "p", AccessToken: "t"},
		StreamName:        "nursery",
		ListenAddr:        "127.0.0.1:8554",
	}}}
	if err := writeGo2RTCConfig(path, registry, false); err != nil {
		t.Fatal(err)
	}
	var config go2RTCConfig
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	// The API stays on container loopback in both modes: it is unauthenticated
	// and returns the camera access token and RTSP password.
	if config.API.Listen != "127.0.0.1:1984" || config.WebRTC.Listen != "" || config.RTSP.Listen != ":8555" {
		t.Fatalf("external media config = %#v", config)
	}
}
