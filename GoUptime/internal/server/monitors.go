package server

import (
	"net/http"
	"time"

	"github.com/levelcodingdev/gouptime/internal/monitor"
)

// monitorDTO is the wire form of a monitor: durations as seconds, not the
// nanosecond ints time.Duration marshals to.
type monitorDTO struct {
	ID           string    `json:"id,omitempty"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Target       string    `json:"target"`
	IntervalSecs int       `json:"interval_seconds"`
	TimeoutSecs  int       `json:"timeout_seconds,omitempty"`
	ExpectStatus [2]int    `json:"expect_status,omitempty"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

func toDTO(m monitor.Monitor) monitorDTO {
	return monitorDTO{
		ID: m.ID, Name: m.Name, Type: string(m.Type), Target: m.Target,
		IntervalSecs: int(m.Interval / time.Second),
		TimeoutSecs:  int(m.Timeout / time.Second),
		ExpectStatus: m.ExpectStatus, Enabled: m.Enabled,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func (d monitorDTO) toModel() monitor.Monitor {
	return monitor.Monitor{
		ID: d.ID, Name: d.Name, Type: monitor.Type(d.Type), Target: d.Target,
		Interval:     time.Duration(d.IntervalSecs) * time.Second,
		Timeout:      time.Duration(d.TimeoutSecs) * time.Second,
		ExpectStatus: d.ExpectStatus, Enabled: d.Enabled,
	}
}

func (s *Server) handleListMonitors(w http.ResponseWriter, _ *http.Request) {
	list := s.store.List()
	out := make([]monitorDTO, len(list))
	for i, m := range list {
		out[i] = toDTO(m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"monitors": out})
}

func (s *Server) handleCreateMonitor(w http.ResponseWriter, r *http.Request) {
	var dto monitorDTO
	if err := decode(r, &dto); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	dto.ID = "" // server-assigned
	m, err := s.store.Create(dto.toModel())
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	s.runner.Sync()
	writeJSON(w, http.StatusCreated, toDTO(m))
}

func (s *Server) handleGetMonitor(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(m))
}

func (s *Server) handleUpdateMonitor(w http.ResponseWriter, r *http.Request) {
	var dto monitorDTO
	if err := decode(r, &dto); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	dto.ID = r.PathValue("id")
	m, err := s.store.Update(dto.toModel())
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	s.runner.Sync()
	writeJSON(w, http.StatusOK, toDTO(m))
}

func (s *Server) handleDeleteMonitor(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Delete(r.PathValue("id")); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	s.runner.Sync()
	w.WriteHeader(http.StatusNoContent)
}
