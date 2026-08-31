package plan

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/levelcodingdev/gorender/internal/media"
	"github.com/levelcodingdev/gorender/internal/spec"
)

func mkSlideshow(n int, xf float64, audio string) spec.Spec {
	imgs := make([]string, n)
	for i := range imgs {
		imgs[i] = "img" + string(rune('a'+i)) + ".jpg"
	}
	x := xf
	s := spec.Spec{
		Template: spec.TemplateSlideshow,
		Slideshow: &spec.Slideshow{
			Images: imgs, Audio: audio, SecondsPerImage: 4, CrossfadeSeconds: &x,
		},
	}
	s.Normalize()
	if err := s.Validate(); err != nil {
		panic(err)
	}
	return s
}

func joined(args []string) string { return strings.Join(args, " ") }

func TestSlideshowCrossfadeChain(t *testing.T) {
	p, err := Build(context.Background(), mkSlideshow(3, 0.5, ""), nil, "out.mp4")
	if err != nil {
		t.Fatal(err)
	}
	g := joined(p.Args)
	// two xfades for three images, offsets k*(d-xf) = 3.5 and 7.0
	if !strings.Contains(g, "xfade=transition=fade:duration=0.5:offset=3.5") {
		t.Fatalf("missing first xfade offset 3.5 in:\n%s", g)
	}
	if !strings.Contains(g, "offset=7[vx2]") {
		t.Fatalf("missing second xfade offset 7 in:\n%s", g)
	}
	if !strings.Contains(g, "-map [vx2]") {
		t.Fatalf("last stream not mapped:\n%s", g)
	}
	// total = 3*4 - 2*0.5 = 11s
	if want := 11 * time.Second; p.Duration != want {
		t.Fatalf("Duration = %v, want %v", p.Duration, want)
	}
}

func TestSlideshowHardCuts(t *testing.T) {
	p, err := Build(context.Background(), mkSlideshow(3, 0, ""), nil, "out.mp4")
	if err != nil {
		t.Fatal(err)
	}
	g := joined(p.Args)
	if strings.Contains(g, "xfade") {
		t.Fatalf("crossfade 0 should not emit xfade:\n%s", g)
	}
	if !strings.Contains(g, "concat=n=3:v=1:a=0[vout]") {
		t.Fatalf("expected 3-way concat:\n%s", g)
	}
	if want := 12 * time.Second; p.Duration != want {
		t.Fatalf("Duration = %v, want %v", p.Duration, want)
	}
}

func TestSlideshowSingleImage(t *testing.T) {
	p, err := Build(context.Background(), mkSlideshow(1, 0, ""), nil, "out.mp4")
	if err != nil {
		t.Fatal(err)
	}
	g := joined(p.Args)
	if strings.Contains(g, "xfade") || strings.Contains(g, "concat") {
		t.Fatalf("single image needs neither xfade nor concat:\n%s", g)
	}
	if !strings.Contains(g, "-map [v0]") {
		t.Fatalf("v0 should be mapped:\n%s", g)
	}
}

func TestSlideshowWithAudio(t *testing.T) {
	p, err := Build(context.Background(), mkSlideshow(2, 0.5, "bed.mp3"), nil, "out.mp4")
	if err != nil {
		t.Fatal(err)
	}
	g := joined(p.Args)
	if !strings.Contains(g, "-i bed.mp3") {
		t.Fatalf("audio input missing:\n%s", g)
	}
	if !strings.Contains(g, "-map 2:a") || !strings.Contains(g, "-shortest") {
		t.Fatalf("audio not mapped / not -shortest:\n%s", g)
	}
}

type fakeProber map[string]media.Info

func (f fakeProber) Probe(_ context.Context, path string) (media.Info, error) {
	return f[path], nil
}

// errProber errors on one named clip and, for any other clip, blocks until the
// context is cancelled — so the test fails unless errgroup fail-fast cancels it.
type errProber struct {
	bad string
	err error
}

