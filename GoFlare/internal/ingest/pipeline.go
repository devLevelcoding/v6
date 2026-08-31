package ingest

import (
	"context"
	"errors"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/group"
)

// ErrBusy is returned by Submit when the queue is full. The handler turns it
// into a 429 with a Retry-After header — the SDK backs off, nothing is dropped
// silently.
var ErrBusy = errors.New("ingest: pipeline is at capacity")

// Pipeline decouples accepting an event from grouping and persisting it: the
// handler validates + enqueues + returns 202, a pool of workers drains the
// queue into group.Store. A burst of traffic can't block the SDK, and a slow
// database (Postgres, blob store) only grows the queue until backpressure kicks
// in — future.md Phase 1, "a burst never blocks the SDK".
//
// Structured-concurrency contract (CoverGo U19):
//   - one bounded stage: Submit → p.ch (cap queueSize) → N drain workers;
//   - Submit never blocks — a full queue returns ErrBusy (the handler → 429);
//   - every worker selects on ctx.Done() each iteration;
//   - shutdown order: cancel the Start ctx → workers flush whatever is already
//     in p.ch (no accepted event is lost) → return → Wait() unblocks;
//   - no goroutine outlives Wait() (TestPipelineNoGoroutineLeak).
type Pipeline struct {
	groups  *group.Store
	log     *slog.Logger
	ch      chan task
	workers int
	grp     *errgroup.Group
}

type task struct {
	projectID string
	slug      string
	ev        event.Event
}

// NewPipeline builds a pipeline with `workers` drain goroutines and a queue
// that buffers `queueSize` events before Submit returns ErrBusy.
func NewPipeline(groups *group.Store, workers, queueSize int, log *slog.Logger) *Pipeline {
	if workers < 1 {
		workers = 1
	}
	if queueSize < 1 {
		queueSize = 1
	}
	if log == nil {
		log = slog.Default()
	}
	return &Pipeline{
		groups:  groups,
		log:     log,
		ch:      make(chan task, queueSize),
		workers: workers,
	}
}

// Start launches the workers under an errgroup bound to ctx: they stop when ctx
// is cancelled, and if any worker returns a non-nil error the group's context
// is cancelled so the others wind down too. Wait blocks for their exit and
// drains whatever is already queued.
func (p *Pipeline) Start(ctx context.Context) {
	grp, gctx := errgroup.WithContext(ctx)
	p.grp = grp
	for i := 0; i < p.workers; i++ {
		worker := i
		grp.Go(func() error { return p.drain(gctx, worker) })
	}
	p.log.Info("ingest pipeline started", "workers", p.workers, "queue", cap(p.ch))
}

// Wait blocks until every worker has returned and reports the first worker
// error, if any (a clean ctx-cancelled shutdown returns nil).
func (p *Pipeline) Wait() error {
	if p.grp == nil {
		return nil
	}
	return p.grp.Wait()
}

// Depth is the number of events waiting to be grouped.
func (p *Pipeline) Depth() int { return len(p.ch) }

// Submit enqueues an event without blocking. ErrBusy means the queue is full.
func (p *Pipeline) Submit(projectID, slug string, ev event.Event) error {
	select {
	case p.ch <- task{projectID: projectID, slug: slug, ev: ev}:
		return nil
	default:
		return ErrBusy
	}
}

func (p *Pipeline) drain(ctx context.Context, worker int) error {
	for {
		select {
		case <-ctx.Done():
			// Best-effort flush of what's already queued so a graceful
			// shutdown doesn't lose accepted events.
			for {
				select {
				case t := <-p.ch:
					p.handle(t)
				default:
					return nil
				}
			}
		case t := <-p.ch:
			p.handle(t)
		}
	}
}

func (p *Pipeline) handle(t task) {
	iss, outcome := p.groups.Ingest(t.projectID, t.ev)
	if iss.ID == "" {
		// group.Ingest swallows durable-store errors and returns a blank issue;
		// that's the signal the event did not persist.
		p.log.Error("ingest: event not persisted",
			"project", t.slug, "event_id", t.ev.EventID, "title", t.ev.Title())
		return
	}
	p.log.Info("event ingested",
		"project", t.slug, "issue", iss.ID, "outcome", outcome,
		"title", iss.Title, "times_seen", iss.TimesSeen)
}
