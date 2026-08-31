package bridge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoopbackDispatch(t *testing.T) {
	lb := NewLoopback()
	lb.Handle("echo", func(_ context.Context, m Message) Reply {
		return Reply{Status: 201, Header: http.Header{"X-Echo": {m.Method}}, Body: m.Body}
	})

	rep, err := lb.Request(context.Background(), "echo", Message{Method: "PUT", Body: []byte("hi")})
	if err != nil || rep.Status != 201 || string(rep.Body) != "hi" || rep.Header.Get("X-Echo") != "PUT" {
		t.Fatalf("Request = %+v, %v", rep, err)
	}

	if _, err := lb.Request(context.Background(), "missing", Message{}); !errors.Is(err, ErrNoHandler) {
		t.Fatalf("missing subject = %v, want ErrNoHandler", err)
	}
}

func TestLoopbackContextCancel(t *testing.T) {
	lb := NewLoopback()
	lb.Handle("slow", func(ctx context.Context, _ Message) Reply {
		<-ctx.Done()
		return Reply{Status: 200}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := lb.Request(ctx, "slow", Message{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow handler = %v, want DeadlineExceeded", err)
	}
}

func TestHandlerServeSubject(t *testing.T) {
	lb := NewLoopback()
	lb.Handle("api", func(_ context.Context, m Message) Reply {
		if m.Path != "/api/x" || m.Query != "a=1" || string(m.Body) != "payload" {
			return Reply{Status: 500, Body: []byte("bad message")}
		}
		return Reply{Status: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"ok":true}`)}
	})
	h := Handler{Transport: lb}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/x?a=1", strings.NewReader("payload"))
	h.ServeSubject(rec, req, "api")

	if rec.Code != 200 || rec.Body.String() != `{"ok":true}` || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
}

func TestHandlerBridgeErrors(t *testing.T) {
	h := Handler{Transport: NewLoopback()} // no handlers registered
	rec := httptest.NewRecorder()
	h.ServeSubject(rec, httptest.NewRequest("GET", "/x", nil), "nope")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("no handler → %d, want 502", rec.Code)
	}

	h2 := Handler{Transport: NewLoopback(), MaxBody: 4}
	rec2 := httptest.NewRecorder()
	h2.ServeSubject(rec2, httptest.NewRequest("POST", "/x", strings.NewReader("way too much")), "any")
	if rec2.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body → %d, want 413", rec2.Code)
	}
}
