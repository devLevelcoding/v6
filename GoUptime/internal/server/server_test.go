package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/levelcodingdev/gouptime/internal/check"
	"github.com/levelcodingdev/gouptime/internal/history"
	"github.com/levelcodingdev/gouptime/internal/incident"
	"github.com/levelcodingdev/gouptime/internal/monitor"
)

type stubRunner struct {
	syncs int
	res   check.Result
	err   error
}

func (s *stubRunner) RunNow(context.Context, string) (check.Result, error) { return s.res, s.err }
func (s *stubRunner) Sync()                                                { s.syncs++ }

func newTestServer(t *testing.T) (*Server, *monitor.MemStore, *stubRunner, *incident.Detector, *history.Ring) {
	t.Helper()
	store := monitor.NewMemStore()
	runner := &stubRunner{}
	det := incident.NewDetector(incident.DefaultPolicy())
	ring := history.NewRing(100)
	return New(store, runner, det, ring), store, runner, det, ring
}

func do(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func TestHealthAndVersion(t *testing.T) {
	srv, _, _, _, _ := newTestServer(t)
	if w := do(t, srv, "GET", "/healthz", ""); w.Code != 200 {
		t.Fatalf("healthz = %d", w.Code)
	}
	w := do(t, srv, "GET", "/v1/version", "")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(Version)) {
		t.Fatalf("version = %d %s", w.Code, w.Body)
	}
}

func TestMonitorCRUDRoundTrip(t *testing.T) {
	srv, _, runner, _, _ := newTestServer(t)

	w := do(t, srv, "POST", "/v1/monitors", `{"name":"api","type":"http","target":"https://example.com","interval_seconds":30,"enabled":true}`)
	if w.Code != 201 {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	var created monitorDTO
	mustJSON(t, w, &created)
	if created.ID == "" || created.IntervalSecs != 30 {
		t.Fatalf("created wrong: %+v", created)
	}
	if runner.syncs != 1 {
		t.Errorf("create should trigger Sync, got %d", runner.syncs)
	}

	w = do(t, srv, "GET", "/v1/monitors/"+created.ID, "")
	if w.Code != 200 {
		t.Fatalf("get = %d", w.Code)
	}

	w = do(t, srv, "PUT", "/v1/monitors/"+created.ID,
		`{"name":"api-v2","type":"http","target":"https://example.com","interval_seconds":60,"enabled":false}`)
	if w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	var updated monitorDTO
	mustJSON(t, w, &updated)
	if updated.Name != "api-v2" || updated.IntervalSecs != 60 || updated.Enabled {
		t.Fatalf("update wrong: %+v", updated)
	}

	w = do(t, srv, "GET", "/v1/monitors", "")
	var list struct {
		Monitors []monitorDTO `json:"monitors"`
	}
	mustJSON(t, w, &list)
	if len(list.Monitors) != 1 {
		t.Fatalf("list len = %d", len(list.Monitors))
	}

	w = do(t, srv, "DELETE", "/v1/monitors/"+created.ID, "")
	if w.Code != 204 {
		t.Fatalf("delete = %d", w.Code)
	}
	if w := do(t, srv, "GET", "/v1/monitors/"+created.ID, ""); w.Code != 404 {
		t.Fatalf("get after delete = %d", w.Code)
	}
}

func TestCreateMonitorValidationError(t *testing.T) {
	srv, _, _, _, _ := newTestServer(t)
	w := do(t, srv, "POST", "/v1/monitors", `{"name":"","type":"http","target":"https://x.io","interval_seconds":30}`)
	if w.Code != 400 {
		t.Fatalf("want 400, got %d %s", w.Code, w.Body)
	}
}

func TestCreateMonitorUnknownField(t *testing.T) {
	srv, _, _, _, _ := newTestServer(t)
	w := do(t, srv, "POST", "/v1/monitors", `{"name":"api","type":"http","target":"https://x.io","interval_seconds":30,"bogus":1}`)
	if w.Code != 400 {
		t.Fatalf("want 400 for unknown field, got %d", w.Code)
	}
}

func TestCheckNowEndpoint(t *testing.T) {
	srv, store, runner, _, _ := newTestServer(t)
	m, _ := store.Create(monitor.Monitor{Name: "api", Type: monitor.TypeHTTP, Target: "https://x.io", Interval: time.Minute, Enabled: true})
	runner.res = check.Result{MonitorID: m.ID, OK: true, StatusCode: 200, Detail: "200 OK"}

	w := do(t, srv, "POST", "/v1/monitors/"+m.ID+"/check", "")
	if w.Code != 200 {
		t.Fatalf("check = %d %s", w.Code, w.Body)
	}
	var res check.Result
	mustJSON(t, w, &res)
	if !res.OK || res.StatusCode != 200 {
		t.Fatalf("check result wrong: %+v", res)
	}
}

func TestResultsAndSummaryEndpoints(t *testing.T) {
	srv, store, _, _, ring := newTestServer(t)
	m, _ := store.Create(monitor.Monitor{Name: "api", Type: monitor.TypeHTTP, Target: "https://x.io", Interval: time.Minute, Enabled: true})
	for i := 0; i < 3; i++ {
		ring.Record(check.Result{MonitorID: m.ID, At: time.Now().Add(time.Duration(i) * time.Second), OK: true, Latency: time.Millisecond})
	}

	w := do(t, srv, "GET", "/v1/monitors/"+m.ID+"/results", "")
	var got struct {
		Results []check.Result `json:"results"`
	}
	mustJSON(t, w, &got)
	if len(got.Results) != 3 {
		t.Fatalf("results len = %d", len(got.Results))
	}

	w = do(t, srv, "GET", "/v1/monitors/"+m.ID+"/summary", "")
	var s history.Summary
	mustJSON(t, w, &s)
	if s.Total != 3 || s.Up != 3 {
		t.Fatalf("summary = %+v", s)
	}

	if w := do(t, srv, "GET", "/v1/monitors/missing/results", ""); w.Code != 404 {
		t.Fatalf("results for missing monitor = %d", w.Code)
	}
}

func TestIncidentsEndpoint(t *testing.T) {
	srv, _, _, det, _ := newTestServer(t)
	// DefaultPolicy opens after 3 failures.
	for i := 0; i < 3; i++ {
		det.Observe(check.Result{MonitorID: "m1", OK: false, At: time.Now(), Detail: "timeout"})
	}
	w := do(t, srv, "GET", "/v1/incidents", "")
	var got struct {
		Incidents []incident.Incident `json:"incidents"`
	}
	mustJSON(t, w, &got)
	if len(got.Incidents) != 1 || got.Incidents[0].MonitorID != "m1" {
		t.Fatalf("incidents = %+v", got.Incidents)
	}

	w = do(t, srv, "GET", "/v1/incidents?monitor=other", "")
	mustJSON(t, w, &got)
	if len(got.Incidents) != 0 {
		t.Fatalf("filtered incidents = %+v", got.Incidents)
	}
}

func mustJSON(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %s: %v", w.Body, err)
	}
}
