// Package ingest is the publish side: an HTTP endpoint that turns a POST into a
// hub.Publish. gRPC / an internal Go API are later phases (see ../../future.md
// §3); the hub interface they'd share is already here.
package ingest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/levelcodingdev/gostream/internal/hub"
	"github.com/levelcodingdev/gostream/internal/proto"
)

// Handler serves POST /pub/{topic}.
type Handler struct {
	Hub     *hub.Hub
	Token   string // when non-empty, required as a bearer token or ?token=
	MaxBody int64  // request body cap; default 1 MiB
}

// Register wires the publish route onto mux.
func (h Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /pub/{topic}", h.publish)
}

func (h Handler) publish(w http.ResponseWriter, r *http.Request) {
	if h.Token != "" && !h.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad or missing publish token"})
		return
	}
	topic := r.PathValue("topic")
	if topic == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty topic"})
		return
	}

	max := h.MaxBody
	if max <= 0 {
		max = 1 << 20
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "payload too large"})
		return
	}

	res := h.Hub.Publish(topic, proto.Message(topic, body))
	writeJSON(w, http.StatusOK, res)
}

func (h Handler) authorized(r *http.Request) bool {
	if t := r.URL.Query().Get("token"); t != "" {
		return t == h.Token
	}
	auth := r.Header.Get("Authorization")
	return len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") && auth[7:] == h.Token
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
