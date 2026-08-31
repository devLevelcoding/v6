package project_test

import (
	"errors"
	"testing"

	"github.com/levelcodingdev/goflare/internal/pgtest"
	"github.com/levelcodingdev/goflare/internal/project"
)

func TestPGStore(t *testing.T) {
	db := pgtest.DB(t)
	s, err := project.NewPGStore(db)
	if err != nil {
		t.Fatal(err)
	}

	// Create + Get + BySlug
	p, err := s.Create("Payments API", "python")
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "payments-api" || len(p.Keys) != 1 || p.Platform != "python" {
		t.Fatalf("Create returned %+v", p)
	}
	got, err := s.Get(p.ID)
	if err != nil || got.ID != p.ID || len(got.Keys) != 1 {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	bySlug, err := s.BySlug("payments-api")
	if err != nil || bySlug.ID != p.ID {
		t.Fatalf("BySlug = %+v, %v", bySlug, err)
	}

	// duplicate slug
	if _, err := s.Create("payments api", ""); !errors.Is(err, project.ErrExists) {
		t.Fatalf("duplicate Create err = %v, want ErrExists", err)
	}

	// Authenticate
	if _, err := s.Authenticate(p.ID, p.Keys[0].PublicKey); err != nil {
		t.Fatalf("Authenticate(valid) = %v", err)
	}
	if _, err := s.Authenticate(p.ID, "wrong-key"); !errors.Is(err, project.ErrAuth) {
		t.Fatalf("Authenticate(bad key) = %v, want ErrAuth", err)
	}
	if _, err := s.Authenticate("no-such-project", "x"); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("Authenticate(bad project) = %v, want ErrNotFound", err)
	}

	// Seed is idempotent by slug and honours the chosen id + key
	seeded, err := s.Seed("Web Frontend", "javascript", "fixedid00000000a", "fixedkey")
	if err != nil {
		t.Fatal(err)
	}
	if seeded.ID != "fixedid00000000a" || seeded.Keys[0].PublicKey != "fixedkey" {
		t.Fatalf("Seed = %+v", seeded)
	}
	again, err := s.Seed("web frontend", "javascript", "", "")
	if err != nil || again.ID != seeded.ID {
		t.Fatalf("Seed not idempotent: %+v, %v", again, err)
	}

	// List is newest-first
	list := s.List()
	if len(list) != 2 || list[0].ID != seeded.ID {
		t.Fatalf("List = %+v", list)
	}

	// DSN renders
	if dsn := got.DSN("https://flare.example.com"); dsn == "" {
		t.Fatal("DSN rendered empty")
	}
}

// The PGStore must be usable everywhere the interface is expected.
var _ project.Store = (*project.PGStore)(nil)
