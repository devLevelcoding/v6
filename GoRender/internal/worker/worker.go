// Package worker is the render pool: N goroutines that claim jobs, compile each
// Spec to an ffmpeg Plan, run it, and record the result. Concurrency = N ffmpeg
// processes, which is the point — Python's moviepy is GIL-serial.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/levelcodingdev/gorender/internal/events"
	"github.com/levelcodingdev/gorender/internal/job"
	"github.com/levelcodingdev/gorender/internal/media"
	"github.com/levelcodingdev/gorender/internal/plan"
	"github.com/levelcodingdev/gorender/internal/queue"
)

// Pool runs jobs from the queue.
type Pool struct {
	N       int
	Queue   *queue.Mem
	Store   *job.Store
	Encoder media.Encoder
	Prober  media.Prober // may be nil if only slideshow is used
	Events  *events.Broker
	OutDir  string
	Log     *slog.Logger

	// JobTimeout caps a single render. 0 = no cap.
	JobTimeout time.Duration

	// CostBudget is the total encode weight that may run concurrently
	// (CoverGo U18). Each job acquires spec.Weight() (1–4). 0 → 2*N, so a
	// spread of light jobs still fills the pool while heavy ones serialise.
	CostBudget int64

	wg   sync.WaitGroup
	sem  *semaphore.Weighted
	once sync.Once
}

func (p *Pool) budget() int64 {
	if p.CostBudget > 0 {
		return p.CostBudget
	}
	n := int64(p.N)
	if n < 1 {
		n = 1
	}
	return 2 * n
}

// Start launches N workers. They stop when ctx is cancelled; Wait blocks for
// their exit.
func (p *Pool) Start(ctx context.Context) {
	n := p.N
	if n < 1 {
		n = 1
	}
	p.once.Do(func() { p.sem = semaphore.NewWeighted(p.budget()) })
	for i := 0; i < n; i++ {
		p.wg.Add(1)
		go func(id int) {
			defer p.wg.Done()
			p.loop(ctx, id)
		}(i)
	}
	p.Log.Info("render pool started", "workers", n, "out", p.OutDir)
}

// Wait blocks until every worker has returned.
func (p *Pool) Wait() { p.wg.Wait() }

func (p *Pool) loop(ctx context.Context, id int) {
	for {
		jobID, ok := p.Queue.Claim(ctx)
		if !ok {
			return
		}
		p.runGuarded(ctx, jobID, id)
	}
}

// runGuarded contains a panicking job (CoverGo P3): the render fails, the worker
// lives. Without this, one bad Spec or a codec-library panic silently removes a
// worker from the pool for the process's lifetime.
func (p *Pool) runGuarded(ctx context.Context, jobID string, id int) {
	defer func() {
		if rec := recover(); rec != nil {
			p.Log.Error("render panic", "job", jobID, "worker", id, "recover", rec,
				"stack", string(debug.Stack()))
			p.fail(jobID, fmt.Errorf("render panicked: %v", rec), p.Log)
		}
	}()
	p.run(ctx, jobID, id)
}

func (p *Pool) run(ctx context.Context, jobID string, workerID int) {
	j, ok := p.Store.Get(jobID)
	if !ok {
		p.Log.Warn("claimed unknown job", "job", jobID)
		return
	}
	log := p.Log.With("job", jobID, "worker", workerID, "template", j.Spec.Template)
	log.Info("render start")

	p.Store.MarkRunning(jobID)
	p.publish(jobID, string(job.StatusRunning), 0, "")

	runCtx := ctx
	if p.JobTimeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, p.JobTimeout)
		defer cancel()
	}

	out := filepath.Join(p.OutDir, jobID+".mp4")
	pl, err := plan.Build(runCtx, j.Spec, p.Prober, out)
	if err != nil {
		p.fail(jobID, fmt.Errorf("building plan: %w", err), log)
		return
	}

	// Cost-weighted admission (CoverGo U18): a 4K job holds 4 units of the
	// pool's budget, a small one holds 1 — so several heavy renders queue
	// instead of thrashing the box.
	w := clampWeight(j.Spec.Weight(), p.budget())
	if err := p.sem.Acquire(runCtx, w); err != nil {
		p.fail(jobID, fmt.Errorf("waiting for encode slot: %w", err), log)
		return
	}
	defer p.sem.Release(w)

	err = p.Encoder.Encode(runCtx, pl, func(frac float64) {
		p.Store.Update(jobID, func(j *job.Job) { j.Progress = frac })
		p.publish(jobID, string(job.StatusRunning), frac, "")
	})
	if err != nil {
		// Cancelled or failed mid-encode: don't leave a half-written .mp4
		// behind for a client to download (CoverGo U19 — cancellable lifecycle).
		if rmErr := os.Remove(out); rmErr != nil && !os.IsNotExist(rmErr) {
			log.Warn("could not remove partial artifact", "path", out, "err", rmErr)
		}
		p.fail(jobID, err, log)
		return
	}

	p.Store.MarkDone(jobID, out, nil)
	p.publish(jobID, string(job.StatusSucceeded), 1, "")
	log.Info("render done", "artifact", out, "duration", pl.Duration)
}

// clampWeight keeps a job's weight from exceeding the whole budget (which would
// make Acquire block forever).
func clampWeight(w, budget int64) int64 {
	if w < 1 {
		return 1
	}
	if w > budget {
		return budget
	}
	return w
}

func (p *Pool) fail(jobID string, err error, log *slog.Logger) {
	p.Store.MarkDone(jobID, "", err)
	p.publish(jobID, string(job.StatusFailed), 0, err.Error())
	log.Error("render failed", "err", err)
}

func (p *Pool) publish(jobID, status string, frac float64, msg string) {
	if p.Events == nil {
		return
	}
	p.Events.Publish(events.Event{JobID: jobID, Status: status, Progress: frac, Message: msg})
}
