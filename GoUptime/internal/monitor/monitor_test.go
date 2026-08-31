package monitor

import (
	"errors"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      Monitor
		wantErr bool
	}{
		{"ok http", Monitor{Name: "api", Type: TypeHTTP, Target: "https://example.com", Interval: 30 * time.Second}, false},
		{"ok tcp", Monitor{Name: "db", Type: TypeTCP, Target: "db.internal:5432", Interval: time.Minute}, false},
		{"no name", Monitor{Type: TypeHTTP, Target: "https://example.com", Interval: time.Minute}, true},
		{"http target not url", Monitor{Name: "x", Type: TypeHTTP, Target: "example.com", Interval: time.Minute}, true},
		{"tcp target no port", Monitor{Name: "x", Type: TypeTCP, Target: "db.internal", Interval: time.Minute}, true},
		{"unknown type", Monitor{Name: "x", Type: "ping", Target: "x", Interval: time.Minute}, true},
		{"interval too small", Monitor{Name: "x", Type: TypeHTTP, Target: "https://x.io", Interval: time.Second}, true},
		{"bad status range", Monitor{Name: "x", Type: TypeHTTP, Target: "https://x.io", Interval: time.Minute, ExpectStatus: [2]int{500, 200}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateDefaults(t *testing.T) {
	m := Monitor{Name: "api", Type: TypeHTTP, Target: "https://example.com"}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if m.Interval != 60*time.Second {
		t.Errorf("default interval = %v, want 60s", m.Interval)
	}
	if m.Timeout != 10*time.Second {
		t.Errorf("default timeout = %v, want 10s", m.Timeout)
	}
}

func TestAcceptsStatus(t *testing.T) {
	any2xx3xx := Monitor{}
	if !any2xx3xx.AcceptsStatus(200) || !any2xx3xx.AcceptsStatus(301) || any2xx3xx.AcceptsStatus(500) {
		t.Error("default status acceptance wrong")
	}
	strict := Monitor{ExpectStatus: [2]int{200, 204}}
	if !strict.AcceptsStatus(200) || strict.AcceptsStatus(301) {
		t.Error("explicit status range wrong")
	}
}

func TestMemStoreCRUD(t *testing.T) {
	s := NewMemStore()

	m, err := s.Create(Monitor{Name: "api", Type: TypeHTTP, Target: "https://example.com", Interval: time.Minute, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == "" || m.CreatedAt.IsZero() {
		t.Fatalf("Create did not populate ID/CreatedAt: %+v", m)
	}

	got, err := s.Get(m.ID)
	if err != nil || got.Name != "api" {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	m.Name = "api-v2"
	upd, err := s.Update(m)
	if err != nil {
		t.Fatal(err)
	}
	if upd.Name != "api-v2" || !upd.UpdatedAt.After(upd.CreatedAt) && upd.UpdatedAt != upd.CreatedAt {
		t.Fatalf("Update = %+v", upd)
	}
	if upd.CreatedAt != got.CreatedAt {
		t.Errorf("Update changed CreatedAt")
	}

	if _, err := s.Update(Monitor{ID: "missing", Name: "x", Type: TypeHTTP, Target: "https://x.io", Interval: time.Minute}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update(missing) err = %v, want ErrNotFound", err)
	}

	if err := s.Delete(m.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(m.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(twice) err = %v, want ErrNotFound", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("List not empty after delete")
	}
}

func TestMemStoreListSorted(t *testing.T) {
	s := NewMemStore()
	for _, n := range []string{"zeta", "alpha", "mid"} {
		if _, err := s.Create(Monitor{Name: n, Type: TypeHTTP, Target: "https://x.io", Interval: time.Minute}); err != nil {
			t.Fatal(err)
		}
	}
	got := s.List()
	if got[0].Name != "alpha" || got[1].Name != "mid" || got[2].Name != "zeta" {
		t.Errorf("List not sorted by name: %v", []string{got[0].Name, got[1].Name, got[2].Name})
	}
}
