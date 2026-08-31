package job

import (
	"errors"
	"testing"
	"time"

	"github.com/levelcodingdev/gorender/internal/spec"
)

func sampleSpec() spec.Spec {
	x := 0.5
	s := spec.Spec{Template: spec.TemplateSlideshow, Slideshow: &spec.Slideshow{
		Images: []string{"a.jpg", "b.jpg"}, SecondsPerImage: 4, CrossfadeSeconds: &x,
	}}
	s.Normalize()
	return s
}

func TestCreateGetList(t *testing.T) {
	s := NewStore()
	j1 := s.Create(sampleSpec())
	j2 := s.Create(sampleSpec())
	if j1.ID == j2.ID {
		t.Fatal("ids collided")
	}
	if j1.Status != StatusQueued {
		t.Fatalf("new job status = %q, want queued", j1.Status)
	}
	got, ok := s.Get(j1.ID)
	if !ok || got.ID != j1.ID {
		t.Fatalf("Get(%s) failed", j1.ID)
	}
	if _, ok := s.Get("nope"); ok {
		t.Fatal("Get of unknown id should be false")
	}
	list := s.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
}

func TestGetReturnsCopy(t *testing.T) {
	s := NewStore()
	j := s.Create(sampleSpec())
	got, _ := s.Get(j.ID)
	got.Status = StatusFailed
	again, _ := s.Get(j.ID)
	if again.Status == StatusFailed {
		t.Fatal("mutating a Get() copy leaked into the store")
	}
}

func TestMarkRunningThenDoneSuccess(t *testing.T) {
	s := NewStore()
	j := s.Create(sampleSpec())
	s.MarkRunning(j.ID)
	got, _ := s.Get(j.ID)
	if got.Status != StatusRunning || got.StartedAt == nil {
		t.Fatalf("after MarkRunning: %+v", got)
	}
	s.MarkDone(j.ID, "/out/x.mp4", nil)
	got, _ = s.Get(j.ID)
	if got.Status != StatusSucceeded || got.Artifact != "/out/x.mp4" || got.Progress != 1 || got.FinishedAt == nil {
		t.Fatalf("after MarkDone(ok): %+v", got)
	}
}

func TestMarkDoneFailure(t *testing.T) {
	s := NewStore()
	j := s.Create(sampleSpec())
	s.MarkRunning(j.ID)
	s.MarkDone(j.ID, "", errors.New("ffmpeg exploded"))
	got, _ := s.Get(j.ID)
	if got.Status != StatusFailed || got.Error != "ffmpeg exploded" || got.Artifact != "" {
		t.Fatalf("after MarkDone(err): %+v", got)
	}
}

func TestUpdateUnknownID(t *testing.T) {
	s := NewStore()
	if s.Update("ghost", func(*Job) {}) {
		t.Fatal("Update of unknown id should return false")
	}
}

func TestListNewestFirst(t *testing.T) {
	s := NewStore()
	base := time.Now()
	i := 0
	s.now = func() time.Time { i++; return base.Add(time.Duration(i) * time.Second) }
	a := s.Create(sampleSpec())
	b := s.Create(sampleSpec())
	list := s.List()
	if list[0].ID != b.ID || list[1].ID != a.ID {
		t.Fatalf("List not newest-first: %s then %s", list[0].ID, list[1].ID)
	}
}
