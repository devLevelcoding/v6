package server_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/levelcodingdev/godoc/internal/server"
)

func newServer(t *testing.T, cfg server.Config) *httptest.Server {
	t.Helper()
	cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(server.New(cfg))
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, srv *httptest.Server, path, body string, hdr http.Header) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestCSVEndpoint(t *testing.T) {
	srv := newServer(t, server.Config{})
	resp := post(t, srv, "/v1/csv", `{"columns":["a","b"],"rows":[[1,2],[3,4]]}`, nil)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content-type %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".csv") {
		t.Fatalf("content-disposition %q", cd)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "a,b\n1,2\n3,4\n" {
		t.Fatalf("body %q", b)
	}
}

func TestXMLEndpoint(t *testing.T) {
	srv := newServer(t, server.Config{})
	resp := post(t, srv, "/v1/xml", `{"root":"doc","data":{"item":[1,2],"@v":"1"}}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/xml") {
		t.Fatalf("status %d ct %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	b, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(b, new(struct{ XMLName xml.Name })); err != nil {
		t.Fatalf("not XML: %v\n%s", err, b)
	}
}

func TestPDFEndpoint(t *testing.T) {
	srv := newServer(t, server.Config{})
	resp := post(t, srv, "/v1/pdf",
		`{"template":"table","title":"T","data":{"columns":["x"],"rows":[["a"],["b"]]}}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("status %d ct %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	b, _ := io.ReadAll(resp.Body)
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
}

func TestRenderUnifiedEndpoint(t *testing.T) {
	srv := newServer(t, server.Config{})
	resp := post(t, srv, "/v1/render",
		`{"format":"csv","filename":"custom.csv","csv":{"rows":[["only"]],"no_header":true}}`, nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), `"custom.csv"`) {
		t.Fatalf("custom filename ignored: %q", resp.Header.Get("Content-Disposition"))
	}
}

func TestValidationAndParseErrors(t *testing.T) {
	srv := newServer(t, server.Config{})
	cases := []struct {
		path, body string
		code       int
	}{
		{"/v1/csv", `not json`, 400},
		{"/v1/csv", `{}`, 422},                               // no rows/records
		{"/v1/xml", `{}`, 422},                               // data missing
		{"/v1/pdf", `{"template":"table"}`, 422},             // no data
		{"/v1/render", `{"format":"docx"}`, 422},             // unknown format
		{"/v1/pdf", `{"template":"unknown","data":{}}`, 422}, // bad template
	}
	for _, c := range cases {
		resp := post(t, srv, c.path, c.body, nil)
		resp.Body.Close()
		if resp.StatusCode != c.code {
			t.Errorf("POST %s %q → %d, want %d", c.path, c.body, resp.StatusCode, c.code)
		}
	}
}

func TestTokenAuth(t *testing.T) {
	srv := newServer(t, server.Config{Token: "sekret"})
	body := `{"rows":[[1]]}`

	no := post(t, srv, "/v1/csv", body, nil)
	no.Body.Close()
	if no.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", no.StatusCode)
	}
	yes := post(t, srv, "/v1/csv", body, http.Header{"Authorization": {"Bearer sekret"}})
	yes.Body.Close()
	if yes.StatusCode != 200 {
		t.Fatalf("with token = %d", yes.StatusCode)
	}
}

func TestTemplatesAndHealth(t *testing.T) {
	srv := newServer(t, server.Config{})

	r1, err := http.Get(srv.URL + "/_godoc/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer r1.Body.Close()
	if r1.StatusCode != 200 {
		t.Fatal("healthz")
	}

	r2, err := http.Get(srv.URL + "/v1/templates")
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	var tpl map[string][]string
	json.NewDecoder(r2.Body).Decode(&tpl)
	if !contains(tpl["pdf"], "invoice") || !contains(tpl["xml"], "payroll") {
		t.Fatalf("templates = %+v", tpl)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
