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

	appconfig "github.com/local/motorola-vm65-bridge/internal/config"
	"github.com/local/motorola-vm65-bridge/internal/health"
)

func TestLoadCredentialSetReadsEveryRegistryCamera(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cameras.json")
	data := `{"cameras":[
        {"device_id":1,"device_udid":"a","device_name":"Room A","model":"VM65CONNECT","sid":"sid-a","device_token":"token-a","control_host":"relay"},
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
