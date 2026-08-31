package edge

// Turning an upstream failure into a synthetic event: the hooks the reverse
// proxy calls on a 5xx response or a transport error, and the event they build.

import (
	"fmt"
	"net/http"
	"time"

	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/uid"
)

func (p *Proxy) responseHook(cr compiledRoute) func(*http.Response) error {
	return func(resp *http.Response) error {
		if resp.StatusCode >= 500 && cr.projectID != "" {
			p.capture.Ingest(cr.projectID, p.synthEvent(resp.Request,
				fmt.Sprintf("Upstream responded %d", resp.StatusCode),
				fmt.Sprintf("HTTP %d from %s", resp.StatusCode, cr.target.Host),
				event.LevelError))
		}
		return nil
	}
}

func (p *Proxy) errorHook(cr compiledRoute) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		if cr.projectID != "" {
			p.capture.Ingest(cr.projectID, p.synthEvent(r,
				"Upstream unreachable", err.Error(), event.LevelFatal))
		}
		p.log.Warn("edge upstream error", "host", r.Host, "upstream", cr.target.Host, "err", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}
}

func (p *Proxy) synthEvent(r *http.Request, excType, excValue string, level event.Level) event.Event {
	e := event.Event{
		EventID:   uid.New(),
		Timestamp: time.Now().UTC(),
		Platform:  "edge",
		Level:     level,
		Logger:    "goflare.edge",
		Exceptions: []event.Exception{{
			Type:  excType,
			Value: excValue,
		}},
		Tags: map[string]string{},
	}
	if r != nil {
		e.Transaction = r.Method + " " + r.URL.Path
		e.Tags["http.method"] = r.Method
		e.Tags["http.path"] = r.URL.Path
		e.Tags["http.host"] = r.Host
		// Group by route+method, not by the exact status string.
		e.Fingerprint = []string{"edge", r.Host, r.URL.Path, excType}
	}
	return e
}
