package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/levelcodingdev/gosnow/internal/catalog"
	"github.com/levelcodingdev/gosnow/internal/query"
	"github.com/levelcodingdev/gosnow/internal/warehouse"
)

func newTestServer() *Server {
	cat := catalog.NewMemory()
	return New(cat, warehouse.NewManager(), query.NewCoordinator(cat))
}

func do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	newTestServer().ServeHTTP(rec, r)
	return rec
}

func TestHealth(t *testing.T) {
	rec := do(t, http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health = %d %s", rec.Code, rec.Body.String())
	}
}

func TestStatementSelect(t *testing.T) {
	rec := do(t, http.MethodPost, "/v1/statements", `{"sql":"SELECT 1"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"rowCount":1`) {
		t.Fatalf("statement = %d %s", rec.Code, rec.Body.String())
	}
}

func TestStatementUnsupported(t *testing.T) {
	rec := do(t, http.MethodPost, "/v1/statements", `{"sql":"MERGE INTO x"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateWarehouse(t *testing.T) {
	rec := do(t, http.MethodPost, "/v1/warehouses", `{"name":"wh1","size":"small"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"wh1"`) {
		t.Fatalf("create wh = %d %s", rec.Code, rec.Body.String())
	}
}
