// Package ingress serves the add-on Web UI behind Home Assistant Ingress.
//
// go2rtc has no authentication of its own. Publishing its port on the host
// therefore hands anyone on the network the camera credentials — `/api/config`
// returns the generated go2rtc configuration, RTSP password and camera access
// token included — and a way to run commands inside the container, because
// go2rtc creates a stream from any `src=` it is given and `exec:` is a valid
// source scheme.
//
// This proxy is what the Supervisor talks to instead. It enforces three things
// before a single byte reaches go2rtc:
//
//  1. the request arrives over the Supervisor network, so an unpublished port
//     stays unreachable from the LAN even if it is published by hand later;
//  2. the request carries the `X-Remote-User-Id` header the Supervisor adds to
//     every ingress request, so an authenticated Home Assistant user is behind
//     it (https://developers.home-assistant.io/docs/apps/security/);
//  3. the request only asks for something a viewer needs — configured streams,
//     never a new source and never the configuration.
package ingress

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/local/motorola-vm65-bridge/internal/netguard"
)

// UserIDHeader is the header the Supervisor sets on every ingress request. Its
// presence is what tells the add-on a Home Assistant session was authenticated;
// the add-on never sees, and never needs, the session token itself.
const UserIDHeader = "X-Remote-User-Id"

// remoteUserHeaders are set by the Supervisor and must never be believed when a
// client sends them: without stripping, anyone able to reach the port could
// forge an authenticated user. They are dropped on the way in and re-added from
// the values this handler validated.
var remoteUserHeaders = []string{
	UserIDHeader,
	"X-Remote-User-Name",
	"X-Remote-User-Display-Name",
}

// blockedPaths never reach go2rtc. `/api/config` and `/api/streams.dot` expose
// the generated configuration, which carries the camera's RTSP password and
// access token; the rest change the running process.
var blockedPaths = map[string]bool{
	"/api/config":      true,
	"/api/exit":        true,
	"/api/restart":     true,
	"/api/streams.dot": true,
}

type Config struct {
	// Upstream is the go2rtc base URL, normally http://127.0.0.1:1984/.
	Upstream string
	// Streams are the stream names a request may name in `src`. Anything else
	// is refused, which is what stops `src=exec:...` from reaching go2rtc.
	Streams []string
	// TrustedCIDRs restricts who may connect. Nil selects
	// netguard.SupervisorCIDRs; an explicitly empty slice disables the check.
	TrustedCIDRs []string
	// RequireUser demands the Supervisor's ingress user header. It defaults to
	// true and only tests turn it off.
	RequireUser *bool
	Logger      *slog.Logger
}

// NewHandler builds the authenticating reverse proxy.
func NewHandler(cfg Config) (http.Handler, error) {
	target, err := url.Parse(cfg.Upstream)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return nil, errors.New("ingress upstream must be an absolute http or https URL")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	guard, err := netguard.New(cfg.TrustedCIDRs)
	if err != nil {
		return nil, err
	}
	streams := make(map[string]bool, len(cfg.Streams))
	for _, name := range cfg.Streams {
		if name != "" {
			streams[name] = true
		}
	}
	requireUser := true
	if cfg.RequireUser != nil {
		requireUser = *cfg.RequireUser
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.Host = target.Host
			// SetXForwarded replaces whatever the client sent, so a forged
			// X-Forwarded-For cannot reach go2rtc either.
			request.SetXForwarded()
			for _, header := range remoteUserHeaders {
				if value := request.In.Header.Get(header); value != "" {
					request.Out.Header.Set(header, value)
				} else {
					request.Out.Header.Del(header)
				}
			}
		},
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, err error) {
			cfg.Logger.Warn("web UI upstream unavailable", "err", err)
			http.Error(writer, "web UI is not available", http.StatusBadGateway)
		},
	}

	handler := &authProxy{
		proxy:       proxy,
		guard:       guard,
		streams:     streams,
		requireUser: requireUser,
		logger:      cfg.Logger,
	}
	return handler, nil
}

type authProxy struct {
	proxy       http.Handler
	guard       *netguard.Guard
	streams     map[string]bool
	requireUser bool
	logger      *slog.Logger
}

func (a *authProxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if !a.guard.Allow(request.RemoteAddr) {
		a.logger.Warn("rejected a Web UI request from an untrusted address", "remote", request.RemoteAddr)
		http.Error(writer, "forbidden", http.StatusForbidden)
		return
	}
	if a.requireUser && request.Header.Get(UserIDHeader) == "" {
		// Reaching the add-on without this header means the request did not
		// come through Ingress, so no Home Assistant session was checked.
		http.Error(writer, "unauthorized: open this add-on from the Home Assistant sidebar", http.StatusUnauthorized)
		return
	}
	if reason, ok := a.refuse(request); !ok {
		a.logger.Warn("refused a Web UI request", "path", request.URL.Path, "method", request.Method, "reason", reason)
		http.Error(writer, "forbidden: "+reason, http.StatusForbidden)
		return
	}
	setSecurityHeaders(writer.Header())
	a.proxy.ServeHTTP(writer, request)
}

// refuse decides whether one request may reach go2rtc, and says why not.
func (a *authProxy) refuse(request *http.Request) (string, bool) {
	path := normalizePath(request.URL.Path)
	if blockedPaths[path] {
		return "this go2rtc endpoint is not exposed through the add-on", false
	}
	// Only reads pass. Every go2rtc write endpoint either edits the running
	// configuration or adds a source, and the add-on owns both.
	switch request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	case http.MethodPost:
		// WebRTC and MSE negotiation are posts against an existing stream.
		if path != "/api/webrtc" && path != "/api/stream" {
			return "the Web UI is read-only in this add-on", false
		}
	default:
		return "the Web UI is read-only in this add-on", false
	}
	for _, value := range srcValues(request.URL.Query()) {
		if !a.streams[value] {
			// A src that is not a configured stream is a request for go2rtc to
			// create one, and "exec:" is a source scheme.
			return "unknown stream", false
		}
	}
	return "", true
}

// srcValues collects every query parameter go2rtc reads a source from.
func srcValues(query url.Values) []string {
	var values []string
	for _, key := range []string{"src", "source", "dst"} {
		values = append(values, query[key]...)
	}
	return values
}

// normalizePath makes the block list independent of a trailing slash or of a
// path the Supervisor did not clean.
func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	cleaned := strings.TrimSuffix(path, "/")
	if cleaned == "" {
		return "/"
	}
	return cleaned
}

func setSecurityHeaders(header http.Header) {
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	// The Web UI only ever runs inside the Home Assistant frame.
	header.Set("X-Frame-Options", "SAMEORIGIN")
}
