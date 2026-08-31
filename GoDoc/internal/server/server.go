// Package server is GoDoc's HTTP surface: one endpoint per format plus a
// unified /v1/render, a template listing, and health. Stateless — every request
// is decode → validate → stream, nothing is kept.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/levelcodingdev/godoc/internal/render"
	"github.com/levelcodingdev/godoc/internal/spec"
)

// Version is stamped by main.
var Version = "dev"

// Config wires the server.
type Config struct {
	Token   string        // when set, required as a bearer token
	MaxBody int64         // request body cap; default 8 MiB
	Timeout time.Duration // per-request render deadline; default 30s
	Log     *slog.Logger
}

type server struct{ cfg Config }

// New returns the handler.
func New(cfg Config) http.Handler {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.MaxBody <= 0 {
		cfg.MaxBody = 8 << 20
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	s := &server{cfg: cfg}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/render", s.handle(""))
	mux.HandleFunc("POST /v1/csv", s.handle(spec.FormatCSV))
	mux.HandleFunc("POST /v1/xml", s.handle(spec.FormatXML))
	mux.HandleFunc("POST /v1/pdf", s.handle(spec.FormatPDF))
	mux.HandleFunc("GET /v1/templates", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, render.Templates())
	})
	mux.HandleFunc("GET /_godoc/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "godocd"})
	})
	mux.HandleFunc("GET /_godoc/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": Version})
	})
	return mux
}

// handle builds the handler for one endpoint. A fixed format means the body is
// the bare sub-spec; an empty format means the body is a full spec.Request.
func (s *server) handle(fixed spec.Format) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token != "" && !bearerOK(r, s.cfg.Token) {
			writeErr(w, http.StatusUnauthorized, "missing or bad token")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.cfg.MaxBody))
		if err != nil {
			writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}

		req, err := parse(fixed, body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := req.Validate(); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}

		// Render into a buffer bounded by the deadline, then flush — so a
		// mid-render error still yields a clean JSON error, not a truncated file.
		done := make(chan result, 1)
		buf := &limitedBuffer{max: 64 << 20}
		go func() {
			out, rErr := render.Do(buf, req)
			done <- result{out, rErr}
		}()

		select {
		case res := <-done:
			if res.err != nil {
				s.cfg.Log.Warn("render failed", "format", req.Format, "err", res.err)
				writeErr(w, http.StatusUnprocessableEntity, "render failed: "+res.err.Error())
				return
			}
			w.Header().Set("Content-Type", res.out.ContentType)
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", res.out.Filename))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_, _ = w.Write(buf.Bytes())
		case <-time.After(s.cfg.Timeout):
			s.cfg.Log.Warn("render timed out", "format", req.Format)
			writeErr(w, http.StatusGatewayTimeout, "render exceeded the time limit")
		}
	}
}

type result struct {
	out render.Output
	err error
}

func parse(fixed spec.Format, body []byte) (*spec.Request, error) {
	if fixed == "" {
		var req spec.Request
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		return &req, nil
	}
	req := &spec.Request{Format: fixed}
	var err error
	switch fixed {
	case spec.FormatCSV:
		req.CSV = &spec.CSV{}
		err = json.Unmarshal(body, req.CSV)
	case spec.FormatXML:
		req.XML = &spec.XMLDoc{}
		err = json.Unmarshal(body, req.XML)
	case spec.FormatPDF:
		req.PDF = &spec.PDF{}
		err = json.Unmarshal(body, req.PDF)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return req, nil
}

func bearerOK(r *http.Request, token string) bool {
	h := r.Header.Get("Authorization")
	return len(h) > 7 && strings.EqualFold(h[:7], "bearer ") && h[7:] == token
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// limitedBuffer is a bytes.Buffer that errors past max, so a runaway template
// can't OOM the process.
type limitedBuffer struct {
	b   []byte
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if len(l.b)+len(p) > l.max {
		return 0, errors.New("render output exceeded the size limit")
	}
	l.b = append(l.b, p...)
	return len(p), nil
}

func (l *limitedBuffer) Bytes() []byte { return l.b }
