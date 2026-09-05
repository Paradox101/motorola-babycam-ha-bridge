// Package pairing serves the account pairing page behind Home Assistant
// Ingress.
//
// Pairing used to cost four steps in two places: fill in the email option,
// start the add-on and watch it fail, find the instruction in the log, paste
// the emailed code into a second option and start again — after which the code
// stayed behind in the configuration as a dead value. The add-on used its own
// crash as a user interface.
//
// This page replaces that. The Supervisor has already authenticated a Home
// Assistant user by the time a request arrives here, so the page can send the
// code and complete pairing in one visit, with no restart and no YAML.
package pairing

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/local/motorola-vm65-bridge/internal/fivegencare"
	"github.com/local/motorola-vm65-bridge/internal/i18n"
	"github.com/local/motorola-vm65-bridge/internal/ingress"
)

// Provider is the pairing half of fivegencare.Provider.
type Provider interface {
	Status() (fivegencare.PairingStatus, error)
	RequestCode(context.Context, string) error
	SubmitCode(context.Context, string) error
}

type Config struct {
	Provider Provider
	// TrustedCIDRs restricts who may connect. Nil selects the Supervisor
	// network; an explicitly empty slice disables the check.
	TrustedCIDRs []string
	// Logger receives failures. Codes and addresses are never logged.
	Logger *slog.Logger
	// OnPaired runs once, after pairing succeeds, so the caller can stop
	// serving and get on with starting the cameras.
	OnPaired func()
	// RequestTimeout bounds one call to the Motorola account service.
	RequestTimeout time.Duration
	// Language forces the language the page and its refusals are written in.
	// Empty follows the browser: the Supervisor tells an add-on who is asking,
	// never which language they set in Home Assistant.
	Language i18n.Language
}

// DefaultRequestTimeout bounds one account exchange.
const DefaultRequestTimeout = 30 * time.Second

type Server struct {
	provider       Provider
	authenticator  *ingress.Authenticator
	logger         *slog.Logger
	requestTimeout time.Duration
	language       i18n.Language

	once     sync.Once
	onPaired func()
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.Provider == nil {
		return nil, errors.New("pairing needs a provider")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	authenticator, err := ingress.NewAuthenticator(cfg.TrustedCIDRs, cfg.Logger)
	if err != nil {
		return nil, err
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	return &Server{
		provider:       cfg.Provider,
		authenticator:  authenticator,
		logger:         cfg.Logger,
		requestTimeout: cfg.RequestTimeout,
		language:       cfg.Language,
		onPaired:       cfg.OnPaired,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/pairing/status", s.handleStatus)
	mux.HandleFunc("/api/pairing/code", s.handleRequestCode)
	mux.HandleFunc("/api/pairing/verify", s.handleVerify)
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
	// The page is self-contained on purpose: an add-on setup screen that needs
	// the internet to render is one that cannot be used to fix a broken setup.
	writer.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; form-action 'none'")
	// The page varies by language, and the language comes from the request, so
	// a cache in front of this must not serve one reader's language to another.
	writer.Header().Set("Vary", "Accept-Language")
	if request.Method == http.MethodHead {
		return
	}
	_, _ = writer.Write([]byte(pageFor(s.pageLanguage(request))))
}

// pageLanguage is the configured language, or the browser's preference when
// none is configured.
func (s *Server) pageLanguage(request *http.Request) i18n.Language {
	if s.language != "" {
		return s.language
	}
	return i18n.Match(request.Header.Get("Accept-Language"))
}

// say returns one message in the language of the request. The page prints what
// this server sends verbatim, so a refusal has to arrive already translated.
func (s *Server) say(request *http.Request, key string) string {
	return i18n.Text(catalogs, s.pageLanguage(request), key)
}

func (s *Server) handleStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", "GET")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status, err := s.provider.Status()
	if err != nil {
		s.fail(writer, s.say(request, "errState"), err, http.StatusInternalServerError)
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (s *Server) handleRequestCode(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if !s.decode(writer, request, &body) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if err := s.provider.RequestCode(ctx, body.Email); err != nil {
		// The address is the user's own and the failure is theirs to read, but
		// the log keeps neither it nor anything derived from it.
		s.logger.Warn("pairing code request failed", "err", err)
		s.fail(writer, s.say(request, "errRequestCode"), err, http.StatusBadGateway)
		return
	}
	s.logger.Info("pairing code requested from the Web UI")
	s.writeStatus(writer, request)
}

func (s *Server) handleVerify(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if !s.decode(writer, request, &body) {
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), s.requestTimeout)
	defer cancel()
	if err := s.provider.SubmitCode(ctx, body.Code); err != nil {
		s.logger.Warn("pairing code rejected", "err", err)
		message := s.say(request, "errCodeWrong")
		if errors.Is(err, fivegencare.ErrNoChallenge) {
			message = s.say(request, "errCodeExpired")
		}
		s.fail(writer, message, err, http.StatusBadRequest)
		return
	}
	s.logger.Info("account paired from the Web UI")
	s.writeStatus(writer, request)
	if s.onPaired != nil {
		s.once.Do(func() { go s.onPaired() })
	}
}

func (s *Server) writeStatus(writer http.ResponseWriter, request *http.Request) {
	status, err := s.provider.Status()
	if err != nil {
		s.fail(writer, s.say(request, "errState"), err, http.StatusInternalServerError)
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

// decode reads a JSON body. Requiring JSON is also what keeps another site from
// posting here: a cross-origin form cannot set this content type without a
// preflight, and no CORS headers are ever sent.
func (s *Server) decode(writer http.ResponseWriter, request *http.Request, value any) bool {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", "POST")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	contentType := request.Header.Get("Content-Type")
	if mediaType, _, _ := strings.Cut(contentType, ";"); strings.TrimSpace(mediaType) != "application/json" {
		http.Error(writer, "expected a JSON body", http.StatusUnsupportedMediaType)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorBody{Error: s.say(request, "errBadRequest")})
		return false
	}
	return true
}

type errorBody struct {
	Error string `json:"error"`
}

func (s *Server) fail(writer http.ResponseWriter, message string, err error, status int) {
	if err != nil && status >= http.StatusInternalServerError {
		s.logger.Error("pairing request failed", "err", err)
	}
	writeJSON(writer, status, errorBody{Error: message})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
