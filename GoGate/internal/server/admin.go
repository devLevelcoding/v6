package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/levelcodingdev/gogate/internal/route"
)

// adminMux serves the control-plane API under cfg.AdminPrefix.
func (s *server) adminMux() http.Handler {
	p := s.cfg.AdminPrefix
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+p+"/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "gogated"})
	})
	mux.HandleFunc("GET "+p+"/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": Version})
	})
	mux.HandleFunc("GET "+p+"/stats", s.stats)
	mux.HandleFunc("GET "+p+"/routes", s.listRoutes)
	mux.HandleFunc("POST "+p+"/routes", s.addRoute)
	mux.HandleFunc("GET "+p+"/routes/{id}", s.getRoute)
	mux.HandleFunc("DELETE "+p+"/routes/{id}", s.deleteRoute)
	return mux
}

func (s *server) listRoutes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.Routes.List())
}

func (s *server) addRoute(w http.ResponseWriter, r *http.Request) {
	var rt route.Route
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rt); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	added, err := s.cfg.Routes.Add(rt)
	if errors.Is(err, route.ErrInvalid) {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, added)
}

func (s *server) getRoute(w http.ResponseWriter, r *http.Request) {
	rt, err := s.cfg.Routes.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "no such route")
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

func (s *server) deleteRoute(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Routes.Delete(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, "no such route")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) stats(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{"routes": len(s.cfg.Routes.List())}
	if s.cfg.Cache != nil {
		out["cache"] = s.cfg.Cache.Stats()
	}
	if s.cfg.Limiter != nil {
		out["rate_limit_keys"] = s.cfg.Limiter.Len()
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// LoadRoutes adds every route in a JSON array (the -config file format). It is
// here so cmd/gogated and tests share one parser.
func LoadRoutes(store route.Store, jsonArray []byte) (int, error) {
	var rs []route.Route
	if err := json.Unmarshal(jsonArray, &rs); err != nil {
		return 0, err
	}
	for i, rt := range rs {
		if _, err := store.Add(rt); err != nil {
			return i, err
		}
	}
	return len(rs), nil
}
