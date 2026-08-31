package check

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/levelcodingdev/gouptime/internal/monitor"
)

func mustValid(t *testing.T, m monitor.Monitor) monitor.Monitor {
	t.Helper()
	if err := m.Validate(); err != nil {
		t.Fatalf("invalid test monitor: %v", err)
	}
	return m
}

func TestProbeHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(200)
		case "/500":
			w.WriteHeader(500)
		case "/slow":
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()

	p := NewProber()

	t.Run("up", func(t *testing.T) {
		r := p.Probe(context.Background(), mustValid(t, monitor.Monitor{
			Name: "x", Type: monitor.TypeHTTP, Target: srv.URL + "/ok", Interval: time.Minute,
		}))
		if !r.OK || r.StatusCode != 200 {
			t.Fatalf("got %+v", r)
		}
		if r.Latency <= 0 {
			t.Errorf("latency not measured: %v", r.Latency)
		}
	})

	t.Run("bad status", func(t *testing.T) {
		r := p.Probe(context.Background(), mustValid(t, monitor.Monitor{
			Name: "x", Type: monitor.TypeHTTP, Target: srv.URL + "/500", Interval: time.Minute,
		}))
		if r.OK || r.StatusCode != 500 {
			t.Fatalf("got %+v", r)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		m := mustValid(t, monitor.Monitor{
			Name: "x", Type: monitor.TypeHTTP, Target: srv.URL + "/slow", Interval: time.Minute,
		})
		m.Timeout = 50 * time.Millisecond
		r := p.Probe(context.Background(), m)
		if r.OK || r.Detail != "timeout" {
			t.Fatalf("got %+v", r)
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		r := p.Probe(context.Background(), mustValid(t, monitor.Monitor{
			Name: "x", Type: monitor.TypeHTTP, Target: "http://127.0.0.1:1", Interval: time.Minute,
		}))
		if r.OK {
			t.Fatalf("expected failure, got %+v", r)
		}
	})
}

func TestProbeTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	p := NewProber()

	t.Run("open", func(t *testing.T) {
		r := p.Probe(context.Background(), mustValid(t, monitor.Monitor{
			Name: "x", Type: monitor.TypeTCP, Target: ln.Addr().String(), Interval: time.Minute,
		}))
		if !r.OK || r.Detail != "connected" {
			t.Fatalf("got %+v", r)
		}
	})

	t.Run("closed", func(t *testing.T) {
		r := p.Probe(context.Background(), mustValid(t, monitor.Monitor{
			Name: "x", Type: monitor.TypeTCP, Target: "127.0.0.1:1", Interval: time.Minute,
		}))
		if r.OK {
			t.Fatalf("expected failure, got %+v", r)
		}
	})
}
