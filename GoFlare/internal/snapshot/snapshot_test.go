package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/levelcodingdev/goflare/internal/event"
	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/project"
)

func statMod(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}

func errEvent(typ, val string) event.Event {
	return event.Event{
		Level:     event.LevelError,
		Timestamp: time.Now(),
		Exceptions: []event.Exception{{
			Type:   typ,
			Value:  val,
			Frames: []event.Frame{{Module: "app." + typ, Function: "run", InApp: true}},
		}},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")

	projects := project.NewMemStore()
	groups := group.NewStore(10)
	p, err := projects.Create("Checkout API", "node")
	if err != nil {
		t.Fatal(err)
	}
	iss, _ := groups.Ingest(p.ID, errEvent("Boom", "1"))
	groups.Ingest(p.ID, errEvent("Boom", "2"))
	groups.Ingest(p.ID, errEvent("Other", "x"))
	if _, err := groups.SetStatus(iss.ID, group.StatusResolved); err != nil {
		t.Fatal(err)
	}

	s := New(path, nil)
	if err := s.Save(projects, groups); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Fresh stores, load into them.
	projects2 := project.NewMemStore()
	groups2 := group.NewStore(10)
	s2 := New(path, nil)
	if err := s2.Load(projects2, groups2); err != nil {
		t.Fatalf("load: %v", err)
	}

	gotP := projects2.List()
	if len(gotP) != 1 || gotP[0].ID != p.ID || gotP[0].Name != "Checkout API" {
		t.Fatalf("projects round-trip: %+v", gotP)
	}
	if gotP[0].DSN("http://x") == "" {
		t.Error("DSN key lost on round-trip")
	}

	gotI := groups2.List(group.Filter{ProjectID: p.ID})
	if len(gotI) != 2 {
		t.Fatalf("issues round-trip: %d", len(gotI))
	}
	restored, err := groups2.Get(iss.ID)
	if err != nil || restored.Status != group.StatusResolved || restored.TimesSeen != 2 {
		t.Fatalf("issue state lost: %+v %v", restored, err)
	}
	evs, err := groups2.Events(iss.ID, 0)
	if err != nil || len(evs) != 2 {
		t.Fatalf("events round-trip: %d %v", len(evs), err)
	}

	// A recurring event after restore must land on the same issue (byHash rebuilt),
	// and because it was resolved this is a regression.
	after, outcome := groups2.Ingest(p.ID, errEvent("Boom", "3"))
	if after.ID != iss.ID || outcome != group.OutcomeRegression {
		t.Fatalf("post-restore ingest: id=%s outcome=%s", after.ID, outcome)
	}
}

func TestLoadRecoversFromBakWhenPrimaryIsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	projects := project.NewMemStore()
	groups := group.NewStore(10)
	p, _ := projects.Create("Billing", "node")
	groups.Ingest(p.ID, errEvent("Boom", "1"))

	s := New(path, nil)
	if err := s.Save(projects, groups); err != nil { // first save: no .bak yet
		t.Fatal(err)
	}
	groups.Ingest(p.ID, errEvent("Boom", "2"))
	if err := s.Save(projects, groups); err != nil { // second save: rolls .bak
		t.Fatal(err)
	}
	// Corrupt the primary as a torn write would.
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	p2 := project.NewMemStore()
	g2 := group.NewStore(10)
	if err := New(path, nil).Load(p2, g2); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := p2.List(); len(got) != 1 || got[0].Name != "Billing" {
		t.Fatalf("did not recover from .bak: %+v", got)
	}
	if got := g2.List(group.Filter{}); len(got) != 1 {
		t.Fatalf("issues not recovered from .bak: %d", len(got))
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "nope.json"), nil)
	if err := s.Load(project.NewMemStore(), group.NewStore(10)); err != nil {
		t.Fatalf("missing file should be fine, got %v", err)
	}
}

func TestFlushOnlyWritesWhenDirty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	projects := project.NewMemStore()
	groups := group.NewStore(10)
	s := New(path, nil)
	groups.SetOnChange(s.Touch)

	if _, err := projects.Create("p", ""); err != nil {
		t.Fatal(err)
	}
	groups.Ingest("p", errEvent("Boom", "1")) // Touch -> dirty
	s.flushIfDirty(projects, groups)

	info1, err := statMod(path)
	if err != nil {
		t.Fatalf("expected a file after first flush: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	s.flushIfDirty(projects, groups) // nothing changed -> no write
	info2, _ := statMod(path)
	if !info1.Equal(info2) {
		t.Error("clean flush rewrote the file")
	}
}
