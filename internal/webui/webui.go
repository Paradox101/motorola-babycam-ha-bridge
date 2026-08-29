// Package webui serves the add-on's own camera page behind Home Assistant
// Ingress.
//
// The Web UI used to be go2rtc's page, proxied. That page is a debugging tool
// for a media server: it lists stream names, not cameras, and says nothing
// about the things that actually go wrong here — a relay tunnel that dropped, a
// camera whose control link is down, the temperature it is reporting. It also
// exposes editing controls that this add-on has to refuse, so half of what it
// offers does not work.
//
// This page shows the cameras instead: live video, the state of each link, and
// the URLs worth copying. go2rtc stays behind it, reached only for the media
// endpoints the player needs.
package webui

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"

	"github.com/local/motorola-vm65-bridge/internal/ingress"
)

// Camera is one camera as the page shows it.
type Camera struct {
	// ID is the stable device identifier.
	ID string `json:"id"`
	// Name is the name from the Nursery app.
	Name string `json:"name"`
	// Model is descriptive only; nothing filters on it.
	Model string `json:"model,omitempty"`
	// Stream is the go2rtc stream name, which is what the player asks for.
	Stream string `json:"stream"`
	// MJPEGStream is the companion stream that transcodes to MJPEG. go2rtc
	// will not transcode for a plain MJPEG request, so the last-resort
	// transport has to name this one instead.
	MJPEGStream string `json:"mjpeg_stream,omitempty"`
	// StreamURL is the RTSP URL for an external player.
	StreamURL string `json:"stream_url,omitempty"`
	// Serving reports whether this camera's bridge is accepting connections
	// right now. A camera that is restarting is not.
	Serving bool `json:"serving"`
	// ActiveSessions counts live viewers of this camera.
	ActiveSessions int64 `json:"active_sessions"`
	// TemperatureCelsius is the last reading, when the camera supports it.
	TemperatureCelsius *float64 `json:"temperature_celsius,omitempty"`
}

// Overview is the whole page state in one response.
type Overview struct {
	Cameras []Camera `json:"cameras"`
	// Version is the running build.
	Version string `json:"version"`
	// Ready reports whether every camera is serving.
	Ready bool `json:"ready"`
	// Go2RTCReady reports whether the media server answers; without it there
	// is no video to play and the page should say so rather than show a black
	// rectangle.
	Go2RTCReady bool `json:"go2rtc_ready"`
	// MQTTConnected reports the broker link when MQTT discovery is on.
	MQTTConnected bool `json:"mqtt_connected"`
	// MQTTEnabled reports whether MQTT discovery is configured at all.
	MQTTEnabled bool `json:"mqtt_enabled"`
	// Reconnects counts bridge restarts since start.
	Reconnects uint64 `json:"reconnects"`
	// UptimeSeconds is how long the add-on has been running.
	UptimeSeconds int64 `json:"uptime_seconds"`
}

// Source is what the page reads and acts on.
type Source interface {
	// Overview reports the current state of every camera.
	Overview() Overview
	// Restart drops one camera's bridge so the supervisor rebuilds it. It is
	// the one repair worth offering: a tunnel that went bad recovers from it,
	// and it touches nothing else.
	Restart(id string) error
}

type Config struct {
	Source Source
	// TrustedCIDRs restricts who may connect. Nil selects the Supervisor
	// network; an explicitly empty slice disables the check.
	TrustedCIDRs []string
	// Media handles the go2rtc endpoints the player needs. Requests to it have
	// already passed the same authentication as the page.
	Media http.Handler
	// Snapshot serves still images to the page.
	Snapshot http.Handler
	Logger   *slog.Logger
}

type Server struct {
	source        Source
	authenticator *ingress.Authenticator
	media         http.Handler
	snapshot      http.Handler
	logger        *slog.Logger
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.Source == nil {
		return nil, errors.New("the web UI needs a camera source")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	authenticator, err := ingress.NewAuthenticator(cfg.TrustedCIDRs, cfg.Logger)
	if err != nil {
		return nil, err
	}
	return &Server{
		source:        cfg.Source,
		authenticator: authenticator,
		media:         cfg.Media,
		snapshot:      cfg.Snapshot,
		logger:        cfg.Logger,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cameras", s.handleOverview)
	mux.HandleFunc("/api/cameras/restart", s.handleRestart)
	if s.snapshot != nil {
		// Deliberately not /snapshot: that path stays the token-protected one
		// Home Assistant fetches, and it is mounted ahead of this page.
		mux.Handle("/camera-still", s.snapshot)
	}
	if s.media != nil {
		// go2rtc's own endpoints, for the player only. The proxy behind this
		// still refuses anything but a read of a configured stream.
		//
		// All three transports are here on purpose. WebRTC media does not go
		// through Ingress at all — the browser reaches the host's UDP port
		// directly — so it works on the local network and not through Nabu
		// Casa or a reverse proxy. MSE runs over the WebSocket this proxy
		// forwards, so it works wherever the page itself loads, and MJPEG is
		// the last resort that needs nothing but an <img>.
		for _, path := range []string{
			"/api/webrtc", "/api/ws", "/api/frame.jpeg",
			"/api/stream.mp4", "/api/stream", "/api/stream.mjpeg",
		} {
			mux.Handle(path, s.media)
		}
	}
	mux.HandleFunc("/", s.handlePage)
	return s.authenticator.Wrap(mux)
}

func (s *Server) handlePage(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	// media-src and img-src stay 'self': the video and the stills come from
	// this same origin, through the proxy.
	writer.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; "+
			"connect-src 'self'; media-src 'self' blob:; img-src 'self' data:; form-action 'none'")
	if request.Method == http.MethodHead {
		return
	}
	_, _ = writer.Write([]byte(page))
}

func (s *Server) handleOverview(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", "GET")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	overview := s.source.Overview()
	sort.Slice(overview.Cameras, func(i, j int) bool {
		if overview.Cameras[i].Name != overview.Cameras[j].Name {
			return overview.Cameras[i].Name < overview.Cameras[j].Name
		}
		return overview.Cameras[i].ID < overview.Cameras[j].ID
	})
	writeJSON(writer, http.StatusOK, overview)
}

func (s *Server) handleRestart(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "POST")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// JSON only, so another site cannot post here without a preflight this
	// server never answers.
	if mediaType := request.Header.Get("Content-Type"); mediaType != "application/json" {
		http.Error(writer, "expected a JSON body", http.StatusUnsupportedMediaType)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorBody{Error: "The request could not be read."})
		return
	}
	if err := s.source.Restart(body.ID); err != nil {
		s.logger.Warn("camera restart failed", "err", err)
		writeJSON(writer, http.StatusNotFound, errorBody{Error: "That camera is not running here."})
		return
	}
	s.logger.Info("camera bridge restarted from the Web UI")
	writeJSON(writer, http.StatusAccepted, map[string]string{"status": "restarting"})
}

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
