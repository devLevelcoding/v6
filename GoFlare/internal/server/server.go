// Package server assembles GoFlare's core HTTP surface: the SDK ingest
// endpoints and the dashboard API on one mux, behind a shared health/version
// check. The edge proxy (package edge) runs on its own listener and is wired
// in cmd/goflared.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/levelcodingdev/goflare/internal/api"
	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/ingest"
	"github.com/levelcodingdev/goflare/internal/project"
	"github.com/levelcodingdev/goflare/internal/ui"
)

// New builds the core handler: ingest + dashboard API + health. pipe is
// optional — when non-nil, ingest enqueues events onto it (async, returns 202)
// instead of grouping them in the request goroutine.
func New(projects project.Store, groups *group.Store, publicURL string, pipe *ingest.Pipeline, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "goflared"})
	})
	mux.HandleFunc("GET /v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": api.Version})
	})

	ing := ingest.New(projects, groups, log)
	if pipe != nil {
		ing = ing.WithPipeline(pipe)
	}
	ing.Register(mux)
	api.New(projects, groups, publicURL).Register(mux)

	// The web console owns every path the API and ingest didn't claim; the
	// client-side router serves index.html for unknown routes.
	mux.Handle("/", ui.Handler())
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
