package bridge

import (
	"encoding/json"
	"net/http"
)

// HealthHandler returns an http.Handler that reports the bridge's liveness and
// session counters as JSON. It is intended for a supervisor watchdog (e.g. the
// Home Assistant add-on) and for basic monitoring. It never exposes credentials.
//
//	GET /            -> 200 {"status":"ok","sessions_total":N,"sessions_active":M,"listen":"host:port"}
func (b *Bridge) HealthHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		total, active := b.Stats()
		listen := ""
		if addr := b.Addr(); addr != nil {
			listen = addr.String()
		}
		body := map[string]any{
			"status":          "ok",
			"sessions_total":  total,
			"sessions_active": active,
			"listen":          listen,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(body)
	})
}
