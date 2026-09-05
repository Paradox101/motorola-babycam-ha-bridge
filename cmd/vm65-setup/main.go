// Command vm65-setup pairs a Motorola Nursery account and writes bridge
// credentials. It remains non-interactive for Home Assistant compatibility.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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
	// overlayFont is the TrueType font the burnt-in overlay draws with. Empty
	// disables the overlay, which is the default: burning text into the picture
	// costs a full re-encode of every frame.
	overlayFont string
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
	flag.StringVar(&opts.overlayFont, "overlay-font", "", "TrueType font used to burn a clock and the camera name into the picture; empty leaves the picture untouched")
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
		go2rtcOptions := go2RTCOptions{
			EnableWebRTC:    opts.go2RTCWebRTC,
			WebRTCCandidate: opts.webrtcCandidate,
			OverlayFont:     opts.overlayFont,
			Logger:          logger,
		}
		if err := writeGo2RTCConfig(opts.go2RTCPath, registry, go2rtcOptions); err != nil {
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

// buildOverlays writes each camera's name where drawtext can read it and
// returns the filter per camera, keyed by stream name. An empty result means
// no overlay: either it was never asked for, or the filter did not survive the
// render check and the picture is better off untouched.
//
// It is all or nothing on purpose. A configuration where one camera carries a
// timestamp and another does not is worse than one where none of them do: the
// missing stamp is the one you would trust.
func buildOverlays(configPath string, registry cameraRegistry, opts go2RTCOptions) (map[string]string, error) {
	if opts.OverlayFont == "" {
		return nil, nil
	}
	log := opts.logger()
	if _, err := os.Stat(opts.OverlayFont); err != nil {
		log.Warn("overlay disabled: the font is not readable", "font", opts.OverlayFont, "err", err)
		return nil, nil
	}
	verify := opts.VerifyOverlay
	if verify == nil {
		verify = ffmpegCanRender
	}

	filters := make(map[string]string, len(registry.Cameras))
	for _, camera := range registry.Cameras {
		textPath := overlayTextPath(configPath, camera.StreamName)
		if err := os.WriteFile(textPath, []byte(overlayName(camera)), 0o600); err != nil {
			return nil, fmt.Errorf("write overlay text for %s: %w", camera.StreamName, err)
		}
		filter := overlayFilter(opts.OverlayFont, textPath)
		if err := verify(filter); err != nil {
			log.Warn("overlay disabled: this build of ffmpeg cannot render it",
				"camera", camera.StreamName, "err", err)
			return nil, nil
		}
		filters[camera.StreamName] = filter
	}
	log.Info("burning a clock and the camera name into the picture",
		"cameras", len(filters), "font", opts.OverlayFont)
	return filters, nil
}

// overlayTextPath is where a camera's name is kept for drawtext to read. The
// name never enters the filter itself: quoting it there is a trap that fails
// silently — an apostrophe swallows the rest of the word rather than erroring.
func overlayTextPath(configPath, streamName string) string {
	return filepath.Join(filepath.Dir(configPath), "overlay-"+streamName+".txt")
}

// overlayName is what the camera is called on screen.
func overlayName(camera cameraRegistryEntry) string {
	name := strings.TrimSpace(camera.DeviceName)
	if name == "" {
		name = camera.StreamName
	}
	// One line only: drawtext renders a newline as a second line, which would
	// climb out of the corner it is anchored in.
	name = strings.NewReplacer("\r", " ", "\n", " ").Replace(name)
	if len([]rune(name)) > overlayNameLimit {
		name = string([]rune(name)[:overlayNameLimit])
	}
	return name
}

const overlayNameLimit = 48

// overlayFilter draws the clock in the top-left corner and the camera name in
// the bottom-right, the way a camera with its own on-screen display does.
//
// The escaping is exact and was arrived at by rendering it: the colon that
// separates %{localtime} from its format needs one backslash to survive the
// filtergraph parser, while the colons inside the clock need three — one pair
// collapsing to a backslash that drawtext's own expansion then reads as an
// escaped colon. Two backslashes give "Stray %", none give "requires at most 1
// arguments", and both of those only appear on stderr while ffmpeg exits 0.
func overlayFilter(fontPath, textPath string) string {
	const style = ":fontsize=h/22:fontcolor=white:shadowcolor=black:shadowx=2:shadowy=2"
	font := "fontfile=" + escapeFilterValue(fontPath)
	clock := "drawtext=" + font +
		`:text='%{localtime\:%d/%m/%Y %H\\\:%M\\\:%S}'` +
		style + ":x=12:y=10"
	// expansion=none keeps the name literal: a % or a : in it is drawn, not
	// interpreted.
	name := "drawtext=" + font +
		":textfile=" + escapeFilterValue(textPath) + ":expansion=none" +
		style + ":x=w-tw-12:y=h-th-10"
	return clock + "," + name
}

// escapeFilterValue protects a path from the filtergraph parser, which reads a
// colon as the next option and a backslash as an escape. A double quote is
// removed outright: go2rtc splits the command it builds on quotes, so one here
// would cut the filter in half.
func escapeFilterValue(value string) string {
	value = strings.ReplaceAll(value, `"`, "")
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, ":", `\:`)
}

