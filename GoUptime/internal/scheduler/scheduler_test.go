package scheduler

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/levelcodingdev/gouptime/internal/check"
	"github.com/levelcodingdev/gouptime/internal/history"
	"github.com/levelcodingdev/gouptime/internal/incident"
	"github.com/levelcodingdev/gouptime/internal/monitor"
	"github.com/levelcodingdev/gouptime/internal/notify"
)

func TestMain(m *testing.M) {
	monitor.MinInterval = time.Millisecond
	os.Exit(m.Run())
}

type fakeProber struct {
	mu    sync.Mutex
	calls int
	ok    bool
}

func (f *fakeProber) setOK(v bool) { f.mu.Lock(); f.ok = v; f.mu.Unlock() }

func (f *fakeProber) Probe(_ context.Context, m monitor.Monitor) check.Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return check.Result{MonitorID: m.ID, At: time.Now(), OK: f.ok, Detail: "fake"}
}

type capturingNotifier struct {
	mu     sync.Mutex
	events []notify.Message
}

func (c *capturingNotifier) Notify(_ context.Context, m notify.Message) error {
	c.mu.Lock()
	c.events = append(c.events, m)
	c.mu.Unlock()
	return nil
}

func (c *capturingNotifier) count() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.events) }

func newMon(t *testing.T, store monitor.Store, interval time.Duration) monitor.Monitor {
	t.Helper()
	m, err := store.Create(monitor.Monitor{
		Name: "api", Type: monitor.TypeHTTP, Target: "https://example.com",
		Interval: interval, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRunNowAppliesResult(t *testing.T) {
	store := monitor.NewMemStore()
	prober := &fakeProber{ok: false}
	det := incident.NewDetector(incident.Policy{FailThreshold: 1, RecoverThreshold: 1})
	ring := history.NewRing(100)
	notifier := &capturingNotifier{}
	s := New(store, prober, det, ring, notifier, nil)

	m := newMon(t, store, time.Minute)

	res, err := s.RunNow(context.Background(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("expected failing result")
	}
	if got := ring.Summary(m.ID); got.Total != 1 {
		t.Errorf("result not recorded: %+v", got)
	}

	waitFor(t, func() bool { return notifier.count() == 1 }, "incident notification")
	if notifier.events[0].Event != incident.Opened {
		t.Errorf("event = %v, want opened", notifier.events[0].Event)
	}
}

func TestRunNowUnknownMonitor(t *testing.T) {
	s := New(monitor.NewMemStore(), &fakeProber{}, nil, nil, nil, nil)
	if _, err := s.RunNow(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for missing monitor")
	}
}

func TestSyncLaunchesAndStopsLoops(t *testing.T) {
	store := monitor.NewMemStore()
	prober := &fakeProber{ok: true}
	s := New(store, prober, nil, nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	m := newMon(t, store, 20*time.Millisecond)
	s.Sync()

	waitFor(t, func() bool {
		prober.mu.Lock()
		defer prober.mu.Unlock()
		return prober.calls >= 2
	}, "loop to probe at least twice")

	if err := store.Delete(m.ID); err != nil {
		t.Fatal(err)
	}
	s.Sync()

	prober.mu.Lock()
	callsAtStop := prober.calls
	prober.mu.Unlock()

	time.Sleep(80 * time.Millisecond)
	prober.mu.Lock()
	after := prober.calls
	prober.mu.Unlock()
	if after > callsAtStop+1 { // allow one in-flight probe to land
		t.Errorf("loop kept probing after delete+sync: %d -> %d", callsAtStop, after)
	}
}

func TestSyncRelaunchesOnDisable(t *testing.T) {
	store := monitor.NewMemStore()
	prober := &fakeProber{ok: true}
	s := New(store, prober, nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	m := newMon(t, store, 15*time.Millisecond)
	s.Sync()
	waitFor(t, func() bool { prober.mu.Lock(); defer prober.mu.Unlock(); return prober.calls >= 1 }, "first probe")

	m.Enabled = false
	if _, err := store.Update(m); err != nil {
		t.Fatal(err)
	}
	s.Sync()

	prober.mu.Lock()
	base := prober.calls
	prober.mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	prober.mu.Lock()
	got := prober.calls
	prober.mu.Unlock()
	if got > base+1 {
		t.Errorf("disabled monitor kept probing: %d -> %d", base, got)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
