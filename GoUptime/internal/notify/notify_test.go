package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/levelcodingdev/gouptime/internal/incident"
	"github.com/levelcodingdev/gouptime/internal/monitor"
)

func sampleMsg() Message {
	start := time.Now().Add(-5 * time.Minute)
	resolved := time.Now()
	return Message{
		Event: incident.Resolved,
		Incident: incident.Incident{
			ID: "inc1", MonitorID: "m1", StartedAt: start, ResolvedAt: &resolved, Cause: "timeout",
		},
		Monitor:   monitor.Monitor{ID: "m1", Name: "api", Target: "https://api.example.com"},
		Timestamp: time.Now(),
	}
}

func TestWebhookNotifierPostsJSON(t *testing.T) {
	var got Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(202)
	}))
	defer srv.Close()

	n := WebhookNotifier{URL: srv.URL}
	if err := n.Notify(context.Background(), sampleMsg()); err != nil {
		t.Fatal(err)
	}
	if got.Incident.ID != "inc1" || got.Monitor.Name != "api" {
		t.Errorf("webhook payload wrong: %+v", got)
	}
}

func TestWebhookNotifierNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	n := WebhookNotifier{URL: srv.URL}
	if err := n.Notify(context.Background(), sampleMsg()); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestWebhookNotifierEmptyURLNoop(t *testing.T) {
	if err := (WebhookNotifier{}).Notify(context.Background(), sampleMsg()); err != nil {
		t.Fatalf("empty URL should be a no-op, got %v", err)
	}
}

type countingNotifier struct {
	n   atomic.Int32
	err error
}

func (c *countingNotifier) Notify(context.Context, Message) error {
	c.n.Add(1)
	return c.err
}

func TestMultiFansOutAndAggregatesErrors(t *testing.T) {
	a := &countingNotifier{}
	b := &countingNotifier{err: errors.New("boom")}
	c := &countingNotifier{err: errors.New("bang")}

	err := Multi{a, b, c}.Notify(context.Background(), sampleMsg())
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	if a.n.Load() != 1 || b.n.Load() != 1 || c.n.Load() != 1 {
		t.Errorf("not all notifiers called: %d %d %d", a.n.Load(), b.n.Load(), c.n.Load())
	}
}

func TestLogNotifierHandlesBothEvents(t *testing.T) {
	l := LogNotifier{}
	m := sampleMsg()
	if err := l.Notify(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	m.Event = incident.Opened
	if err := l.Notify(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}
