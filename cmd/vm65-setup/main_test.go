package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if err := writeGo2RTCConfig(path, registry, go2RTCOptions{EnableWebRTC: true, WebRTCCandidate: "homeassistant.local:8556"}); err != nil {
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
	// Two cameras plus the legacy alias, each with an MJPEG companion.
	if len(config.Streams) != 6 || config.Streams["vm65"][0] != config.Streams["nursery"][0] {
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
	if err := writeGo2RTCConfig(path, registry, go2RTCOptions{}); err != nil {
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

// pairingAccount stands in for the Motorola account service.
type pairingAccount struct {
	requested int
}

func (p *pairingAccount) RequestOTP(context.Context, string, string) (fivegencare.OTPChallenge, error) {
	p.requested++
	return fivegencare.OTPChallenge{UserID: 42, Domain: "shard.example"}, nil
}

func (p *pairingAccount) LoginOTP(_ context.Context, _ fivegencare.OTPChallenge, _, _, code string) (fivegencare.Session, error) {
	if code != "123456" {
		return fivegencare.Session{}, errors.New("rejected")
	}
	return fivegencare.Session{UserID: 42, SessionToken: "token", SessionID: "session", Domain: "shard.example"}, nil
}

func (p *pairingAccount) Devices(context.Context, fivegencare.Session) ([]fivegencare.Device, error) {
	return []fivegencare.Device{{
		ID: 1, UDID: "camera-a", Name: "Nursery", Model: "VM65CONNECT",
		SID: "sid-a", DeviceToken: "device-token",
	}}, nil
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func postJSON(t *testing.T, url, body string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Remote-User-Id", "01HQ")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(payload)
}

// The whole first-run path: no session, the page comes up instead of the
// process failing, and pairing through it produces the credential files.
func TestPairingThroughTheWebUIProducesCredentials(t *testing.T) {
	directory := t.TempDir()
	address := freeAddress(t)
	opts := options{
		statePath:    filepath.Join(directory, "state.json"),
		outPath:      filepath.Join(directory, "creds.json"),
		registryPath: filepath.Join(directory, "cameras.json"),
		go2RTCPath:   filepath.Join(directory, "go2rtc.yaml"),
		go2RTCWebRTC: true,
		relayHost:    "relay.example.test",
		timeout:      10 * time.Second,
		pairUI:       address,
		requestCode:  false,
	}
	account := &pairingAccount{}
	provider := fivegencare.NewProvider(fivegencare.ProviderConfig{
		Client:    account,
		Store:     fivegencare.NewStore(opts.statePath),
		RelayHost: opts.relayHost,
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	type result struct {
		cameras []fivegencare.CameraCredentials
		err     error
	}
	done := make(chan result, 1)
	go func() {
		cameras, err := loadCameras(provider, opts, logger)
		done <- result{cameras, err}
	}()

	base := "http://" + address
	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("the pairing page never came up")
		}
		request, err := http.NewRequest(http.MethodGet, base+"/api/pairing/status", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Remote-User-Id", "01HQ")
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			_ = response.Body.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if status, body := postJSON(t, base+"/api/pairing/code", `{"email":"owner@example.test"}`); status != http.StatusOK {
		t.Fatalf("code request status = %d (%s)", status, body)
	}
	if account.requested != 1 {
		t.Fatalf("requested codes = %d, want 1", account.requested)
	}
	if status, _ := postJSON(t, base+"/api/pairing/verify", `{"code":"000000"}`); status != http.StatusBadRequest {
		t.Fatalf("a wrong code returned %d, want %d", status, http.StatusBadRequest)
	}
	if status, body := postJSON(t, base+"/api/pairing/verify", `{"code":"123456"}`); status != http.StatusOK {
		t.Fatalf("verify status = %d (%s)", status, body)
	}

	select {
	case outcome := <-done:
		if outcome.err != nil {
			t.Fatalf("loadCameras: %v", outcome.err)
		}
		if len(outcome.cameras) != 1 || outcome.cameras[0].DeviceUDID != "camera-a" {
			t.Fatalf("cameras = %#v", outcome.cameras)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pairing completed but startup did not continue")
	}

	// A wrong code must not have cost the user a second email.
	if account.requested != 1 {
		t.Fatalf("requested codes = %d, want the one the user asked for", account.requested)
	}
}

func TestParseTrustedCIDRs(t *testing.T) {
	if parseTrustedCIDRs("") != nil {
		t.Fatal("an empty value must keep the default network")
	}
	if networks := parseTrustedCIDRs("any"); networks == nil || len(networks) != 0 {
		t.Fatalf(`"any" = %#v, want an empty non-nil slice`, networks)
	}
	if networks := parseTrustedCIDRs(" 172.30.32.0/23 , 10.0.0.0/8 "); len(networks) != 2 || networks[0] != "172.30.32.0/23" {
		t.Fatalf("networks = %#v", networks)
	}
}

// Inside a container go2rtc advertises only its own Docker address, which no
// browser on the network can reach: WebRTC then negotiates successfully and
// never delivers a packet. The candidate is what makes it reachable.
func TestGeneratedConfigAdvertisesAReachableWebRTCCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	registry := cameraRegistry{Cameras: []cameraRegistryEntry{{
		CameraCredentials: fivegencare.CameraCredentials{DeviceUDID: "a", AccessToken: "token"},
		StreamName:        "vm65-connect",
		ListenAddr:        "127.0.0.1:8554",
	}}}
	if err := writeGo2RTCConfig(path, registry, go2RTCOptions{EnableWebRTC: true, WebRTCCandidate: "homeassistant.local:8556"}); err != nil {
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
	if len(config.WebRTC.Candidates) != 1 || config.WebRTC.Candidates[0] != "homeassistant.local:8556" {
		t.Fatalf("candidates = %#v", config.WebRTC.Candidates)
	}

	// External mode has no bundled WebRTC listener, so there is nothing to
	// advertise and a candidate would be a lie.
	if err := writeGo2RTCConfig(path, registry, go2RTCOptions{WebRTCCandidate: "homeassistant.local:8556"}); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	config = go2RTCConfig{}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if config.WebRTC.Listen != "" || len(config.WebRTC.Candidates) != 0 {
		t.Fatalf("webrtc = %#v", config.WebRTC)
	}
}

// go2rtc answers a /api/stream.mjpeg request for an H264 camera with
// "codecs not matched" and plays nothing, so the last-resort transport needs a
// stream that actually transcodes.
func TestGeneratedConfigCarriesAnMJPEGStreamPerCamera(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	registry := cameraRegistry{Cameras: []cameraRegistryEntry{{
		CameraCredentials: fivegencare.CameraCredentials{DeviceUDID: "a", AccessToken: "token"},
		StreamName:        "vm65-connect",
		ListenAddr:        "127.0.0.1:8554",
	}}}
	if err := writeGo2RTCConfig(path, registry, go2RTCOptions{EnableWebRTC: true}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config go2RTCConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"vm65-connect" + MJPEGSuffix: "ffmpeg:vm65-connect#video=mjpeg",
		"vm65" + MJPEGSuffix:         "ffmpeg:vm65#video=mjpeg",
	} {
		sources := config.Streams[name]
		if len(sources) != 1 || sources[0] != want {
			t.Fatalf("stream %q = %#v, want %q", name, sources, want)
		}
	}
	// The camera streams themselves must stay untouched.
	if sources := config.Streams["vm65"]; len(sources) != 1 || !strings.HasPrefix(sources[0], "rtsp://") {
		t.Fatalf("vm65 = %#v", sources)
	}
}
