// Package flags is a live feature-flag store: toggling a flag, or its
// rollout percentage, changes behavior immediately with no restart.
// Per-user overrides win over the flag's global enabled/rollout state;
// rollout percentage otherwise buckets users deterministically by a hash
// of (flag, user). See GraphQLGateMiddleware in gate.go for how it's used
// to gate an endpoint live.
package flags

import (
	"encoding/json"
	"hash/fnv"
	"net/http"
	"sync"
)

// Flag is one feature flag's live configuration.
type Flag struct {
	Name           string          `json:"name"`
	Enabled        bool            `json:"enabled"`
	RolloutPercent int             `json:"rollout_percent"`
	UserOverrides  map[string]bool `json:"user_overrides"`
}

// Store holds all flags, safe for concurrent read/write.
type Store struct {
	mu    sync.RWMutex
	flags map[string]Flag
}

func NewStore() *Store {
	return &Store{flags: map[string]Flag{}}
}

func (s *Store) Set(f Flag) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flags[f.Name] = f
}

func (s *Store) Get(name string) (Flag, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.flags[name]
	return f, ok
}

// Evaluate reports whether flagName is enabled for userID, reading
// whatever the CURRENT live configuration is at the moment of the call.
func (s *Store) Evaluate(flagName, userID string) bool {
	s.mu.RLock()
	f, ok := s.flags[flagName]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if override, ok := f.UserOverrides[userID]; ok {
		return override
	}
	if !f.Enabled {
		return false
	}
	if f.RolloutPercent >= 100 {
		return true
	}
	if f.RolloutPercent <= 0 {
		return false
	}
	return bucket(flagName, userID) < f.RolloutPercent
}

func bucket(flagName, userID string) int {
	h := fnv.New32a()
	h.Write([]byte(flagName))
	h.Write([]byte{':'})
	h.Write([]byte(userID))
	return int(h.Sum32() % 100)
}

// Handler exposes the flag service over real HTTP:
//
//	GET  /flags/{name}/eval?user={id}  -> {"enabled": bool}
//	PUT  /flags/{name}                 -> body is a Flag JSON, live-updates it
func (s *Store) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /flags/{name}/eval", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		userID := r.URL.Query().Get("user")
		enabled := s.Evaluate(name, userID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"flag": name, "user": userID, "enabled": enabled})
	})
	mux.HandleFunc("PUT /flags/{name}", func(w http.ResponseWriter, r *http.Request) {
		var f Flag
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		f.Name = r.PathValue("name")
		s.Set(f)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(f)
	})
	return mux
}
