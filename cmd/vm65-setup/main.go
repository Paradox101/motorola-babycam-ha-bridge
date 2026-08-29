// Command vm65-setup pairs a Motorola Nursery account and writes bridge
// credentials. It remains non-interactive for Home Assistant compatibility.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
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
	"github.com/local/motorola-vm65-bridge/internal/fivegencare"
	"github.com/local/motorola-vm65-bridge/internal/health"
	"github.com/local/motorola-vm65-bridge/internal/pairing"
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

// go2RTCWebRTCConfig carries the candidates go2rtc advertises. Inside a
// container it otherwise offers only its own Docker address, which no phone or
// laptop on the network can reach, so WebRTC negotiates successfully and then
// never delivers a packet.
type go2RTCWebRTCConfig struct {
	Listen     string   `json:"listen"`
	Candidates []string `json:"candidates,omitempty"`
}

type go2RTCLogConfig struct {
	Level string `json:"level"`
}

type go2RTCConfig struct {
	Streams map[string][]string `json:"streams"`
	API     go2RTCListenConfig  `json:"api"`
	RTSP    go2RTCListenConfig  `json:"rtsp"`
	WebRTC  go2RTCWebRTCConfig  `json:"webrtc"`
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
	// webrtcCandidate is the address browsers should reach for WebRTC media,
	// normally the Home Assistant host and the published WebRTC port.
	webrtcCandidate string
	relayHost       string
	timeout         time.Duration
	verbose         bool

	// pairUI serves the pairing page until the account is paired. It is what
	// the add-on starts instead of failing when there is no session yet.
	pairUI string
	// statusAddr keeps the Supervisor watchdog fed while pairing is pending.
	// Without it the watchdog finds nothing listening and restarts the add-on
	// out from under the person filling in the form.
	statusAddr string
	// requestCode lets a run send a code by itself. The add-on turns it off:
	// with the Web UI, a code arrives because someone asked for one.
	requestCode bool
	// trustedCIDRs restricts who may reach the pairing page.
	trustedCIDRs []string
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
	flag.StringVar(&opts.webrtcCandidate, "webrtc-candidate", "", "host:port browsers use for WebRTC media, e.g. homeassistant.local:8556")
	flag.StringVar(&opts.relayHost, "control-host", "vrelay-de0.5gen.care", "Magic relay control host")
	flag.DurationVar(&opts.timeout, "timeout", 30*time.Second, "overall timeout for the account exchange")
	flag.BoolVar(&opts.verbose, "v", false, "verbose protocol diagnostics (never logs credentials)")
	flag.StringVar(&opts.pairUI, "pair-ui", "", "serve the pairing page on this address until the account is paired")
	flag.StringVar(&opts.statusAddr, "status", "", "optional health listen address used while pairing is pending")
	flag.BoolVar(&opts.requestCode, "request-code", true, "send an email code when none is pending")
	trustedCIDRs := flag.String("trusted-cidr", "", "comma-separated networks allowed to reach the pairing page (default: the Supervisor network)")
	showVersion := flag.Bool("version", false, "print the build version and exit")
	flag.Parse()
	opts.trustedCIDRs = parseTrustedCIDRs(*trustedCIDRs)

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
		Client:          client,
		Store:           fivegencare.NewStore(opts.statePath),
		Email:           opts.email,
		OTPCode:         opts.code,
		RelayHost:       opts.relayHost,
		AutoRequestCode: opts.requestCode,
	})

	cameras, err := loadCameras(provider, opts, logger)
	if err != nil {
		return err
	}
	if err := writeCameraFiles(opts.outPath, opts.registryPath, cameras); err != nil {
		return err
	}
	if opts.go2RTCPath != "" {
		registry, err := buildCameraRegistry(cameras)
		if err != nil {
			return err
		}
		if err := writeGo2RTCConfig(opts.go2RTCPath, registry, opts.go2RTCWebRTC, opts.webrtcCandidate); err != nil {
			return fmt.Errorf("write go2rtc configuration: %w", err)
		}
	}
	logger.Info("camera credentials written", "cameras", len(cameras))
	return nil
}

// loadCameras restores the account, and when that needs pairing either says so
// or — with -pair-ui — serves the page that fixes it and tries again.
func loadCameras(provider *fivegencare.Provider, opts options, logger *slog.Logger) ([]fivegencare.CameraCredentials, error) {
	cameras, err := restoreCameras(provider, opts.timeout)
	if err == nil {
		return cameras, nil
	}
	if !errors.Is(err, fivegencare.ErrPairingRequired) || opts.pairUI == "" {
		return nil, pairingError(err)
	}
	if err := servePairing(provider, opts, logger); err != nil {
		return nil, err
	}
	cameras, err = restoreCameras(provider, opts.timeout)
	if err != nil {
		return nil, pairingError(err)
	}
	return cameras, nil
}

