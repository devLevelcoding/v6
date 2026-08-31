// Package history stores recent check.Results so the API can show a monitor's
// timeline and compute uptime. Phase 0 keeps a bounded per-monitor ring in
// memory; Phase 1 swaps this for a Postgres table with a retention window (see
// future.md).
package history

import (
	"sort"
	"sync"
	"time"

	"github.com/levelcodingdev/gouptime/internal/check"
)

// Recorder accepts results as the scheduler produces them.
type Recorder interface {
	Record(check.Result)
}

// Summary is a monitor's rolled-up state over the retained window.
type Summary struct {
	MonitorID   string        `json:"monitor_id"`
	Total       int           `json:"total"`
	Up          int           `json:"up"`
	UptimeRatio float64       `json:"uptime_ratio"`
	AvgLatency  time.Duration `json:"avg_latency"`
	Last        *check.Result `json:"last,omitempty"`
}

// Ring is an in-memory Recorder holding up to Cap results per monitor.
type Ring struct {
	mu  sync.RWMutex
	cap int
	m   map[string][]check.Result
}

// NewRing returns a store keeping the last perMonitor results for each monitor.
func NewRing(perMonitor int) *Ring {
	if perMonitor < 1 {
		perMonitor = 1
	}
	return &Ring{cap: perMonitor, m: map[string][]check.Result{}}
}

// Record appends a result, evicting the oldest if at capacity.
func (r *Ring) Record(res check.Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	buf := r.m[res.MonitorID]
	if len(buf) >= r.cap {
		buf = buf[len(buf)-r.cap+1:]
	}
	r.m[res.MonitorID] = append(buf, res)
}

// Results returns a monitor's retained results, newest first, capped at limit
// (0 = all retained).
func (r *Ring) Results(monitorID string, limit int) []check.Result {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src := r.m[monitorID]
	out := make([]check.Result, len(src))
	copy(out, src)
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Summary rolls up a monitor's retained window.
func (r *Ring) Summary(monitorID string) Summary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	src := r.m[monitorID]
	s := Summary{MonitorID: monitorID, Total: len(src)}
	if len(src) == 0 {
		return s
	}
	var latSum time.Duration
	for _, res := range src {
		if res.OK {
			s.Up++
		}
		latSum += res.Latency
	}
	s.UptimeRatio = float64(s.Up) / float64(s.Total)
	s.AvgLatency = latSum / time.Duration(s.Total)
	last := src[len(src)-1]
	s.Last = &last
	return s
}
