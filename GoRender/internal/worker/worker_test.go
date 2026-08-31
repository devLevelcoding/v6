package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/levelcodingdev/gorender/internal/events"
	"github.com/levelcodingdev/gorender/internal/job"
	"github.com/levelcodingdev/gorender/internal/media"
	"github.com/levelcodingdev/gorender/internal/queue"
	"github.com/levelcodingdev/gorender/internal/spec"
)

// fakeEncoder reports a couple of progress ticks then returns err.
type fakeEncoder struct {
	err   error
	ticks []float64
}

func (f fakeEncoder) Encode(_ context.Context, _ media.Plan, onProgress func(float64)) error {
	for _, p := range f.ticks {
		onProgress(p)
	}
	return f.err
}

func slideshowSpec() spec.Spec {
	x := 0.5
	s := spec.Spec{Template: spec.TemplateSlideshow, Slideshow: &spec.Slideshow{
		Images: []string{"a.jpg", "b.jpg"}, SecondsPerImage: 4, CrossfadeSeconds: &x,
	}}
	s.Normalize()
	return s
}

func newPool(t *testing.T, enc media.Encoder) (*Pool, *job.Store, *queue.Mem, *events.Broker) {
	t.Helper()
	store := job.NewStore()
	q := queue.NewMem(8)
	br := events.NewBroker()
	p := &Pool{
		N: 2, Queue: q, Store: store, Encoder: enc, Events: br,
		OutDir: t.TempDir(),
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return p, store, q, br
}

func waitStatus(t *testing.T, store *job.Store, id string, want job.Status) *job.Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if j, ok := store.Get(id); ok && j.Status == want {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	j, _ := store.Get(id)
	t.Fatalf("job %s never reached %q (last: %+v)", id, want, j)
	return nil
}

func TestPoolRendersJobToSuccess(t *testing.T) {
	p, store, q, _ := newPool(t, fakeEncoder{ticks: []float64{0.3, 0.9}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	j := store.Create(slideshowSpec())
	if err := q.Push(ctx, j.ID); err != nil {
		t.Fatal(err)
	}

	done := waitStatus(t, store, j.ID, job.StatusSucceeded)
	if done.Progress != 1 {
		t.Fatalf("progress = %v, want 1", done.Progress)
	}
	if done.Artifact == "" || done.StartedAt == nil || done.FinishedAt == nil {
		t.Fatalf("success job under-populated: %+v", done)
	}
	cancel()
	p.Wait()
}

func TestPoolMarksFailureOnEncoderError(t *testing.T) {
	p, store, q, _ := newPool(t, fakeEncoder{err: errors.New("boom")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	j := store.Create(slideshowSpec())
	_ = q.Push(ctx, j.ID)

	failed := waitStatus(t, store, j.ID, job.StatusFailed)
	if failed.Error == "" || failed.Artifact != "" {
		t.Fatalf("failed job wrong shape: %+v", failed)
	}
}

// gateEncoder blocks in Encode until released, and counts concurrent entries.
type gateEncoder struct {
	inFlight atomic.Int64
	maxSeen  atomic.Int64
	release  chan struct{}
}

func (g *gateEncoder) Encode(ctx context.Context, _ media.Plan, _ func(float64)) error {
	n := g.inFlight.Add(1)
	for {
		old := g.maxSeen.Load()
		if n <= old || g.maxSeen.CompareAndSwap(old, n) {
			break
		}
	}
	defer g.inFlight.Add(-1)
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func spec4K() spec.Spec {
	s := spec.Spec{Width: 3840, Height: 2160, Template: spec.TemplateSlideshow,
		Slideshow: &spec.Slideshow{Images: []string{"a.jpg", "b.jpg"}, SecondsPerImage: 4}}
	s.Normalize()
	return s
}

// TestPoolCostWeightedAdmission: budget 4, two 4K jobs (weight 4 each) → only
// one encodes at a time even though 2 workers are free (CoverGo U18).
func TestPoolCostWeightedAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		enc := &gateEncoder{release: make(chan struct{})}
		p, store, q, _ := newPool(t, enc)
		p.CostBudget = 4
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		p.Start(ctx)

		for i := 0; i < 2; i++ {
			j := store.Create(spec4K())
			if err := q.Push(ctx, j.ID); err != nil {
				t.Fatal(err)
			}
		}

		synctest.Wait() // one job in Encode, the other blocked on the semaphore
		if got := enc.inFlight.Load(); got != 1 {
			t.Fatalf("in-flight encodes = %d, want 1 (budget 4, weight 4)", got)
		}

		close(enc.release)
		cancel()
		p.Wait()
		if got := enc.maxSeen.Load(); got != 1 {
			t.Fatalf("peak concurrent 4K encodes = %d, want 1", got)
		}
	})
}

type panicEncoder struct{ n atomic.Int64 }

func (p *panicEncoder) Encode(context.Context, media.Plan, func(float64)) error {
	if p.n.Add(1) == 1 {
		panic("codec exploded")
	}
	return nil
}

// TestPoolSurvivesJobPanic (CoverGo P3): a panicking render fails that job but
// the worker keeps going — a second job still succeeds.
func TestPoolSurvivesJobPanic(t *testing.T) {
	enc := &panicEncoder{}
	p, store, q, _ := newPool(t, enc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	a := store.Create(slideshowSpec())
	_ = q.Push(ctx, a.ID)
	failed := waitStatus(t, store, a.ID, job.StatusFailed)
	if failed.Error == "" {
		t.Fatalf("panicked job should carry an error: %+v", failed)
	}

	b := store.Create(slideshowSpec())
	_ = q.Push(ctx, b.ID)
	done := waitStatus(t, store, b.ID, job.StatusSucceeded)
	if done.Progress != 1 {
		t.Fatalf("second job after a panic = %+v", done)
	}
}

// TestPoolCleansPartialArtifactOnFailure (CoverGo U19): a job that fails
// mid-encode must not leave a downloadable half-file behind.
func TestPoolCleansPartialArtifactOnFailure(t *testing.T) {
	dir := t.TempDir()
	// encoder writes a partial file, then errors
	enc := encFunc(func(_ context.Context, p media.Plan, _ func(float64)) error {
		_ = os.WriteFile(p.Output, []byte("half a video"), 0o644)
		return errors.New("encode died")
	})
	store := job.NewStore()
	q := queue.NewMem(4)
	p := &Pool{N: 1, Queue: q, Store: store, Encoder: enc, OutDir: dir,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	j := store.Create(slideshowSpec())
	_ = q.Push(ctx, j.ID)
	failed := waitStatus(t, store, j.ID, job.StatusFailed)
	if failed.Artifact != "" {
		t.Fatalf("failed job should have no artifact: %q", failed.Artifact)
	}
	if _, err := os.Stat(filepath.Join(dir, j.ID+".mp4")); !os.IsNotExist(err) {
		t.Fatalf("partial artifact not cleaned up (stat err: %v)", err)
	}
}

type encFunc func(context.Context, media.Plan, func(float64)) error

func (f encFunc) Encode(c context.Context, p media.Plan, cb func(float64)) error { return f(c, p, cb) }

func TestPoolFailsOnBadSpec(t *testing.T) {
	// concat needs a prober; pool has none → plan.Build errors → job fails
	p, store, q, _ := newPool(t, fakeEncoder{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Start(ctx)

	s := spec.Spec{Template: spec.TemplateConcat, Concat: &spec.Concat{Clips: []string{"a.mp4", "b.mp4"}}}
	s.Normalize()
	j := store.Create(s)
	_ = q.Push(ctx, j.ID)

	waitStatus(t, store, j.ID, job.StatusFailed)
}

func TestPoolEmitsProgressEvents(t *testing.T) {
	p, store, q, br := newPool(t, fakeEncoder{ticks: []float64{0.5}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	j := store.Create(slideshowSpec())
	ch, unsub := br.Subscribe(j.ID)
	defer unsub()

	p.Start(ctx)
	_ = q.Push(ctx, j.ID)

	var sawRunning, sawDone bool
	timeout := time.After(2 * time.Second)
	for !sawDone {
		select {
		case e := <-ch:
			if e.Status == string(job.StatusRunning) {
				sawRunning = true
			}
			if e.Status == string(job.StatusSucceeded) {
				sawDone = true
			}
		case <-timeout:
			t.Fatalf("did not see terminal event (running=%v)", sawRunning)
		}
	}
	if !sawRunning {
		t.Fatal("never saw a running event")
	}
}