func restoreCameras(provider *fivegencare.Provider, timeout time.Duration) ([]fivegencare.CameraCredentials, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cameras, err := provider.Restore(ctx)
	if err != nil {
		return nil, err
	}
	return cameras, nil
}

func pairingError(err error) error {
	if errors.Is(err, fivegencare.ErrPairingRequired) {
		message := strings.TrimPrefix(err.Error(), fivegencare.ErrPairingRequired.Error()+": ")
		return fmt.Errorf("PAIRING_REQUIRED: %s", message)
	}
	return fmt.Errorf("load cameras: %w", err)
}

// servePairing runs the pairing page until the account is paired or the process
// is asked to stop. The account exchange has its own timeout; the wait for a
// person to read their email does not.
func servePairing(provider *fivegencare.Provider, opts options, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	paired := make(chan struct{})
	server, err := pairing.NewServer(pairing.Config{
		Provider:       provider,
		TrustedCIDRs:   opts.trustedCIDRs,
		Logger:         logger.With("component", "pairing"),
		RequestTimeout: opts.timeout,
		OnPaired:       func() { close(paired) },
	})
	if err != nil {
		return err
	}

	// The watchdog polls while this is up. Report live but not ready: the
	// add-on is running and must not be restarted, and it is not serving
	// cameras yet either.
	healthState := health.NewState(time.Now())
	healthState.SetLive(true)
	healthState.SetCredentialsReady(false)
	if opts.statusAddr != "" {
		if err := listenAndServe(ctx, opts.statusAddr, health.NewHandler(healthState), logger, "health endpoint"); err != nil {
			return err
		}
	}
	if err := listenAndServe(ctx, opts.pairUI, server.Handler(), logger, "pairing page"); err != nil {
		return err
	}

	logger.Warn("this account is not paired yet; open the add-on Web UI in Home Assistant to finish it")
	select {
	case <-paired:
		logger.Info("account paired; continuing startup")
		return nil
	case <-ctx.Done():
		return errors.New("stopped before the account was paired")
	}
}

// listenAndServe binds before returning, so a port already in use is reported
// as a startup failure instead of a page that silently never answers.
func listenAndServe(ctx context.Context, addr string, handler http.Handler, logger *slog.Logger, name string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen for the %s: %w", name, err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		logger.Info(name+" listening", "addr", listener.Addr().String())
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(name+" failed", "err", err)
		}
	}()
	return nil
}

// parseTrustedCIDRs turns the flag value into the network list. Empty keeps the
// default; "any" disables the check.
func parseTrustedCIDRs(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.EqualFold(value, "any") {
		return []string{}
	}
	parts := strings.Split(value, ",")
	networks := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			networks = append(networks, part)
		}
	}
	return networks
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

// MJPEGSuffix names the companion stream that transcodes a camera to MJPEG.
//
// go2rtc does not transcode on its own for /api/stream.mjpeg: asked for one it
// answers "codecs not matched: video:H264 => video:JPEG" and nothing plays.
// Snapshots are different — /api/frame.jpeg does fall back to ffmpeg — which is
// why stills worked while the last-resort video transport did not.
const MJPEGSuffix = "-mjpeg"

func writeGo2RTCConfig(path string, registry cameraRegistry, enableWebRTC bool, webrtcCandidate string) error {
	config := go2RTCConfig{
		Streams: make(map[string][]string, len(registry.Cameras)+1),
		// go2rtc's API is unauthenticated and returns this very file, camera
		// access token and RTSP password included, so it stays on container
		// loopback. The bridge proxies it to Home Assistant Ingress after
		// checking the Supervisor's ingress user header.
		API:    go2RTCListenConfig{Listen: "127.0.0.1:1984"},
		RTSP:   go2RTCListenConfig{Listen: ":8555"},
		WebRTC: go2RTCWebRTCConfig{Listen: ":8556"},
		Log:    go2RTCLogConfig{Level: "info"},
	}
	if !enableWebRTC {
		config.WebRTC.Listen = ""
	} else if webrtcCandidate != "" {
		config.WebRTC.Candidates = []string{webrtcCandidate}
	}
	for index, camera := range registry.Cameras {
		if camera.ListenAddr == "" {
			return fmt.Errorf("camera %q has no listen address", camera.StreamName)
		}
		userInfo := url.UserPassword(camera.RTSPUser, camera.RTSPPass).String()
		source := fmt.Sprintf("rtsp://%s@%s/owner/streaming?accessToken=%s#rtsp/tcp#backchannel=0",
			userInfo, camera.ListenAddr, url.QueryEscape(camera.AccessToken))
		config.Streams[camera.StreamName] = []string{source}
		config.Streams[camera.StreamName+MJPEGSuffix] = []string{"ffmpeg:" + camera.StreamName + "#video=mjpeg"}
		if index == 0 {
			config.Streams["vm65"] = []string{source}
			config.Streams["vm65"+MJPEGSuffix] = []string{"ffmpeg:vm65#video=mjpeg"}
		}
	}
	return fivegencare.WritePrivateJSON(path, config)
}
