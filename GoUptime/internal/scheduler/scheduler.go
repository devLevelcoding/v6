// Package scheduler drives the probe loop: one goroutine per enabled monitor,
// each firing on the monitor's interval. Every result is recorded, fed to the
// incident detector, and — when the detector reports a state change —
// dispatched to the notifier.
//
// This file owns the lifecycle (New / Start / Sync / Wait); the per-monitor
// probe loop and result handling are in probe.go.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/levelcodingdev/gouptime/internal/check"
	"github.com/levelcodingdev/gouptime/internal/history"
	"github.com/levelcodingdev/gouptime/internal/incident"
	"github.com/levelcodingdev/gouptime/internal/monitor"
	"github.com/levelcodingdev/gouptime/internal/notify"
)

// Scheduler owns the running probe goroutines.
type Scheduler struct {
	store    monitor.Store
	prober   check.Prober
	detector *incident.Detector
	recorder history.Recorder
	notifier notify.Notifier
	log      *slog.Logger

	mu      sync.Mutex
	base    context.Context
	running map[string]*worker
	wg      sync.WaitGroup
}

type worker struct {
	cancel context.CancelFunc
	spec   string // interval|type|target|enabled — a relaunch trigger
}

// New wires a Scheduler. Any nil collaborator except store gets a default.
func New(store monitor.Store, p check.Prober, d *incident.Detector, rec history.Recorder, n notify.Notifier, log *slog.Logger) *Scheduler {
	if p == nil {
		p = check.NewProber()
	}
	if d == nil {
		d = incident.NewDetector(incident.DefaultPolicy())
	}
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		store: store, prober: p, detector: d, recorder: rec, notifier: n, log: log,
		running: map[string]*worker{},
	}
}

// Start records the base context and launches loops for current monitors.
// Call Sync after any monitor mutation.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.base = ctx
	s.mu.Unlock()
	s.Sync()
}

// Sync reconciles running loops with the store: launches new/changed enabled
// monitors, stops removed or disabled ones.
func (s *Scheduler) Sync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.base == nil {
		return
	}

	want := map[string]monitor.Monitor{}
	for _, m := range s.store.List() {
		if m.Enabled {
			want[m.ID] = m
		}
	}

	for id, w := range s.running {
		m, keep := want[id]
		if !keep || specOf(m) != w.spec {
			w.cancel()
			delete(s.running, id)
		}
	}
	for id, m := range want {
		if _, ok := s.running[id]; ok {
			continue
		}
		ctx, cancel := context.WithCancel(s.base)
		s.running[id] = &worker{cancel: cancel, spec: specOf(m)}
		s.wg.Add(1)
		go s.loop(ctx, id, m.Interval)
	}
}

// Wait blocks until every loop has exited (after the base context is done).
func (s *Scheduler) Wait() { s.wg.Wait() }

func specOf(m monitor.Monitor) string {
	return fmt.Sprintf("%s|%s|%s|%v", m.Interval, m.Type, m.Target, m.Enabled)
}
