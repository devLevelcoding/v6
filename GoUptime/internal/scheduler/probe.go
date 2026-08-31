package scheduler

import (
	"context"
	"time"

	"github.com/levelcodingdev/gouptime/internal/check"
	"github.com/levelcodingdev/gouptime/internal/monitor"
	"github.com/levelcodingdev/gouptime/internal/notify"
)

// RunNow probes a monitor once, synchronously, applying the result to history,
// the detector and notifications just as a scheduled probe would.
func (s *Scheduler) RunNow(ctx context.Context, id string) (check.Result, error) {
	m, err := s.store.Get(id)
	if err != nil {
		return check.Result{}, err
	}
	res := s.prober.Probe(ctx, m)
	s.apply(m, res)
	return res, nil
}

func (s *Scheduler) loop(ctx context.Context, id string, interval time.Duration) {
	defer s.wg.Done()

	probe := func() {
		m, err := s.store.Get(id)
		if err != nil {
			return // deleted between tick and fetch; Sync will clean up
		}
		res := s.prober.Probe(ctx, m)
		s.apply(m, res)
	}

	probe() // probe immediately so a monitor's first data point is not one interval away
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			probe()
		}
	}
}

// apply records a result and dispatches any incident event.
func (s *Scheduler) apply(m monitor.Monitor, res check.Result) {
	if s.recorder != nil {
		s.recorder.Record(res)
	}
	ev, changed := s.detector.Observe(res)
	if !changed || s.notifier == nil {
		return
	}
	msg := notify.Message{
		Event:     ev.Type,
		Incident:  ev.Incident,
		Monitor:   m,
		Timestamp: time.Now(),
	}
	go func() {
		nctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := s.notifier.Notify(nctx, msg); err != nil {
			s.log.Error("notify failed", "monitor", m.Name, "event", ev.Type, "err", err)
		}
	}()
}
