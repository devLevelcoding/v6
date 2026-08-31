package route

import (
	"errors"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	ok := Route{PathPrefix: "/api", Target: Target{Upstream: "http://svc:80"}}
	cases := []struct {
		name    string
		r       Route
		wantErr bool
	}{
		{"ok http", ok, false},
		{"ok bridge", Route{PathPrefix: "/", Target: Target{Subject: "crm3-crm"}}, false},
		{"no target", Route{PathPrefix: "/x"}, true},
		{"both targets", Route{PathPrefix: "/x", Target: Target{Upstream: "http://a", Subject: "b"}}, true},
		{"prefix no slash", Route{PathPrefix: "api", Target: Target{Subject: "b"}}, true},
		{"relative upstream", Route{PathPrefix: "/x", Target: Target{Upstream: "svc:80"}}, true},
		{"half rate", Route{PathPrefix: "/x", Target: Target{Subject: "b"}, Policy: Policy{RateLimit: Rate{PerSecond: 10}}}, true},
		{"neg ttl", Route{PathPrefix: "/x", Target: Target{Subject: "b"}, Policy: Policy{CacheTTL: -1}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.r.Validate(); (err != nil) != c.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestMemStoreCRUD(t *testing.T) {
	s := NewMemStore()
	r, err := s.Add(Route{PathPrefix: "/api", Target: Target{Upstream: "http://svc"}})
	if err != nil || r.ID == "" {
		t.Fatalf("Add = %+v, %v", r, err)
	}
	if got, err := s.Get(r.ID); err != nil || got.ID != r.ID {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := s.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(nope) = %v", err)
	}
	if err := s.Delete(r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double Delete = %v", err)
	}
}

func TestMatch(t *testing.T) {
	s := NewMemStore()
	mustAdd := func(r Route) Route {
		got, err := s.Add(r)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	root := mustAdd(Route{PathPrefix: "/", Target: Target{Upstream: "http://root"}})
	api := mustAdd(Route{PathPrefix: "/api", Target: Target{Upstream: "http://api"}})
	apiUsers := mustAdd(Route{PathPrefix: "/api/users", Target: Target{Upstream: "http://users"}})
	hostAdmin := mustAdd(Route{Host: "admin.example.com", PathPrefix: "/api", Target: Target{Upstream: "http://admin"}})

	check := func(host, path, wantID string) {
		t.Helper()
		got, ok := s.Match(host, path)
		if !ok || got.ID != wantID {
			t.Fatalf("Match(%q,%q) = %q (ok=%v), want %q", host, path, got.ID, ok, wantID)
		}
	}
	check("x.com", "/", root.ID)
	check("x.com", "/other", root.ID)
	check("x.com", "/api", api.ID)
	check("x.com", "/api/orders", api.ID)
	check("x.com", "/api/users", apiUsers.ID) // longest prefix wins
	check("x.com", "/api/users/42", apiUsers.ID)
	check("x.com", "/apixyz", root.ID)                      // segment boundary, not a substring
	check("admin.example.com:8090", "/api/x", hostAdmin.ID) // host-specific beats wildcard, :port stripped
	check("other.com", "/api/x", api.ID)
}

func TestCacheable(t *testing.T) {
	r := Route{Policy: Policy{CacheTTL: time.Minute}}
	if !r.Cacheable("GET") || !r.Cacheable("HEAD") {
		t.Fatal("GET/HEAD should be cacheable with a TTL")
	}
	if r.Cacheable("POST") {
		t.Fatal("POST is never cacheable")
	}
	if (Route{}).Cacheable("GET") {
		t.Fatal("no TTL → not cacheable")
	}
}
