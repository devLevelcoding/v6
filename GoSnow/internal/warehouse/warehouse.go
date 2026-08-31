// Package warehouse manages GoSnow "virtual warehouses" — named, resizable
// compute pools that execute queries, decoupled from stored data. The skeleton
// only tracks declared state; starting and scaling real worker processes is
// future.md (Phase 3).
package warehouse

import (
	"errors"
	"sort"
	"sync"
)

var (
	// ErrExists is returned by Create for a duplicate name.
	ErrExists = errors.New("warehouse: already exists")
	// ErrNotFound is returned when a named warehouse is missing.
	ErrNotFound = errors.New("warehouse: not found")
)

// Size is a t-shirt compute size (worker count / CPU).
type Size string

// Known sizes.
const (
	XSmall Size = "x-small"
	Small  Size = "small"
	Medium Size = "medium"
	Large  Size = "large"
)

// State is a warehouse's run state.
type State string

// Known states.
const (
	Suspended State = "suspended"
	Running   State = "running"
)

// Warehouse is one compute pool.
type Warehouse struct {
	Name  string `json:"name"`
	Size  Size   `json:"size"`
	State State  `json:"state"`
}

// Manager is a concurrency-safe registry of warehouses.
type Manager struct {
	mu  sync.Mutex
	whs map[string]*Warehouse
}

// NewManager returns an empty manager.
func NewManager() *Manager { return &Manager{whs: map[string]*Warehouse{}} }

// Create registers a new, suspended warehouse. An empty size defaults to XSmall.
func (m *Manager) Create(name string, size Size) (*Warehouse, error) {
	if name == "" {
		return nil, errors.New("warehouse: empty name")
	}
	if size == "" {
		size = XSmall
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.whs[name]; ok {
		return nil, ErrExists
	}
	w := &Warehouse{Name: name, Size: size, State: Suspended}
	m.whs[name] = w
	return w, nil
}

// SetState transitions a warehouse (Resume/Suspend at the API layer).
func (m *Manager) SetState(name string, s State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.whs[name]
	if !ok {
		return ErrNotFound
	}
	w.State = s
	return nil
}

// Get returns one warehouse.
func (m *Manager) Get(name string) (*Warehouse, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.whs[name]
	return w, ok
}

// List returns all warehouses ordered by name.
func (m *Manager) List() []*Warehouse {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Warehouse, 0, len(m.whs))
	for _, w := range m.whs {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
