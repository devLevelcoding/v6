package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/levelcodingdev/goflare/internal/group"
)

func (h *Handler) listIssues(w http.ResponseWriter, r *http.Request) {
	if _, err := h.projects.Get(r.PathValue("id")); err != nil {
		writeErr(w, statusForProject(err), err)
		return
	}
	f := group.Filter{
		ProjectID: r.PathValue("id"),
		Status:    group.Status(r.URL.Query().Get("status")),
		Query:     r.URL.Query().Get("query"),
	}
	if f.Status != "" && !f.Status.Valid() {
		writeErr(w, http.StatusBadRequest, errors.New("unknown status filter"))
		return
	}
	writeJSON(w, http.StatusOK, h.groups.List(f))
}

func (h *Handler) getIssue(w http.ResponseWriter, r *http.Request) {
	iss, err := h.groups.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusForGroup(err), err)
		return
	}
	writeJSON(w, http.StatusOK, iss)
}

func (h *Handler) updateIssue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	iss, err := h.groups.SetStatus(r.PathValue("id"), group.Status(req.Status))
	if err != nil {
		writeErr(w, statusForGroup(err), err)
		return
	}
	writeJSON(w, http.StatusOK, iss)
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	evs, err := h.groups.Events(r.PathValue("id"), limit)
	if err != nil {
		writeErr(w, statusForGroup(err), err)
		return
	}
	writeJSON(w, http.StatusOK, evs)
}

func (h *Handler) latestEvent(w http.ResponseWriter, r *http.Request) {
	ev, err := h.groups.LatestEvent(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusForGroup(err), err)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}
