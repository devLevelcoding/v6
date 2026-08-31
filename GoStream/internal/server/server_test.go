package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/levelcodingdev/gostream/internal/hub"
	"github.com/levelcodingdev/gostream/internal/proto"
	"github.com/levelcodingdev/gostream/internal/server"
	"github.com/levelcodingdev/gostream/internal/wstest"
)

func newServer(t *testing.T, cfg server.Config) (*httptest.Server, *hub.Hub) {
	t.Helper()
	h := hub.New(hub.Config{SendBuffer: 16})
	cfg.Hub = h
	cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg.PingInterval = time.Hour // don't ping during a short test
	srv := httptest.NewServer(server.New(cfg))
	t.Cleanup(srv.Close)
	return srv, h
}

func dial(t *testing.T, srv *httptest.Server, pathAndQuery string) *wstest.Client {
	t.Helper()
	cl, err := wstest.Dial(wstest.WSURL(srv.URL, pathAndQuery))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	cl.SetDeadline(time.Now().Add(3 * time.Second))
	return cl
}

func expect(t *testing.T, cl *wstest.Client, typ string) proto.Event {
	t.Helper()
	var e proto.Event
	if err := cl.ReadJSON(&e); err != nil {
		t.Fatalf("read %s: %v", typ, err)
	}
	if e.Type != typ {
		t.Fatalf("got event %q, want %q (%+v)", e.Type, typ, e)
	}
	return e
}

func TestSubscribeThenReceivePublished(t *testing.T) {
	srv, _ := newServer(t, server.Config{})
	cl := dial(t, srv, "/ws")

	expect(t, cl, "welcome")
	if err := cl.WriteJSON(proto.Command{Type: "subscribe", Topic: "room"}); err != nil {
		t.Fatal(err)
	}
	expect(t, cl, "subscribed")

	// publish via HTTP
	resp, err := http.Post(srv.URL+"/pub/room", "application/json", strings.NewReader(`{"hello":"world"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	msg := expect(t, cl, "message")
	if msg.Topic != "room" || string(msg.Data) != `{"hello":"world"}` {
		t.Fatalf("message = %+v", msg)
	}
}

func TestAutoSubscribeFromQuery(t *testing.T) {
	srv, _ := newServer(t, server.Config{})
	cl := dial(t, srv, "/ws?topics=a,b")
	expect(t, cl, "welcome")

	http.Post(srv.URL+"/pub/b", "", strings.NewReader("42"))
	msg := expect(t, cl, "message")
	if msg.Topic != "b" {
		t.Fatalf("expected a message on b, got %+v", msg)
	}
}

func TestClientPublishGatedByConfig(t *testing.T) {
	srv, _ := newServer(t, server.Config{AllowClientPublish: false})
	cl := dial(t, srv, "/ws")
	expect(t, cl, "welcome")

	cl.WriteJSON(proto.Command{Type: "publish", Topic: "x", Data: json.RawMessage(`1`)})
	if e := expect(t, cl, "error"); !strings.Contains(e.Error, "disabled") {
		t.Fatalf("error = %q", e.Error)
	}
}

func TestClientPublishWhenAllowed(t *testing.T) {
	srv, _ := newServer(t, server.Config{AllowClientPublish: true})
	pub := dial(t, srv, "/ws")
	expect(t, pub, "welcome")
	sub := dial(t, srv, "/ws?topics=live")
	expect(t, sub, "welcome")

	pub.WriteJSON(proto.Command{Type: "publish", Topic: "live", Data: json.RawMessage(`{"tick":7}`)})
	msg := expect(t, sub, "message")
	if string(msg.Data) != `{"tick":7}` {
		t.Fatalf("relayed data = %s", msg.Data)
	}
}

func TestWSTokenGate(t *testing.T) {
	srv, _ := newServer(t, server.Config{WSToken: "letmein"})
	if _, err := wstest.Dial(wstest.WSURL(srv.URL, "/ws")); err == nil {
		t.Fatal("no token should be rejected at the handshake")
	}
	cl := dial(t, srv, "/ws?token=letmein")
	expect(t, cl, "welcome")
}

func TestAdminEndpoints(t *testing.T) {
	srv, h := newServer(t, server.Config{})
	c := h.Add(hub.Meta{Subject: "u1"})
	h.Subscribe(c, "room")

	get := func(path string) map[string]any {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return m
	}
	if get("/_gostream/healthz")["status"] != "ok" {
		t.Fatal("healthz not ok")
	}
	if get("/_gostream/stats")["clients"].(float64) != 1 {
		t.Fatal("stats clients != 1")
	}

	resp, _ := http.Get(srv.URL + "/_gostream/presence?topic=room")
	var p []map[string]any
	json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()
	if len(p) != 1 || p[0]["subject"] != "u1" {
		t.Fatalf("presence?topic=room = %+v", p)
	}
}

func TestDisconnectCleansUp(t *testing.T) {
	srv, h := newServer(t, server.Config{})
	cl := dial(t, srv, "/ws?topics=room")
	expect(t, cl, "welcome")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && h.Stats().Clients == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	cl.Close()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.Stats().Clients == 0 && h.TopicSize("room") == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hub not cleaned up after disconnect: %+v", h.Stats())
}
