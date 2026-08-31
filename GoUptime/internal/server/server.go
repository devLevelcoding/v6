// Package server exposes GoUptime's HTTP API: monitor CRUD, on-demand checks,
// result timelines and the incident log. The route shape is deliberately plain
// REST so a status-page frontend or CLI (future.md Phase 2) can sit on top
// without translation.
//
// This file wires the routes and shared helpers; monitor CRUD handlers are in
// monitors.go and the check/results/summary/incidents handlers in timeline.go.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/levelcodingdev/gouptime/internal/check"
	"github.com/levelcodingdev/gouptime/internal/history"
	"github.com/levelcodingdev/gouptime/internal/incident"
	"github.com/levelcodingdev/gouptime/internal/monitor"
)

// Version is the running build's version, overridable at link time.
var Version = "0.1.0-skeleton"

// Runner is the slice of the scheduler the API needs.
type Runner interface {
	RunNow(ctx context.Context, id string) (check.Result, error)
	Sync()
}

// IncidentLog reads incidents.
type IncidentLog interface {
	Incidents(monitorID string) []incident.Incident
}

// HistoryView reads recorded results.
type HistoryView interface {
	Results(monitorID string, limit int) []check.Result
	Summary(monitorID string) history.Summary
}

// Server is the API handler.
type Server struct {
	store  monitor.Store
	runner Runner
	inc    IncidentLog
	hist   HistoryView
	mux    *http.ServeMux
}

// New builds a Server with its routes registered.
func New(store monitor.Store, runner Runner, inc IncidentLog, hist HistoryView) *Server {
	s := &Server{store: store, runner: runner, inc: inc, hist: hist, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /v1/version", s.handleVersion)
	s.mux.HandleFunc("GET /v1/monitors", s.handleListMonitors)
	s.mux.HandleFunc("POST /v1/monitors", s.handleCreateMonitor)
	s.mux.HandleFunc("GET /v1/monitors/{id}", s.handleGetMonitor)
	s.mux.HandleFunc("PUT /v1/monitors/{id}", s.handleUpdateMonitor)
	s.mux.HandleFunc("DELETE /v1/monitors/{id}", s.handleDeleteMonitor)
	s.mux.HandleFunc("POST /v1/monitors/{id}/check", s.handleCheckNow)
	s.mux.HandleFunc("GET /v1/monitors/{id}/results", s.handleResults)
	s.mux.HandleFunc("GET /v1/monitors/{id}/summary", s.handleSummary)
	s.mux.HandleFunc("GET /v1/incidents", s.handleIncidents)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "gouptimed"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": Version})
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, monitor.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, monitor.ErrExists):
		return http.StatusConflict
	case errors.Is(err, monitor.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
