package health

import (
	"encoding/json"
	"net/http"
)

func NewHandler(state *State) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]string{"status": "ok"}, state.Live())
	})
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		snapshot := state.Snapshot()
		writeJSON(writer, snapshot, snapshot.Ready)
	})
	mux.HandleFunc("/status", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, state.Snapshot(), true)
	})
	return mux
}

func writeJSON(writer http.ResponseWriter, value any, healthy bool) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	if !healthy {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(writer).Encode(value)
}
