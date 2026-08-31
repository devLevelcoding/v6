// Package check runs a single probe against a monitor and reports the outcome.
// It holds no state: the scheduler decides when to call Probe, and package
// incident decides what a run of Results means.
package check

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/levelcodingdev/gouptime/internal/monitor"
)

// Result is the outcome of one probe.
type Result struct {
	MonitorID string    `json:"monitor_id"`
	At        time.Time `json:"at"`
	OK        bool      `json:"ok"`
	// StatusCode is the HTTP status for http monitors, 0 otherwise.
	StatusCode int `json:"status_code,omitempty"`
	// Latency is wall time from probe start to a usable response.
	Latency time.Duration `json:"latency"`
	// Detail explains a failure, or notes the success ("200 OK", "connected").
	Detail string `json:"detail"`
}

// Prober performs one probe. The default is DefaultProber; tests substitute
// their own.
type Prober interface {
	Probe(ctx context.Context, m monitor.Monitor) Result
}

// DefaultProber probes over the real network.
type DefaultProber struct {
	// Client is used for http monitors. If nil, a per-probe client is built
	// so that timeouts and redirects stay isolated.
	Client *http.Client
	now    func() time.Time
}

// NewProber returns a DefaultProber using the real clock.
func NewProber() *DefaultProber { return &DefaultProber{now: time.Now} }

// Probe dispatches on monitor type and always returns a Result (never panics
// on a bad target; validation is the store's job).
func (p *DefaultProber) Probe(ctx context.Context, m monitor.Monitor) Result {
	nowFn := p.now
	if nowFn == nil {
		nowFn = time.Now
	}
	start := nowFn()
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()

	var r Result
	switch m.Type {
	case monitor.TypeHTTP:
		r = p.probeHTTP(ctx, m)
	case monitor.TypeTCP:
		r = probeTCP(ctx, m)
	default:
		r = Result{OK: false, Detail: fmt.Sprintf("unsupported monitor type %q", m.Type)}
	}
	r.MonitorID = m.ID
	r.At = start
	r.Latency = nowFn().Sub(start)
	return r
}

func (p *DefaultProber) probeHTTP(ctx context.Context, m monitor.Monitor) Result {
	client := p.Client
	if client == nil {
		client = &http.Client{
			// The scheduler-supplied context already carries the timeout;
			// this is a backstop for a wedged connection.
			Timeout: m.Timeout + time.Second,
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.Target, nil)
	if err != nil {
		return Result{OK: false, Detail: "bad request: " + err.Error()}
	}
	req.Header.Set("User-Agent", "GoUptime/0.1 (+https://github.com/levelcodingdev/gouptime)")
	resp, err := client.Do(req)
	if err != nil {
		return Result{OK: false, Detail: classifyErr(err)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	if !m.AcceptsStatus(resp.StatusCode) {
		return Result{OK: false, StatusCode: resp.StatusCode, Detail: "unexpected status " + resp.Status}
	}
	return Result{OK: true, StatusCode: resp.StatusCode, Detail: resp.Status}
}

func probeTCP(ctx context.Context, m monitor.Monitor) Result {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", m.Target)
	if err != nil {
		return Result{OK: false, Detail: classifyErr(err)}
	}
	_ = conn.Close()
	return Result{OK: true, Detail: "connected"}
}

// classifyErr turns a transport error into a short, stable phrase.
func classifyErr(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "Client.Timeout"):
		return "timeout"
	case strings.Contains(msg, "no such host"):
		return "dns failure"
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "actively refused"):
		return "connection refused"
	case strings.Contains(msg, "certificate"):
		return "tls error: " + msg
	default:
		return msg
	}
}
