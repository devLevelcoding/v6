package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/project"
)

func newAPI(t *testing.T) (*httptest.Server, project.Store, *group.Store) {
	t.Helper()
	projects := project.NewMemStore()
	groups := group.NewStore(50)
	mux := http.NewServeMux()
	New(projects, groups, "https://flare.example.com").Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, projects, groups
}

func getJSON(t *testing.T, url string, v any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil {
		json.NewDecoder(resp.Body).Decode(v)
	}
	return resp.StatusCode
}

func TestProjectCreateAndList(t *testing.T) {
	srv, _, _ := newAPI(t)

	resp, err := http.Post(srv.URL+"/api/0/projects/", "application/json",
		bytes.NewBufferString(`{"name":"Checkout","platform":"go"}`))
	if err != nil {
		t.Fatal(err)
	}
	var created projectView
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	if created.Slug != "checkout" || created.DSN == "" {
		t.Fatalf("created = %+v", created)
	}

	var list []projectView
	if code := getJSON(t, srv.URL+"/api/0/projects/", &list); code != 200 || len(list) != 1 {
		t.Fatalf("list code=%d len=%d", code, len(list))
	}

	if code := getJSON(t, srv.URL+"/api/0/projects/nope/", nil); code != 404 {
		t.Errorf("missing project code = %d", code)
	}
}

func TestIssueLifecycleOverAPI(t *testing.T) {
	srv, projects, groups := newAPI(t)
	p, _ := projects.Create("app", "python")

	// two events → one issue seen twice (shared in-app frame pins the group)
	for _, v := range []string{"first", "second"} {
		groups.Ingest(p.ID, event.Event{
			Level: event.LevelError,
			Exceptions: []event.Exception{{
				Type:   "DBError",
				Value:  v,
				Frames: []event.Frame{{Module: "app.db", Function: "query", InApp: true}},
			}},
		})
	}

	var issues []group.Issue
	if code := getJSON(t, srv.URL+"/api/0/projects/"+p.ID+"/issues/", &issues); code != 200 {
		t.Fatalf("issues code = %d", code)
	}
	if len(issues) != 1 || issues[0].TimesSeen != 2 {
		t.Fatalf("issues = %+v", issues)
	}
	id := issues[0].ID

	var one group.Issue
	if code := getJSON(t, srv.URL+"/api/0/issues/"+id+"/", &one); code != 200 || one.Title != "DBError: second" {
		t.Fatalf("issue detail code=%d %+v", code, one)
	}

	// resolve it
	req, _ := http.NewRequest("PUT", srv.URL+"/api/0/issues/"+id+"/", bytes.NewBufferString(`{"status":"resolved"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var resolved group.Issue
	json.NewDecoder(resp.Body).Decode(&resolved)
	resp.Body.Close()
	if resp.StatusCode != 200 || resolved.Status != group.StatusResolved {
		t.Fatalf("resolve → %d %+v", resp.StatusCode, resolved)
	}

	// events + latest
	var evs []event.Event
	if code := getJSON(t, srv.URL+"/api/0/issues/"+id+"/events/", &evs); code != 200 || len(evs) != 2 {
		t.Fatalf("events code=%d len=%d", code, len(evs))
	}
	var latest event.Event
	if code := getJSON(t, srv.URL+"/api/0/issues/"+id+"/events/latest/", &latest); code != 200 {
		t.Fatalf("latest code = %d", code)
	}
	if latest.Exceptions[0].Value != "second" {
		t.Errorf("latest event = %+v", latest)
	}
}

func TestUpdateIssueBadStatus(t *testing.T) {
	srv, projects, groups := newAPI(t)
	p, _ := projects.Create("app", "")
	iss, _ := groups.Ingest(p.ID, event.Event{Message: "x", Level: event.LevelError})

	req, _ := http.NewRequest("PUT", srv.URL+"/api/0/issues/"+iss.ID+"/", bytes.NewBufferString(`{"status":"banana"}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