// ffmpegCanRender renders one frame through filter so a filter this ffmpeg
// cannot parse is caught here, not by a media server that then restarts on it
// forever. A drawtext ffmpeg dislikes still exits 0 while complaining on
// stderr, so any output at all counts as a refusal.
func ffmpegCanRender(filter string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=black:s=320x180:d=1",
		"-vf", filter, "-frames:v", "1", "-f", "null", "-")
	output, err := command.CombinedOutput()
	complaint := strings.TrimSpace(string(output))
	switch {
	case err != nil && complaint != "":
		return fmt.Errorf("%w: %s", err, complaint)
	case err != nil:
		return err
	case complaint != "":
		return errors.New(complaint)
	}
	return nil
}

// SourceSuffix names the untouched camera stream an overlaid stream reads
// from. It exists so every overlaid name is a consumer of one source, and
// therefore of one relay session, instead of opening its own tunnel.
const SourceSuffix = "-source"

// go2RTCOptions carries what the generated media-server configuration needs
// beyond the cameras themselves.
type go2RTCOptions struct {
	EnableWebRTC    bool
	WebRTCCandidate string

	// OverlayFont enables the burnt-in clock and camera name when set to a
	// TrueType font path. Empty, the default, leaves every frame untouched.
	OverlayFont string

	// VerifyOverlay renders one frame through a generated filter. Nil selects
	// ffmpegCanRender. A filter this rejects disables the overlay rather than
	// reaching the media server, which would otherwise restart on it forever.
	VerifyOverlay func(filter string) error

	Logger *slog.Logger
}

func (o go2RTCOptions) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func writeGo2RTCConfig(path string, registry cameraRegistry, opts go2RTCOptions) error {
	enableWebRTC, webrtcCandidate := opts.EnableWebRTC, opts.WebRTCCandidate
	overlays, err := buildOverlays(path, registry, opts)
	if err != nil {
		return err
	}
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

		// With an overlay every published name transcodes the one untouched
		// source stream, so the two names of the first camera still cost a
		// single relay session between them.
		published := source
		if filter, ok := overlays[camera.StreamName]; ok {
			config.Streams[camera.StreamName+SourceSuffix] = []string{source}
			// The quotes are go2rtc's, not Go's: it splits the command on
			// spaces and strips one layer of quotes without unescaping
			// anything, so the filter has to arrive verbatim.
			published = fmt.Sprintf(`ffmpeg:%s%s#video=h264#audio=copy#raw=-vf "%s"`,
				camera.StreamName, SourceSuffix, filter)
		}
		config.Streams[camera.StreamName] = []string{published}
		config.Streams[camera.StreamName+MJPEGSuffix] = []string{"ffmpeg:" + camera.StreamName + "#video=mjpeg"}
		if index == 0 {
			config.Streams["vm65"] = []string{published}
			config.Streams["vm65"+MJPEGSuffix] = []string{"ffmpeg:vm65#video=mjpeg"}
		}
	}
	return fivegencare.WritePrivateJSON(path, config)
}
