package ingress

import (
	"log/slog"
	"net/http"

	"github.com/local/motorola-vm65-bridge/internal/netguard"
)

// Authenticator is the gate every listener the Supervisor reaches goes through.
// It is deliberately shared: the Web UI proxy and the pairing UI have the same
// two requirements, and a second copy of this check is a second thing to get
// wrong.
type Authenticator struct {
	guard  *netguard.Guard
	logger *slog.Logger
}

// NewAuthenticator builds the gate. Nil trustedCIDRs selects the Supervisor
// network; an explicitly empty slice disables the network check.
func NewAuthenticator(trustedCIDRs []string, logger *slog.Logger) (*Authenticator, error) {
	guard, err := netguard.New(trustedCIDRs)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Authenticator{guard: guard, logger: logger}, nil
}

// Allow reports whether one request may proceed, and writes the refusal itself
// when it may not.
func (a *Authenticator) Allow(writer http.ResponseWriter, request *http.Request) bool {
	if !a.guard.Allow(request.RemoteAddr) {
		a.logger.Warn("rejected a request from an untrusted address",
			"remote", request.RemoteAddr, "path", request.URL.Path)
		http.Error(writer, "forbidden", http.StatusForbidden)
		return false
	}
	if request.Header.Get(UserIDHeader) == "" {
		// Without this header the request did not come through Ingress, so no
		// Home Assistant session was ever checked.
		http.Error(writer, "unauthorized: open this add-on from the Home Assistant sidebar", http.StatusUnauthorized)
		return false
	}
	return true
}

// Wrap applies Allow in front of next.
func (a *Authenticator) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !a.Allow(writer, request) {
			return
		}
		SetSecurityHeaders(writer.Header())
		next.ServeHTTP(writer, request)
	})
}

// SetSecurityHeaders applies the response headers every add-on page carries.
func SetSecurityHeaders(header http.Header) {
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	// These pages only ever run inside the Home Assistant frame.
	header.Set("X-Frame-Options", "SAMEORIGIN")
}
