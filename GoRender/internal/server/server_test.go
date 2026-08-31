package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/levelcodingdev/gorender/internal/events"
	"github.com/levelcodingdev/gorender/internal/job"
	"github.com/levelcodingdev/gorender/internal/media"
	"github.com/levelcodingdev/gorender/internal/queue"
	"github.com/levelcodingdev/gorender/internal/spec"
	"github.com/levelcodingdev/gorender/internal/worker"
)

type okEncoder struct{}

func (okEncoder) Encode(_ context.Context, p media.Plan, onProgress func(float64)) error {
	onProgress(0.5)
	// write the output path so the artifact handler has a file to serve
	return os.WriteFile(p.Output, []byte("fake-mp4"), 0o644)
}

func mustSpec(jsonBody string) spec.Spec {
	var s spec.Spec
	if err := json.Unmarshal([]byte(jsonBody), &s); err != nil {
		panic(err)
	}
	s.Normalize()
	return s
}

func newTestServer(t *testing.T) (http.Handler, *job.Store, *queue.Mem, func()) {
	t.Helper()
	store := job.NewStore()
	q := queue.NewMem(8)
	br := events.NewBroker()
	dir := t.TempDir()

	pool := &worker.Pool{
		N: 1, Queue: q, Store: store, Encoder: okEncoder{}, Events: br,
		OutDir: dir, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)

	h := New(Deps{Store: store, Queue: q, Events: br, OutDir: dir, FFmpeg: "/x/ffmpeg", FFprobe: "/x/ffprobe"})
	return h, store, q, func() { cancel(); pool.Wait() }
}

const goodSlideshow = `{"template":"slideshow","slideshow":{"images":["a.jpg","b.jpg"],"seconds_per_image":2,"crossfade_seconds":0.5}}`

func TestHealthz(t *testing.T) {
	h, _, _, stop := newTestServer(t)
	defer stop()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("healthz = %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Fatalf("status = %v", body["status"])
	}
}

func TestCreateJobValidationErrors(t *testing.T) {
	h, _, _, stop := newTestServer(t)
	defer stop()
	cases := []struct {
		body string
		code int
	}{
		{`not json`, 400},
		{`{"template":"slideshow"}`, 422},                                            // missing block
		{`{"template":"nope","width":1080,"height":1920}`, 422},                      // unknown template
		{`{"template":"slideshow","extra":1,"slideshow":{"images":["a.jpg"]}}`, 400}, // unknown field
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/jobs", strings.NewReader(c.body)))
		if rec.Code != c.code {
			t.Errorf("body %q → %d, want %d (%s)", c.body, rec.Code, c.code, rec.Body.String())
		}
	}
}

func TestJobLifecycleAndArtifact(t *testing.T) {
	h, store, _, stop := newTestServer(t)
	defer stop()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/jobs", strings.NewReader(goodSlideshow)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var created job.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	// poll GET /v1/jobs/{id} until it finishes
	var final job.Job
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/jobs/"+created.ID, nil))
		_ = json.Unmarshal(r.Body.Bytes(), &final)
		if final.Status == job.StatusSucceeded || final.Status == job.StatusFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if final.Status != job.StatusSucceeded {
		t.Fatalf("job ended %q: %s", final.Status, final.Error)
	}
	_ = store

	// artifact should now download
	ra := httptest.NewRecorder()
	h.ServeHTTP(ra, httptest.NewRequest("GET", "/v1/jobs/"+created.ID+"/artifact", nil))
	if ra.Code != 200 || !bytes.Equal(ra.Body.Bytes(), []byte("fake-mp4")) {
		t.Fatalf("artifact = %d %q", ra.Code, ra.Body.String())
	}
}

func TestArtifactBeforeReady(t *testing.T) {
	h, store, _, stop := newTestServer(t)
	defer stop()
	j := store.Create(mustSpec(goodSlideshow))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/jobs/"+j.ID+"/artifact", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("artifact before ready = %d, want 409", rec.Code)
	}
}

func TestUnknownJob(t *testing.T) {
	h, _, _, stop := newTestServer(t)
	defer stop()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/jobs/deadbeef", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown job = %d", rec.Code)
	}
}
