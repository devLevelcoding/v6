// Package ingest is the SDK-facing side of GoFlare: the endpoints a Sentry
// client posts to. It authenticates the DSN public key, decodes the payload
// (envelope or the legacy store format, gzip/deflate aware) and hands the
// event to the grouping store.
//
// This file has the handlers; body decompression, key resolution and the JSON
// responses are in payload.go.
package ingest

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/project"
	"github.com/levelcodingdev/goflare/internal/uid"
)

// Handler serves the ingest endpoints.
type Handler struct {
	projects project.Store
	groups   *group.Store
	pipe     *Pipeline // nil → group synchronously in the request (Phase 0 mode)
	log      *slog.Logger
}

// New builds an ingest Handler that groups events synchronously.
func New(projects project.Store, groups *group.Store, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{projects: projects, groups: groups, log: log}
}

// WithPipeline returns h configured to enqueue events onto pipe instead of
// grouping them in the request goroutine. The caller owns pipe's lifecycle
// (Start / Wait).
func (h *Handler) WithPipeline(pipe *Pipeline) *Handler {
	h.pipe = pipe
	return h
}

// Register wires the ingest routes onto a mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/{project_id}/envelope/", h.handleEnvelope)
	mux.HandleFunc("POST /api/{project_id}/store/", h.handleStore)
}

func (h *Handler) handleEnvelope(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project_id")
	body, err := readBody(r)
	if err != nil {
		writeIngestError(w, http.StatusBadRequest, err)
		return
	}
	env, err := event.ParseEnvelope(body)
	if err != nil {
		writeIngestError(w, http.StatusBadRequest, err)
		return
	}

	key := ingestKey(r, env.DSN())
	proj, err := h.authenticate(projectID, key)
	if err != nil {
		writeIngestError(w, statusForAuth(err), err)
		return
	}

	ev, err := env.EventItem()
	if err != nil {
		if errors.Is(err, event.ErrNoEvent) {
			// A valid envelope with only sessions/attachments/etc. Accept it.
			writeJSON(w, http.StatusOK, map[string]string{"id": envelopeID(env)})
			return
		}
		writeIngestError(w, http.StatusBadRequest, err)
		return
	}
	h.accept(w, proj, ev)
}

func (h *Handler) handleStore(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project_id")
	body, err := readBody(r)
	if err != nil {
		writeIngestError(w, http.StatusBadRequest, err)
		return
	}
	proj, err := h.authenticate(projectID, ingestKey(r, ""))
	if err != nil {
		writeIngestError(w, statusForAuth(err), err)
		return
	}
	ev, err := event.Decode(body)
	if err != nil {
		writeIngestError(w, http.StatusBadRequest, err)
		return
	}
	h.accept(w, proj, ev)
}

func (h *Handler) accept(w http.ResponseWriter, proj project.Project, ev event.Event) {
	if ev.EventID == "" {
		ev.EventID = uid.New()
	}
	if ev.Platform == "" {
		ev.Platform = proj.Platform
	}

	if h.pipe != nil {
		if err := h.pipe.Submit(proj.ID, proj.Slug, ev); err != nil {
			w.Header().Set("Retry-After", "2")
			writeIngestError(w, http.StatusTooManyRequests, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"id": ev.EventID})
		return
	}

	iss, outcome := h.groups.Ingest(proj.ID, ev)
	h.log.Info("event ingested",
		"project", proj.Slug, "issue", iss.ID, "outcome", outcome,
		"title", iss.Title, "times_seen", iss.TimesSeen)
	writeJSON(w, http.StatusOK, map[string]string{"id": ev.EventID})
}

func (h *Handler) authenticate(projectID, publicKey string) (project.Project, error) {
	if publicKey == "" {
		return project.Project{}, project.ErrAuth
	}
	return h.projects.Authenticate(projectID, publicKey)
}
