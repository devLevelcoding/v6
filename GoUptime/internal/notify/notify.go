// Package notify delivers incident events to the outside world. Phase 0 ships a
// structured log sink and a generic JSON webhook; email, Slack and PagerDuty
// are Phase 1 (see future.md).
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/levelcodingdev/gouptime/internal/incident"
	"github.com/levelcodingdev/gouptime/internal/monitor"
)

// Message is the payload handed to every Notifier.
type Message struct {
	Event     incident.EventType `json:"event"`
	Incident  incident.Incident  `json:"incident"`
	Monitor   monitor.Monitor    `json:"monitor"`
	Timestamp time.Time          `json:"timestamp"`
}

// Notifier delivers one Message. Implementations must be safe for concurrent
// use and must not block indefinitely.
type Notifier interface {
	Notify(ctx context.Context, m Message) error
}

// Multi fans a Message out to every child, collecting errors but never
// short-circuiting.
type Multi []Notifier

// Notify delivers to all children.
func (ns Multi) Notify(ctx context.Context, m Message) error {
	var errs []error
	for _, n := range ns {
		if err := n.Notify(ctx, m); err != nil {
			errs = append(errs, err)
		}
	}
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return fmt.Errorf("%d of %d notifiers failed: %v", len(errs), len(ns), errs)
	}
}

// LogNotifier writes one structured log line per event.
type LogNotifier struct{ Logger *slog.Logger }

// Notify logs the event at INFO (opened) or WARN→INFO by type.
func (l LogNotifier) Notify(_ context.Context, m Message) error {
	lg := l.Logger
	if lg == nil {
		lg = slog.Default()
	}
	attrs := []any{
		"monitor_id", m.Monitor.ID,
		"monitor", m.Monitor.Name,
		"target", m.Monitor.Target,
		"incident_id", m.Incident.ID,
		"cause", m.Incident.Cause,
	}
	if m.Event == incident.Opened {
		lg.Warn("incident opened", attrs...)
	} else {
		lg.Info("incident resolved", append(attrs,
			"duration", m.Incident.ResolvedAt.Sub(m.Incident.StartedAt).String())...)
	}
	return nil
}

// WebhookNotifier POSTs the Message as JSON to a fixed URL.
type WebhookNotifier struct {
	URL    string
	Client *http.Client
}

// Notify sends the webhook. Non-2xx responses are errors.
func (w WebhookNotifier) Notify(ctx context.Context, m Message) error {
	if w.URL == "" {
		return nil
	}
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GoUptime/0.1")

	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook post: status %s", resp.Status)
	}
	return nil
}
