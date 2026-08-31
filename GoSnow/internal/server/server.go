// Package server exposes GoSnow's HTTP API: statement submission, catalog
// browsing and warehouse management. Its shape tracks Snowflake's SQL REST API
// closely enough that the eventual client driver (future.md Phase 4) can
// target it.
package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/levelcodingdev/gosnow/internal/catalog"
	"github.com/levelcodingdev/gosnow/internal/query"
	"github.com/levelcodingdev/gosnow/internal/warehouse"
)

// Version is the running build's version, overridable at link time.
var Version = "0.0.1-skeleton"

// Server is the API handler.
type Server struct {
	cat catalog.Catalog
	wh  *warehouse.Manager
	eng query.Engine
	mux *http.ServeMux
}

// New builds a Server with its routes registered.
func New(cat catalog.Catalog, wh *warehouse.Manager, eng query.Engine) *Server {
	s := &Server{cat: cat, wh: wh, eng: eng, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /v1/version", s.handleVersion)
	s.mux.HandleFunc("POST /v1/statements", s.handleStatements)
	s.mux.HandleFunc("GET /v1/databases", s.handleDatabases)
	s.mux.HandleFunc("GET /v1/warehouses", s.handleWarehouses)
	s.mux.HandleFunc("POST /v1/warehouses", s.handleCreateWarehouse)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "gosnowd"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": Version})
}

func (s *Server) handleStatements(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SQL       string `json:"sql"`
		Database  string `json:"database"`
		Schema    string `json:"schema"`
		Warehouse string `json:"warehouse"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.eng.Execute(r.Context(), query.Request{
		SQL:       req.SQL,
		Database:  req.Database,
		Schema:    req.Schema,
		Warehouse: req.Warehouse,
	})
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDatabases(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"databases": s.cat.Databases()})
}

func (s *Server) handleWarehouses(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"warehouses": s.wh.List()})
}

func (s *Server) handleCreateWarehouse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Size string `json:"size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	wh, err := s.wh.Create(req.Name, warehouse.Size(req.Size))
	if err != nil {
		writeError(w, statusForWarehouse(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, wh)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, query.ErrBadRequest):
		return http.StatusBadRequest
	case errors.Is(err, query.ErrUnsupported):
		return http.StatusUnprocessableEntity
	case errors.Is(err, catalog.ErrExists):
		return http.StatusConflict
	case errors.Is(err, catalog.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func statusForWarehouse(err error) int {
	switch {
	case errors.Is(err, warehouse.ErrExists):
		return http.StatusConflict
	case errors.Is(err, warehouse.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
