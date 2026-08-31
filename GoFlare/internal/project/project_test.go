package project

import (
	"errors"
	"strings"
	"testing"
)

func TestCreateAndDSN(t *testing.T) {
	s := NewMemStore()
	p, err := s.Create("My App", "javascript")
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "my-app" {
		t.Errorf("slug = %q, want my-app", p.Slug)
	}
	if len(p.Keys) != 1 || p.Keys[0].PublicKey == "" {
		t.Fatalf("expected one generated key: %+v", p.Keys)
	}
	dsn := p.DSN("https://flare.example.com")
	if !strings.HasPrefix(dsn, "https://"+p.Keys[0].PublicKey+"@flare.example.com/") || !strings.HasSuffix(dsn, "/"+p.ID) {
		t.Errorf("dsn = %q", dsn)
	}
}

func TestCreateValidation(t *testing.T) {
	s := NewMemStore()
	if _, err := s.Create("   ", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("blank name err = %v, want ErrInvalid", err)
	}
	if _, err := s.Create("!!!", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("unsluggable name err = %v, want ErrInvalid", err)
	}
	if _, err := s.Create("Dup", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("dup", ""); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate slug err = %v, want ErrExists", err)
	}
}

func TestAuthenticate(t *testing.T) {
	s := NewMemStore()
	p, _ := s.Create("app", "")
	key := p.Keys[0].PublicKey

	if _, err := s.Authenticate(p.ID, key); err != nil {
		t.Errorf("valid auth failed: %v", err)
	}
	if _, err := s.Authenticate(p.ID, "wrong"); !errors.Is(err, ErrAuth) {
		t.Errorf("bad key err = %v, want ErrAuth", err)
	}
	if _, err := s.Authenticate("nope", key); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing project err = %v, want ErrNotFound", err)
	}
}

func TestBySlugAndList(t *testing.T) {
	s := NewMemStore()
	a, _ := s.Create("Alpha", "")
	b, _ := s.Create("Beta", "")

	got, err := s.BySlug("alpha")
	if err != nil || got.ID != a.ID {
		t.Fatalf("BySlug(alpha) = %+v, %v", got, err)
	}
	if _, err := s.BySlug("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("BySlug(missing) err = %v", err)
	}

	list := s.List()
	if len(list) != 2 || list[0].ID != b.ID {
		t.Errorf("List not newest-first: %+v", list)
	}
}
