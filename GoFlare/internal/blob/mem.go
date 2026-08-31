package blob

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// MemStore keeps blobs in memory. Safe for concurrent use.
type MemStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore { return &MemStore{data: map[string][]byte{}} }

func (m *MemStore) Put(_ context.Context, key string, data []byte) error {
	cp := append([]byte(nil), data...)
	m.mu.Lock()
	m.data[key] = cp
	m.mu.Unlock()
	return nil
}

func (m *MemStore) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.data[key]
	if !ok {
		return nil, ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

func (m *MemStore) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *MemStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[key]; !ok {
		return ErrNotExist
	}
	delete(m.data, key)
	return nil
}
