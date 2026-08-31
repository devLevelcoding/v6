package debug

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServesPprofIndex(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap?debug=1", "/debug/pprof/goroutine?debug=1"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestStartInertWithoutAddr(t *testing.T) {
	Start(Options{MutexFraction: 1, BlockRate: 1}) // no Addr → no listener, no panic
}
