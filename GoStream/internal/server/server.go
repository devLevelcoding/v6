// Package server assembles GoStream: the /ws WebSocket endpoint (subscribe /
// unsubscribe / publish over a small JSON protocol), the POST /pub publish
// ingress, and a /_gostream control-plane API for health, stats and presence.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/levelcodingdev/gostream/internal/hub"
	"github.com/levelcodingdev/gostream/internal/ingest"
)

// Version is stamped by main.
var Version = "dev"

// Config wires the server.
type Config struct {
	Hub                *hub.Hub
	PublishToken       string        // guards POST /pub and client "publish" commands
	WSToken            string        // required (bearer or ?token=) to open /ws when set
	AllowClientPublish bool          // let a socket client publish, not just subscribe
	IdleTimeout        time.Duration // drop a socket silent this long (default 75s)
	PingInterval       time.Duration // server→client ping cadence (default 30s)
	ReadLimit          int64         // max inbound message bytes (default 1 MiB)
	Log                *slog.Logger
}

type server struct {
	cfg Config
}

// New returns the HTTP handler.
func New(cfg Config) http.Handler {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 75 * time.Second
	}
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = 30 * time.Second
	}
	if cfg.ReadLimit <= 0 {
		cfg.ReadLimit = 1 << 20
	}
	s := &server{cfg: cfg}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", s.serveWS)
	ingest.Handler{Hub: cfg.Hub, Token: cfg.PublishToken}.Register(mux)

	mux.HandleFunc("GET /_gostream/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "gostreamd"})
	})
	mux.HandleFunc("GET /_gostream/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": Version})
	})
	mux.HandleFunc("GET /_gostream/stats", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, cfg.Hub.Stats())
	})
	mux.HandleFunc("GET /_gostream/presence", func(w http.ResponseWriter, r *http.Request) {
		p := cfg.Hub.Presence()
		if topic := r.URL.Query().Get("topic"); topic != "" {
			p = filterByTopic(p, topic)
		}
		writeJSON(w, http.StatusOK, p)
	})
	return mux
}

func filterByTopic(in []hub.ClientInfo, topic string) []hub.ClientInfo {
	out := in[:0]
	for _, ci := range in {
		for _, t := range ci.Topics {
			if t == topic {
				out = append(out, ci)
				break
			}
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
