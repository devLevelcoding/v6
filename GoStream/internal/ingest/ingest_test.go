package ingest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/levelcodingdev/gostream/internal/hub"
)

func newMux(h Handler) *http.ServeMux {
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func TestPublishDeliversToSubscribers(t *testing.T) {
	hb := hub.New(hub.Config{SendBuffer: 4})
	c := hb.Add(hub.Meta{})
	hb.Subscribe(c, "events")
	mux := newMux(Handler{Hub: hb})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/pub/events", strings.NewReader(`{"n":1}`)))
	if rec.Code != 200 {
		t.Fatalf("publish = %d", rec.Code)
	}
	var res hub.Result
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Delivered != 1 {
		t.Fatalf("delivered = %d, want 1", res.Delivered)
	}
	select {
	case msg := <-c.Out():
		if !strings.Contains(string(msg), `"topic":"events"`) || !strings.Contains(string(msg), `"n":1`) {
			t.Fatalf("bad wrapped message: %s", msg)
		}
	default:
		t.Fatal("subscriber got nothing")
	}
}

func TestPublishTokenAuth(t *testing.T) {
	hb := hub.New(hub.Config{})
	mux := newMux(Handler{Hub: hb, Token: "sekret"})

	no := httptest.NewRecorder()
	mux.ServeHTTP(no, httptest.NewRequest("POST", "/pub/x", strings.NewReader("1")))
	if no.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", no.Code)
	}

	yes := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/pub/x?token=sekret", strings.NewReader("1"))
	mux.ServeHTTP(yes, req)
	if yes.Code != 200 {
		t.Fatalf("with ?token = %d", yes.Code)
	}

	hdr := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/pub/x", strings.NewReader("1"))
	req2.Header.Set("Authorization", "Bearer sekret")
	mux.ServeHTTP(hdr, req2)
	if hdr.Code != 200 {
		t.Fatalf("with bearer = %d", hdr.Code)
	}
}

func TestPublishBodyTooLarge(t *testing.T) {
	hb := hub.New(hub.Config{})
	mux := newMux(Handler{Hub: hb, MaxBody: 8})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/pub/x", strings.NewReader(strings.Repeat("a", 100))))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized = %d, want 413", rec.Code)
	}
}
