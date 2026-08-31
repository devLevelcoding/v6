package edge

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/group"
)

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestEdgeCapturesUpstream5xx(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "kaboom", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	groups := group.NewStore(10)
	p := New(groups, nil)
	if err := p.SetRoutes([]Route{{Host: "", Upstream: upstream.URL, ProjectID: "proj1"}}); err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(p)
	defer front.Close()

	resp, err := http.Get(front.URL + "/checkout")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("client saw %d, expected the upstream 500 to pass through", resp.StatusCode)
	}

	waitFor(t, func() bool { return len(groups.List(group.Filter{ProjectID: "proj1"})) == 1 }, "captured issue")
	iss := groups.List(group.Filter{ProjectID: "proj1"})[0]
	if iss.Level != event.LevelError {
		t.Errorf("captured level = %q", iss.Level)
	}
	evs, _ := groups.Events(iss.ID, 1)
	if len(evs) != 1 || evs[0].Tags["http.path"] != "/checkout" {
		t.Fatalf("captured event = %+v", evs)
	}
}

func TestEdgeCapturesUpstreamDown(t *testing.T) {
	groups := group.NewStore(10)
	p := New(groups, nil)
	// nothing is listening here
	if err := p.SetRoutes([]Route{{Upstream: "http://127.0.0.1:1", ProjectID: "proj1"}}); err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(p)
	defer front.Close()

	resp, err := http.Get(front.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}

	waitFor(t, func() bool { return len(groups.List(group.Filter{ProjectID: "proj1"})) == 1 }, "captured issue")
	iss := groups.List(group.Filter{ProjectID: "proj1"})[0]
	if iss.Level != event.LevelFatal {
		t.Errorf("upstream-down level = %q, want fatal", iss.Level)
	}
}

func TestEdgeHostRouting(t *testing.T) {
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("A")) }))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("B")) }))
	defer b.Close()

	p := New(group.NewStore(10), nil)
	if err := p.SetRoutes([]Route{
		{Host: "a.example.com", Upstream: a.URL, ProjectID: "a"},
		{Host: "b.example.com", Upstream: b.URL, ProjectID: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(p)
	defer front.Close()

	get := func(host string) string {
		req, _ := http.NewRequest("GET", front.URL, nil)
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 1)
		resp.Body.Read(buf)
		return string(buf)
	}
	if got := get("a.example.com"); got != "A" {
		t.Errorf("a.example.com routed to %q", got)
	}
	if got := get("b.example.com"); got != "B" {
		t.Errorf("b.example.com routed to %q", got)
	}
}

func TestEdgeNoRoute(t *testing.T) {
	p := New(group.NewStore(10), nil)
	p.SetRoutes(nil)
	front := httptest.NewServer(p)
	defer front.Close()

	resp, err := http.Get(front.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}
