package ingest

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/project"
)

func setup(t *testing.T) (*httptest.Server, project.Project, *group.Store) {
	t.Helper()
	projects := project.NewMemStore()
	groups := group.NewStore(50)
	p, err := projects.Create("Web App", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(projects, groups, nil).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, p, groups
}

func mustDo(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func envelopeBody(msg string) string {
	item := fmt.Sprintf(`{"message":%q,"level":"error"}`, msg)
	return fmt.Sprintf("{}\n{\"type\":\"event\",\"length\":%d}\n%s\n", len(item), item)
}

func TestEnvelopeHappyPath(t *testing.T) {
	srv, p, groups := setup(t)

	req, _ := http.NewRequest("POST", srv.URL+"/api/"+p.ID+"/envelope/", bytes.NewBufferString(envelopeBody("kaboom")))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_key="+p.Keys[0].PublicKey+", sentry_version=7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct{ ID string }
	json.NewDecoder(resp.Body).Decode(&out)
	if out.ID == "" {
		t.Error("no event id in response")
	}

	issues := groups.List(group.Filter{ProjectID: p.ID})
	if len(issues) != 1 || issues[0].Title != "kaboom" {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestEnvelopeAuthViaQueryParam(t *testing.T) {
	srv, p, _ := setup(t)
	url := fmt.Sprintf("%s/api/%s/envelope/?sentry_key=%s", srv.URL, p.ID, p.Keys[0].PublicKey)
	resp, err := http.Post(url, "application/x-sentry-envelope", bytes.NewBufferString(envelopeBody("q")))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestEnvelopeBadKey(t *testing.T) {
	srv, p, _ := setup(t)
	req, _ := http.NewRequest("POST", srv.URL+"/api/"+p.ID+"/envelope/", bytes.NewBufferString(envelopeBody("x")))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_key=wrongkey")
	resp := mustDo(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestEnvelopeUnknownProject(t *testing.T) {
	srv, p, _ := setup(t)
	req, _ := http.NewRequest("POST", srv.URL+"/api/deadbeef/envelope/", bytes.NewBufferString(envelopeBody("x")))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_key="+p.Keys[0].PublicKey)
	resp := mustDo(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestEnvelopeGzip(t *testing.T) {
	srv, p, groups := setup(t)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(envelopeBody("compressed")))
	zw.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/api/"+p.ID+"/envelope/", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_key="+p.Keys[0].PublicKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := groups.List(group.Filter{ProjectID: p.ID}); len(got) != 1 || got[0].Title != "compressed" {
		t.Fatalf("issues = %+v", got)
	}
}

func TestLegacyStoreEndpoint(t *testing.T) {
	srv, p, groups := setup(t)
	body := `{"message":"legacy path","level":"warning"}`
	req, _ := http.NewRequest("POST", srv.URL+"/api/"+p.ID+"/store/", bytes.NewBufferString(body))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_key="+p.Keys[0].PublicKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := groups.List(group.Filter{ProjectID: p.ID}); len(got) != 1 || got[0].Title != "legacy path" {
		t.Fatalf("issues = %+v", got)
	}
}

func TestEnvelopeNoEventItemStillAccepted(t *testing.T) {
	srv, p, groups := setup(t)
	body := "{}\n{\"type\":\"session\",\"length\":2}\n{}\n"
	req, _ := http.NewRequest("POST", srv.URL+"/api/"+p.ID+"/envelope/", bytes.NewBufferString(body))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_key="+p.Keys[0].PublicKey)
	resp := mustDo(t, req)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := groups.List(group.Filter{ProjectID: p.ID}); len(got) != 0 {
		t.Fatalf("expected no issues, got %+v", got)
	}
}
