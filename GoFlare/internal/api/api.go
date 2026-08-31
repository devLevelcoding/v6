// Package api is the dashboard-facing REST API: projects and their DSNs, the
// issue stream, issue triage, and per-issue event samples. Route shapes follow
// Sentry's web API closely enough that a familiar frontend can sit on top,
// minus the org/team nesting which is a Phase 1 concern.
//
// This file wires routes and shared helpers; the project handlers are in
// projects.go and the issue/event handlers in issues.go.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/project"
)

// Version is the running build's version, overridable at link time.
var Version = "0.1.0-skeleton"

// Handler serves the dashboard API.
type Handler struct {
	projects  project.Store
	groups    *group.Store
	publicURL string
}

// New builds the API Handler. publicURL is the externally reachable base URL
// (scheme://host[:port]) used to render project DSNs.
func New(projects project.Store, groups *group.Store, publicURL string) *Handler {
	return &Handler{projects: projects, groups: groups, publicURL: publicURL}
}

// Register wires the API routes onto a mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/0/version/", h.version)
	mux.HandleFunc("GET /api/0/projects/", h.listProjects)
	mux.HandleFunc("POST /api/0/projects/", h.createProject)
	mux.HandleFunc("GET /api/0/projects/{id}/", h.getProject)
	mux.HandleFunc("GET /api/0/projects/{id}/issues/", h.listIssues)
	mux.HandleFunc("GET /api/0/issues/{id}/", h.getIssue)
	mux.HandleFunc("PUT /api/0/issues/{id}/", h.updateIssue)
	mux.HandleFunc("GET /api/0/issues/{id}/events/", h.listEvents)
	mux.HandleFunc("GET /api/0/issues/{id}/events/latest/", h.latestEvent)
}

func statusForProject(err error) int {
	switch {
	case errors.Is(err, project.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, project.ErrExists):
		return http.StatusConflict
	case errors.Is(err, project.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func statusForGroup(err error) int {
	switch {
	case errors.Is(err, group.ErrNotFound):
		return http.StatusNotFound
	case err != nil && err.Error() == "group: invalid status":
		return http.StatusBadRequest
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

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
