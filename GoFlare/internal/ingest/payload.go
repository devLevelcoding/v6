package ingest

// Transport concerns for an ingest request: pulling the (possibly compressed)
// body, resolving the DSN key, and the JSON responses SDKs expect.

import (
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/project"
	"github.com/levelcodingdev/goflare/internal/uid"
)

// maxBody caps a decompressed ingest payload.
const maxBody = 5 << 20

// ingestKey resolves the DSN public key from the request, falling back to a
// DSN carried in the envelope header.
func ingestKey(r *http.Request, envelopeDSN string) string {
	if a, err := event.ParseAuth(r); err == nil {
		return a.PublicKey
	}
	if envelopeDSN != "" {
		if k, _, err := event.AuthFromDSN(envelopeDSN); err == nil {
			return k
		}
	}
	return ""
}

func envelopeID(env *event.Envelope) string {
	if v, ok := env.Headers["event_id"].(string); ok && v != "" {
		return v
	}
	return uid.New()
}

func readBody(r *http.Request) ([]byte, error) {
	var reader io.Reader = io.LimitReader(r.Body, maxBody)
	switch r.Header.Get("Content-Encoding") {
	case "gzip":
		zr, err := gzip.NewReader(reader)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		reader = io.LimitReader(zr, maxBody)
	case "deflate":
		zr, err := zlib.NewReader(reader)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		reader = io.LimitReader(zr, maxBody)
	}
	return io.ReadAll(reader)
}

func statusForAuth(err error) int {
	switch {
	case errors.Is(err, project.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusUnauthorized
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeIngestError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
