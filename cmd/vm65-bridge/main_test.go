package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/app"
	"github.com/local/motorola-vm65-bridge/internal/bridge"
	appconfig "github.com/local/motorola-vm65-bridge/internal/config"
	"github.com/local/motorola-vm65-bridge/internal/health"
)

func TestLoadCredentialSetReadsEveryRegistryCamera(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cameras.json")
	data := `{"cameras":[
		{"device_id":1,"device_udid":"a","device_name":"Room A","model":"VM65CONNECT","sid":"sid-a","device_token":"token-a","control_host":"relay","device_api_host":"shard.example","device_api_port":2288},
		{"device_id":2,"device_udid":"b","device_name":"Room B","model":"MBP99","sid":"sid-b","device_token":"token-b","control_host":"relay"}
	]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cameras, err := loadCredentialSet(appconfig.Config{RegistryPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(cameras) != 2 || cameras[1].Model != "MBP99" {
		t.Fatalf("cameras = %#v", cameras)
	}
	if cameras[0].DeviceAPIHost != "shard.example" || cameras[0].DeviceAPIPort != 2288 {
		t.Fatalf("device API endpoint = %s:%d", cameras[0].DeviceAPIHost, cameras[0].DeviceAPIPort)
	}
}

func TestTemperatureCamerasKeepOnlyCompleteControlCredentials(t *testing.T) {
	registry := app.Registry{Cameras: []app.Camera{
		{Credentials: bridge.Credentials{DeviceID: 7, DeviceUDID: "camera-a", DeviceToken: "token-a", DeviceAPIHost: "shard.example", DeviceAPIPort: 2288}},
		{Credentials: bridge.Credentials{DeviceID: 8, DeviceUDID: "camera-b", DeviceToken: "token-b"}},
	}}
	cameras := temperatureCameras(registry)
	if len(cameras) != 1 {
		t.Fatalf("temperature cameras = %#v", cameras)
	}
	if cameras[0].ID != "camera-a" || cameras[0].DeviceID != 7 || cameras[0].Host != "shard.example" || cameras[0].Port != 2288 {
		t.Fatalf("temperature camera = %#v", cameras[0])
	}
}

func TestMonitorGo2RTCUpdatesReadiness(t *testing.T) {
	var ready atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	state := health.NewState(time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go monitorGo2RTC(ctx, server.Client(), server.URL, 5*time.Millisecond, state)

	waitForGo2RTC(t, state, false)
	ready.Store(true)
	waitForGo2RTC(t, state, true)
}

func waitForGo2RTC(t *testing.T, state *health.State, want bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if state.Snapshot().Go2RTCReady == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("go2rtc ready = %t, want %t", state.Snapshot().Go2RTCReady, want)
}

func TestSnapshotURLTargetsTheGo2RTCStillFrameEndpoint(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		stream string
		want   string
	}{
		{"plain base", "http://homeassistant.local:1984", "vm65", "http://homeassistant.local:1984/api/frame.jpeg?src=vm65"},
		{"trailing slash", "http://homeassistant.local:1984/", "vm65", "http://homeassistant.local:1984/api/frame.jpeg?src=vm65"},
		{"name needing escaping", "http://10.0.0.5:1984", "baby room", "http://10.0.0.5:1984/api/frame.jpeg?src=baby+room"},
		{"no base configured", "", "vm65", ""},
		{"no stream name", "http://10.0.0.5:1984", "", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := snapshotURL(testCase.base, testCase.stream); got != testCase.want {
				t.Fatalf("snapshotURL(%q, %q) = %q, want %q", testCase.base, testCase.stream, got, testCase.want)
			}
		})
	}
}
