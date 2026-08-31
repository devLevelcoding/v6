// Package server is GoRender's REST + SSE surface. Submit a Spec, poll the job,
// stream its progress, download the artifact.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/levelcodingdev/gorender/internal/events"
	"github.com/levelcodingdev/gorender/internal/job"
	"github.com/levelcodingdev/gorender/internal/queue"
	"github.com/levelcodingdev/gorender/internal/spec"
)

// Version is stamped by main at build/start.
var Version = "dev"

// Deps is what the server needs from the rest of the process.
type Deps struct {
	Store   *job.Store
	Queue   *queue.Mem
	Events  *events.Broker
	OutDir  string
	FFmpeg  string // resolved path, for /healthz
	FFprobe string
}

// New returns the HTTP handler.
func New(d Deps) http.Handler {
	s := &srv{d: d}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /v1/version", s.version)
	mux.HandleFunc("POST /v1/jobs", s.createJob)
	mux.HandleFunc("GET /v1/jobs", s.listJobs)
	mux.HandleFunc("GET /v1/jobs/{id}", s.getJob)
	mux.HandleFunc("GET /v1/jobs/{id}/events", s.jobEvents)
	mux.HandleFunc("GET /v1/jobs/{id}/artifact", s.jobArtifact)
	return mux
}

type srv struct{ d Deps }

func (s *srv) healthz(w http.ResponseWriter, r *http.Request) {
	ok := s.d.FFmpeg != "" && s.d.FFprobe != ""
	code := http.StatusOK
	if !ok {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status":  map[bool]string{true: "ok", false: "degraded"}[ok],
		"ffmpeg":  s.d.FFmpeg,
		"ffprobe": s.d.FFprobe,
		"queued":  s.d.Queue.Len(),
	})
}

func (s *srv) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": Version})
}

func (s *srv) createJob(w http.ResponseWriter, r *http.Request) {
	var sp spec.Spec
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sp); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	sp.Normalize()
	if err := sp.Validate(); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	j := s.d.Store.Create(sp)
	if err := s.d.Queue.Push(r.Context(), j.ID); err != nil {
		// Roll the job back to failed so it doesn't sit "queued" forever.
		s.d.Store.MarkDone(j.ID, "", fmt.Errorf("enqueue: %w", err))
		writeErr(w, http.StatusServiceUnavailable, "queue full, try again")
		return
	}
	writeJSON(w, http.StatusAccepted, j)
}

func (s *srv) listJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.d.Store.List())
}

func (s *srv) getJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.d.Store.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "no such job")
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (s *srv) jobEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j, ok := s.d.Store.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "no such job")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cancel := s.d.Events.Subscribe(id)
	defer cancel()

	// Send the current state immediately so a late subscriber isn't blank.
	sendSSE(w, flusher, events.Event{JobID: id, Status: string(j.Status), Progress: j.Progress, Message: j.Error})
	if j.Status == job.StatusSucceeded || j.Status == job.StatusFailed {
		return
	}

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-ch:
			if !open {
				return
			}
			sendSSE(w, flusher, e)
			if e.Status == string(job.StatusSucceeded) || e.Status == string(job.StatusFailed) {
				return
			}
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *srv) jobArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j, ok := s.d.Store.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "no such job")
		return
	}
	if j.Status != job.StatusSucceeded || j.Artifact == "" {
		writeErr(w, http.StatusConflict, "artifact not ready (job is "+string(j.Status)+")")
		return
	}
	// id is a 32-char hex uid, so reconstructing the path from OutDir + id is
	// traversal-proof — no need to trust the stored Artifact string.
	abs := filepath.Join(s.d.OutDir, id+".mp4")
	f, err := os.Open(abs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "opening artifact: "+err.Error())
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", id+".mp4"))
	http.ServeContent(w, r, id+".mp4", modTime(st), f)
}

func modTime(st os.FileInfo) time.Time {
	if st == nil {
		return time.Time{}
	}
	return st.ModTime()
}

// --- helpers ---

func sendSSE(w http.ResponseWriter, f http.Flusher, e events.Event) {
	b, _ := json.Marshal(e)
	fmt.Fprintf(w, "data: %s\n\n", b)
	f.Flush()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
