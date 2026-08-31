// Package monitor is GoUptime's registry of things to watch: HTTP endpoints and
// TCP ports, each with its own probe interval and pass/fail criteria. The
// in-memory Store here is a stand-in for the Postgres-backed store planned in
// future.md (Phase 1); it mirrors the Store interface exactly so the swap is
// mechanical.
package monitor

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// MinInterval is the smallest probe interval a monitor may declare. It is a
// var, not a const, so tests can drive the scheduler faster than production
// would ever run.
var MinInterval = 5 * time.Second

var (
	// ErrExists is returned when creating a monitor whose ID already exists.
	ErrExists = errors.New("monitor: already exists")
	// ErrNotFound is returned when a referenced monitor is missing.
	ErrNotFound = errors.New("monitor: not found")
	// ErrInvalid is returned when a monitor fails validation.
	ErrInvalid = errors.New("monitor: invalid")
)

// Type is the probe kind.
type Type string

const (
	// TypeHTTP issues an HTTP(S) request and checks the status code.
	TypeHTTP Type = "http"
	// TypeTCP opens a TCP connection to host:port.
	TypeTCP Type = "tcp"
)

// Monitor is one watched target.
type Monitor struct {
	ID string `json:"id"`
	// Name is a human label.
	Name string `json:"name"`
	// Type selects the probe (http, tcp).
	Type Type `json:"type"`
	// Target is a URL for http monitors, host:port for tcp monitors.
	Target string `json:"target"`
	// Interval is how often to probe. Minimum 5s in Phase 0.
	Interval time.Duration `json:"interval"`
	// Timeout bounds a single probe. Defaults to min(Interval, 10s).
	Timeout time.Duration `json:"timeout"`
	// ExpectStatus is the accepted HTTP status range for http monitors.
	// Zero value means "any 2xx or 3xx".
	ExpectStatus [2]int `json:"expect_status"`
	// Enabled gates scheduling without deleting the monitor.
	Enabled bool `json:"enabled"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AcceptsStatus reports whether an HTTP status code passes this monitor.
func (m Monitor) AcceptsStatus(code int) bool {
	lo, hi := m.ExpectStatus[0], m.ExpectStatus[1]
	if lo == 0 && hi == 0 {
		return code >= 200 && code < 400
	}
	return code >= lo && code <= hi
}

// Validate checks the monitor's fields and normalizes derived defaults.
func (m *Monitor) Validate() error {
	m.Name = strings.TrimSpace(m.Name)
	m.Target = strings.TrimSpace(m.Target)
	if m.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	switch m.Type {
	case TypeHTTP:
		if !strings.HasPrefix(m.Target, "http://") && !strings.HasPrefix(m.Target, "https://") {
			return fmt.Errorf("%w: http target must be an http(s) URL", ErrInvalid)
		}
	case TypeTCP:
		if !strings.Contains(m.Target, ":") {
			return fmt.Errorf("%w: tcp target must be host:port", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown type %q", ErrInvalid, m.Type)
	}
	if m.Interval == 0 {
		m.Interval = 60 * time.Second
	}
	if m.Interval < MinInterval {
		return fmt.Errorf("%w: interval must be >= %s", ErrInvalid, MinInterval)
	}
	if m.Timeout <= 0 {
		m.Timeout = min(m.Interval, 10*time.Second)
	}
	if m.Timeout > m.Interval {
		m.Timeout = m.Interval
	}
	if m.ExpectStatus[0] != 0 || m.ExpectStatus[1] != 0 {
		if m.ExpectStatus[0] < 100 || m.ExpectStatus[1] > 599 || m.ExpectStatus[0] > m.ExpectStatus[1] {
			return fmt.Errorf("%w: expect_status range out of bounds", ErrInvalid)
		}
	}
	return nil
}

// The Store interface and its in-memory implementation live in store.go.
