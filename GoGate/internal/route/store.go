package route

import (
	"iter"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/levelcodingdev/gogate/internal/uid"
)

// Store is the routing-table contract used by the server.
type Store interface {
	Add(r Route) (Route, error)
	Get(id string) (Route, error)
	List() []Route
	Delete(id string) error
	// Match returns the route for an incoming request: the longest PathPrefix
	// that the path starts under, preferring a Host-specific route over a
	// wildcard one.
	Match(host, path string) (Route, bool)
}

// MemStore is an in-memory Store, safe for concurrent use.
type MemStore struct {
	mu   sync.RWMutex
	seq  int64
	byID map[string]*Route
}

// NewMemStore returns an empty store.
func NewMemStore() *MemStore { return &MemStore{byID: map[string]*Route{}} }

// Add validates r, assigns it an id, and stores it.
func (s *MemStore) Add(r Route) (Route, error) {
	if r.PathPrefix == "" {
		r.PathPrefix = "/"
	}
	if err := r.Validate(); err != nil {
		return Route{}, err
	}
	r.Host = strings.ToLower(strings.TrimSpace(r.Host))
	r.Target.Upstream = strings.TrimSpace(r.Target.Upstream)
	r.Target.Subject = strings.TrimSpace(r.Target.Subject)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	if r.ID == "" {
		r.ID = uid.New()[:12]
	}
	r.seq = s.seq
	cp := r
	s.byID[r.ID] = &cp
	return r, nil
}

// Get returns one route by id.
func (s *MemStore) Get(id string) (Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.byID[id]
	if !ok {
		return Route{}, ErrNotFound
	}
	return *r, nil
}

// List returns every route, most-specific first (longest prefix, then newest).
func (s *MemStore) List() []Route {
	return slices.Collect(s.All())
}

// All iterates every route, most-specific first — the streaming form of List
// (CoverGo U8). Iteration holds the read lock, so don't mutate the store or
// block inside the loop.
func (s *MemStore) All() iter.Seq[Route] {
	return func(yield func(Route) bool) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		out := make([]Route, 0, len(s.byID))
		for _, r := range s.byID {
			out = append(out, *r)
		}
		sortRoutes(out)
		for _, r := range out {
			if !yield(r) {
				return
			}
		}
	}
}

// Delete removes a route.
func (s *MemStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return ErrNotFound
	}
	delete(s.byID, id)
	return nil
}

// Match resolves the route for (host, path).
func (s *MemStore) Match(host, path string) (Route, bool) {
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i] // strip :port
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var best *Route
	for _, r := range s.byID {
		if r.Host != "" && r.Host != host {
			continue
		}
		if !prefixMatch(path, r.PathPrefix) {
			continue
		}
		if best == nil || better(r, best) {
			best = r
		}
	}
	if best == nil {
		return Route{}, false
	}
	return *best, true
}

// prefixMatch is a path-segment-aware prefix test: "/api" matches "/api" and
// "/api/x" but not "/apixyz". "/" matches everything.
func prefixMatch(path, prefix string) bool {
	if prefix == "/" {
		return true
	}
	prefix = strings.TrimRight(prefix, "/")
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := path[len(prefix):]
	return rest == "" || rest[0] == '/'
}

// better reports whether a should win over b: a Host-specific route beats a
// wildcard, then a longer prefix, then a newer route.
func better(a, b *Route) bool {
	if (a.Host != "") != (b.Host != "") {
		return a.Host != ""
	}
	if la, lb := len(a.PathPrefix), len(b.PathPrefix); la != lb {
		return la > lb
	}
	return a.seq > b.seq
}

func sortRoutes(rs []Route) {
	sort.Slice(rs, func(i, j int) bool { return better(&rs[i], &rs[j]) })
}
