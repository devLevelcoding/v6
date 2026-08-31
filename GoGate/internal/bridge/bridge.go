// Package bridge is GoGate's HTTP↔queue translation: a route can target a
// message subject instead of an HTTP upstream, and the bridge turns the request
// into a Message, does a request/reply over a Transport, and writes the Reply
// back. This is the crm3-micro gateway's whole job in ~one file.
//
// Phase 0 ships the HTTP half plus a Loopback transport (in-process handlers,
// for tests and single-binary deploys). A real Pub/Sub / NATS Transport is a
// later phase (see ../../future.md §3) and plugs in behind this interface with
// no change here.
package bridge

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/levelcodingdev/gogate/internal/uid"
)

// ErrNoHandler is returned by a Transport when nothing serves the subject.
var ErrNoHandler = errors.New("bridge: no handler for subject")

// Message is an HTTP request flattened for transport.
type Message struct {
	ID     string      `json:"id"`
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Query  string      `json:"query"`
	Header http.Header `json:"header"`
	Body   []byte      `json:"body"`
}

// Reply is what the subject's handler returns.
type Reply struct {
	Status int         `json:"status"`
	Header http.Header `json:"header"`
	Body   []byte      `json:"body"`
}

// Transport does one request/reply against a subject.
type Transport interface {
	Request(ctx context.Context, subject string, m Message) (Reply, error)
}

// Handler serves bridge routes by calling a Transport.
type Handler struct {
	Transport Transport
	Timeout   time.Duration // per-request cap; default 20s
	MaxBody   int64         // request body cap; default 4 MiB
}

// ServeSubject reads the request, does the request/reply, and writes the reply.
func (h Handler) ServeSubject(w http.ResponseWriter, r *http.Request, subject string) {
	maxBody := h.MaxBody
	if maxBody <= 0 {
		maxBody = 4 << 20
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
		return
	}

	timeout := h.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	m := Message{
		ID:     uid.New(),
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Header: r.Header.Clone(),
		Body:   body,
	}
	reply, err := h.Transport.Request(ctx, subject, m)
	if err != nil {
		writeBridgeError(w, err)
		return
	}
	for k, vs := range reply.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	status := reply.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(reply.Body)
}

func writeBridgeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNoHandler):
		http.Error(w, `{"error":"no upstream for this route"}`, http.StatusBadGateway)
	case errors.Is(err, context.DeadlineExceeded):
		http.Error(w, `{"error":"upstream timed out"}`, http.StatusGatewayTimeout)
	default:
		http.Error(w, `{"error":"bridge failure"}`, http.StatusBadGateway)
	}
}
