package api

import (
	"net/http"

	"github.com/levelcodingdev/goflare/internal/project"
)

// projectView is a project plus its rendered DSN.
type projectView struct {
	project.Project
	DSN string `json:"dsn"`
}

func (h *Handler) view(p project.Project) projectView {
	return projectView{Project: p, DSN: p.DSN(h.publicURL)}
}

func (h *Handler) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": Version})
}

func (h *Handler) listProjects(w http.ResponseWriter, _ *http.Request) {
	list := h.projects.List()
	out := make([]projectView, len(list))
	for i, p := range list {
		out[i] = h.view(p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := h.projects.Create(req.Name, req.Platform)
	if err != nil {
		writeErr(w, statusForProject(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, h.view(p))
}

func (h *Handler) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := h.projects.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusForProject(err), err)
		return
	}
	writeJSON(w, http.StatusOK, h.view(p))
}
