package org_test

import (
	"errors"
	"testing"

	"github.com/levelcodingdev/goflare/internal/org"
	"github.com/levelcodingdev/goflare/internal/pgtest"
)

// run exercises any Store implementation.
func run(t *testing.T, s org.Store) {
	t.Helper()

	o, err := s.CreateOrg("Acme Inc")
	if err != nil {
		t.Fatal(err)
	}
	if o.Slug != "acme-inc" {
		t.Fatalf("org slug = %q", o.Slug)
	}
	if _, err := s.CreateOrg("acme inc"); !errors.Is(err, org.ErrExists) {
		t.Fatalf("dup org = %v, want ErrExists", err)
	}
	if got, err := s.GetOrg(o.ID); err != nil || got.ID != o.ID {
		t.Fatalf("GetOrg = %+v, %v", got, err)
	}
	if _, err := s.GetOrg("nope"); !errors.Is(err, org.ErrNotFound) {
		t.Fatalf("GetOrg(missing) = %v", err)
	}

	// team scoped to org; slug unique per-org, not globally
	tm, err := s.CreateTeam(o.ID, "Platform")
	if err != nil {
		t.Fatal(err)
	}
	if tm.OrgID != o.ID || tm.Slug != "platform" {
		t.Fatalf("team = %+v", tm)
	}
	if _, err := s.CreateTeam(o.ID, "platform"); !errors.Is(err, org.ErrExists) {
		t.Fatalf("dup team = %v", err)
	}
	if _, err := s.CreateTeam("no-such-org", "X"); !errors.Is(err, org.ErrNotFound) {
		t.Fatalf("team under missing org = %v", err)
	}

	o2, _ := s.CreateOrg("Beta LLC")
	if _, err := s.CreateTeam(o2.ID, "Platform"); err != nil {
		t.Fatalf("same team slug under a different org should be allowed: %v", err)
	}

	if teams := s.ListTeams(o.ID); len(teams) != 1 {
		t.Fatalf("ListTeams(acme) = %d, want 1", len(teams))
	}
	if all := s.ListTeams(""); len(all) != 2 {
		t.Fatalf("ListTeams(all) = %d, want 2", len(all))
	}
	if orgs := s.ListOrgs(); len(orgs) != 2 {
		t.Fatalf("ListOrgs = %d, want 2", len(orgs))
	}
}

func TestMemStore(t *testing.T) { run(t, org.NewMemStore()) }

func TestPGStore(t *testing.T) {
	db := pgtest.DB(t)
	s, err := org.NewPGStore(db)
	if err != nil {
		t.Fatal(err)
	}
	run(t, s)
}

var (
	_ org.Store = (*org.MemStore)(nil)
	_ org.Store = (*org.PGStore)(nil)
)
