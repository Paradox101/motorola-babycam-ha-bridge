package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/local/motorola-vm65-bridge/internal/fivegencare"
)

func overlayRegistry() cameraRegistry {
	return cameraRegistry{Cameras: []cameraRegistryEntry{
		{
			CameraCredentials: fivegencare.CameraCredentials{
				RTSPUser: "owner", RTSPPass: "secret", AccessToken: "token",
				DeviceName: "Baby's kamer: 100%",
			},
			StreamName: "nursery", ListenAddr: "127.0.0.1:8554",
		},
	}}
}

func readConfig(t *testing.T, path string) go2RTCConfig {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config go2RTCConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("config is not valid JSON-compatible YAML: %v", err)
	}
	return config
}

// TestOverlayIsOffByDefault pins the promise the add-on option makes: without
// it asked for, not one frame is re-encoded and the configuration is the one
// that shipped before the overlay existed.
func TestOverlayIsOffByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	if err := writeGo2RTCConfig(path, overlayRegistry(), go2RTCOptions{EnableWebRTC: true}); err != nil {
		t.Fatal(err)
	}
	config := readConfig(t, path)
	if !strings.HasPrefix(config.Streams["nursery"][0], "rtsp://") {
		t.Fatalf("the camera stream was not left untouched: %q", config.Streams["nursery"][0])
	}
	if _, ok := config.Streams["nursery"+SourceSuffix]; ok {
		t.Fatal("a source stream was generated without an overlay being asked for")
	}
}

// TestOverlayTranscodesOneSourcePerCamera covers the shape that matters: both
// published names of the first camera read the same untouched source, so the
// overlay costs one relay session, not two.
func TestOverlayTranscodesOneSourcePerCamera(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go2rtc.yaml")
	var rendered []string
	options := go2RTCOptions{
		EnableWebRTC:  true,
		OverlayFont:   writeFontStub(t, dir),
		VerifyOverlay: func(filter string) error { rendered = append(rendered, filter); return nil },
	}
	if err := writeGo2RTCConfig(path, overlayRegistry(), options); err != nil {
		t.Fatal(err)
	}
	config := readConfig(t, path)

	if !strings.HasPrefix(config.Streams["nursery"+SourceSuffix][0], "rtsp://") {
		t.Fatalf("the source stream is not the raw camera: %#v", config.Streams)
	}
	published := config.Streams["nursery"][0]
	if !strings.HasPrefix(published, "ffmpeg:nursery"+SourceSuffix+"#") {
		t.Fatalf("the published stream does not read the source: %q", published)
	}
	if config.Streams["vm65"][0] != published {
		t.Fatalf("the legacy alias opens its own tunnel: %q", config.Streams["vm65"][0])
	}
	// Without this go2rtc emits -an and the camera's sound is gone.
	if !strings.Contains(published, "#audio=copy") {
		t.Fatalf("audio was not carried through: %q", published)
	}
	if len(rendered) != 1 {
		t.Fatalf("the filter was rendered %d times before being used", len(rendered))
	}
}

// TestOverlayNameIsWrittenBesideTheConfig proves the camera name never enters
// the filter syntax: it goes to a file drawtext reads literally, so quoting can
// neither swallow it nor break the stream.
func TestOverlayNameIsWrittenBesideTheConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go2rtc.yaml")
	options := go2RTCOptions{
		OverlayFont:   writeFontStub(t, dir),
		VerifyOverlay: func(string) error { return nil },
	}
	if err := writeGo2RTCConfig(path, overlayRegistry(), options); err != nil {
		t.Fatal(err)
	}
	name, err := os.ReadFile(overlayTextPath(path, "nursery"))
	if err != nil {
		t.Fatal(err)
	}
	if string(name) != "Baby's kamer: 100%" {
		t.Fatalf("overlay name = %q", name)
	}
	published := readConfig(t, path).Streams["nursery"][0]
	if strings.Contains(published, "Baby") {
		t.Fatalf("the name was inlined into the filter after all: %q", published)
	}
}

// TestOverlayFallsBackWhenFFmpegRefusesIt is the safety net: a filter this
// ffmpeg will not render must cost the picture nothing at all.
func TestOverlayFallsBackWhenFFmpegRefusesIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go2rtc.yaml")
	options := go2RTCOptions{
		OverlayFont:   writeFontStub(t, dir),
		VerifyOverlay: func(string) error { return errors.New("Stray % near 'S}'") },
	}
	if err := writeGo2RTCConfig(path, overlayRegistry(), options); err != nil {
		t.Fatal(err)
	}
	config := readConfig(t, path)
	if !strings.HasPrefix(config.Streams["nursery"][0], "rtsp://") {
		t.Fatalf("a filter ffmpeg refused still reached the media server: %q", config.Streams["nursery"][0])
	}
}

// TestOverlayFallsBackWithoutAFont covers the other half of that: an option
// turned on in a build whose font package is missing must not break video.
func TestOverlayFallsBackWithoutAFont(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go2rtc.yaml")
	options := go2RTCOptions{
		OverlayFont:   filepath.Join(dir, "absent.ttf"),
		VerifyOverlay: func(string) error { return nil },
	}
	if err := writeGo2RTCConfig(path, overlayRegistry(), options); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(readConfig(t, path).Streams["nursery"][0], "rtsp://") {
		t.Fatal("a missing font did not disable the overlay")
	}
}

// TestOverlayFilterRendersInFFmpeg is the test the escaping actually needs.
// drawtext reports a filter it dislikes on stderr and still exits 0, so this
// asserts on the output, exactly as ffmpegCanRender does.
func TestOverlayFilterRendersInFFmpeg(t *testing.T) {
	font := findSystemFont(t)
	dir := t.TempDir()
	textPath := filepath.Join(dir, "overlay-nursery.txt")
	if err := os.WriteFile(textPath, []byte("Baby's kamer: 100%"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ffmpegCanRender(overlayFilter(font, textPath)); err != nil {
		t.Fatalf("ffmpeg would not render the generated overlay: %v", err)
	}
	// The escaping is the fragile part: a colon one level under-escaped makes
	// %{localtime} take too many arguments, one level over gives "Stray %".
	broken := strings.Replace(overlayFilter(font, textPath), `%H\\\:`, `%H\:`, 1)
	if err := ffmpegCanRender(broken); err == nil {
		t.Fatal("ffmpegCanRender accepted a filter drawtext complains about")
	}
}

// writeFontStub stands in for a font file: buildOverlays only has to find one,
// and the tests that use it never reach ffmpeg.
func writeFontStub(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "font.ttf")
	if err := os.WriteFile(path, []byte("not really a font"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func findSystemFont(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	candidates := []string{
		"/usr/share/fonts/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Skip("no DejaVu font installed")
	return ""
}
