package route

import "testing"

// TestAllSeqEarlyBreak (CoverGo U8): the iterator must honour a caller's break
// and not deadlock on the read lock it holds.
func TestAllSeqEarlyBreak(t *testing.T) {
	s := NewMemStore()
	for _, id := range []string{"a", "b", "c"} {
		if _, err := s.Add(Route{ID: id, PathPrefix: "/" + id, Target: Target{Upstream: "http://u"}}); err != nil {
			t.Fatal(err)
		}
	}
	seen := 0
	for range s.All() {
		seen++
		break
	}
	if seen != 1 {
		t.Fatalf("break after 1 saw %d routes", seen)
	}
	// The lock must be released — a following call must not hang.
	if got := len(s.List()); got != 3 {
		t.Fatalf("List after a broken iteration = %d, want 3", got)
	}
}

// FuzzMatch (CoverGo U23) throws adversarial (host, path) pairs at the routing
// table. Contract: Match never panics, and a hit's PathPrefix is always an
// actual prefix of the requested path.
func FuzzMatch(f *testing.F) {
	s := NewMemStore()
	for _, r := range []Route{
		{ID: "api", Host: "x.example", PathPrefix: "/api", Target: Target{Upstream: "http://u1"}},
		{ID: "apiv2", Host: "x.example", PathPrefix: "/api/v2", Target: Target{Upstream: "http://u2"}},
		{ID: "root", PathPrefix: "/", Target: Target{Upstream: "http://u3"}},
		{ID: "bridge", Host: "q.example", PathPrefix: "/events", Target: Target{Subject: "events"}},
	} {
		if _, err := s.Add(r); err != nil {
			f.Fatalf("seed route %s: %v", r.ID, err)
		}
	}

	f.Add("x.example", "/api/v2/things")
	f.Add("x.example", "/api")
	f.Add("", "/")
	f.Add("q.example", "/events/stream")
	f.Add("other", "/nope")

	f.Fuzz(func(t *testing.T, host, path string) {
		r, ok := s.Match(host, path)
		if !ok {
			return
		}
		if !prefixMatch(path, r.PathPrefix) {
			t.Fatalf("Match(%q,%q) returned route %q whose prefix %q is not a prefix of the path",
				host, path, r.ID, r.PathPrefix)
		}
	})
}
