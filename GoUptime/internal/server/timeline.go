package server

import (
	"net/http"
	"strconv"
)

func (s *Server) handleCheckNow(w http.ResponseWriter, r *http.Request) {
	res, err := s.runner.RunNow(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Get(r.PathValue("id")); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": s.hist.Results(r.PathValue("id"), limit),
	})
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Get(r.PathValue("id")); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, s.hist.Summary(r.PathValue("id")))
}

func (s *Server) handleIncidents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"incidents": s.inc.Incidents(r.URL.Query().Get("monitor")),
	})
}
