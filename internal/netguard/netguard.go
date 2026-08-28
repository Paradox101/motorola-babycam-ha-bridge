// Package netguard restricts an HTTP listener to the networks it is meant to
// serve.
//
// The add-on's Web UI and snapshot endpoints are reached over the Docker
// network the Supervisor, Home Assistant and the add-ons share. Neither port is
// published to the host, but a listener that would answer anyone if it were
// published is a trap waiting for the next configuration change, so the
// restriction is enforced in the process as well.
package netguard

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// SupervisorCIDRs is the Docker network Home Assistant, the Supervisor and the
// add-ons share, plus loopback for in-container diagnostics.
var SupervisorCIDRs = []string{"172.30.32.0/23", "127.0.0.0/8", "::1/128"}

// Guard reports whether a peer address is inside the allowed networks. The zero
// Guard allows everything, which is what an explicitly empty CIDR list means.
type Guard struct {
	networks []*net.IPNet
}

// New parses the allowed networks. A nil slice selects SupervisorCIDRs; an
// empty, non-nil slice disables the check.
func New(cidrs []string) (*Guard, error) {
	if cidrs == nil {
		cidrs = SupervisorCIDRs
	}
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, value := range cidrs {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("parse trusted network %q: %w", value, err)
		}
		networks = append(networks, network)
	}
	return &Guard{networks: networks}, nil
}

// Allow reports whether remoteAddr, in host:port or bare host form, is trusted.
func (g *Guard) Allow(remoteAddr string) bool {
	if g == nil || len(g.networks) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	for _, network := range g.networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// Wrap refuses untrusted peers before next sees the request.
func (g *Guard) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !g.Allow(request.RemoteAddr) {
			http.Error(writer, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(writer, request)
	})
}