func (e errProber) Probe(ctx context.Context, path string) (media.Info, error) {
	if path == e.bad {
		return media.Info{}, e.err
	}
	<-ctx.Done()
	return media.Info{}, ctx.Err()
}

func TestConcatProbeFailFast(t *testing.T) {
	want := context.DeadlineExceeded // any sentinel
	s := spec.Spec{Template: spec.TemplateConcat, Concat: &spec.Concat{Clips: []string{"ok.mp4", "bad.mp4", "ok2.mp4"}}}
	s.Normalize()
	done := make(chan error, 1)
	go func() {
		_, err := Build(context.Background(), s, errProber{bad: "bad.mp4", err: want}, "out.mp4")
		done <- err
	}()
	select {
	case err := <-done:
		if err != want {
			t.Fatalf("Build err = %v, want %v", err, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Build did not fail-fast — the bad clip's error should cancel the sibling probes")
	}
}

func TestConcatWithAudio(t *testing.T) {
	pr := fakeProber{
		"a.mp4": {Path: "a.mp4", HasVideo: true, HasAudio: true, Duration: 3 * time.Second},
		"b.mp4": {Path: "b.mp4", HasVideo: true, HasAudio: true, Duration: 5 * time.Second},
	}
	s := spec.Spec{Template: spec.TemplateConcat, Concat: &spec.Concat{Clips: []string{"a.mp4", "b.mp4"}}}
	s.Normalize()
	p, err := Build(context.Background(), s, pr, "out.mp4")
	if err != nil {
		t.Fatal(err)
	}
	g := joined(p.Args)
	if !strings.Contains(g, "concat=n=2:v=1:a=1[vout][aout]") {
		t.Fatalf("expected a/v concat:\n%s", g)
	}
	if p.Duration != 8*time.Second {
		t.Fatalf("Duration = %v, want 8s", p.Duration)
	}
}

func TestConcatVideoOnlyWhenAClipHasNoAudio(t *testing.T) {
	pr := fakeProber{
		"a.mp4": {Path: "a.mp4", HasVideo: true, HasAudio: true, Duration: 3 * time.Second},
		"b.mp4": {Path: "b.mp4", HasVideo: true, HasAudio: false, Duration: 2 * time.Second},
	}
	s := spec.Spec{Template: spec.TemplateConcat, Concat: &spec.Concat{Clips: []string{"a.mp4", "b.mp4"}}}
	s.Normalize()
	p, err := Build(context.Background(), s, pr, "out.mp4")
	if err != nil {
		t.Fatal(err)
	}
	g := joined(p.Args)
	if !strings.Contains(g, "concat=n=2:v=1:a=0[vout]") {
		t.Fatalf("expected video-only concat:\n%s", g)
	}
	if strings.Contains(g, "[aout]") {
		t.Fatalf("should not map audio:\n%s", g)
	}
}

func TestConcatRejectsClipWithoutVideo(t *testing.T) {
	pr := fakeProber{
		"a.mp4":    {Path: "a.mp4", HasVideo: true, HasAudio: true},
		"song.mp3": {Path: "song.mp3", HasVideo: false, HasAudio: true},
	}
	s := spec.Spec{Template: spec.TemplateConcat, Concat: &spec.Concat{Clips: []string{"a.mp4", "song.mp3"}}}
	s.Normalize()
	if _, err := Build(context.Background(), s, pr, "out.mp4"); err == nil {
		t.Fatal("expected error for audio-only clip in concat")
	}
}

func TestFF(t *testing.T) {
	for in, want := range map[float64]string{
		3.5: "3.5", 7.0: "7", 0.5: "0.5", 0: "0", 4.25: "4.25", 12.000: "12",
	} {
		if got := ff(in); got != want {
			t.Errorf("ff(%v) = %q, want %q", in, got, want)
		}
	}
}
