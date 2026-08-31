package worker_test

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/levelcodingdev/gorender/internal/events"
	"github.com/levelcodingdev/gorender/internal/job"
	"github.com/levelcodingdev/gorender/internal/media"
	"github.com/levelcodingdev/gorender/internal/queue"
	"github.com/levelcodingdev/gorender/internal/spec"
	"github.com/levelcodingdev/gorender/internal/worker"
)

// TestEndToEndSlideshowRealFFmpeg runs the whole pipeline — plan → ffmpeg →
// artifact — against a real ffmpeg, on images generated here. Skipped when
// ffmpeg is not installed or -short is set.
func TestEndToEndSlideshowRealFFmpeg(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	tools, err := media.Locate("", "")
	if err != nil {
		t.Skipf("no ffmpeg: %v", err)
	}

	dir := t.TempDir()
	imgs := []string{
		writePNG(t, filepath.Join(dir, "1.png"), color.RGBA{200, 40, 40, 255}),
		writePNG(t, filepath.Join(dir, "2.png"), color.RGBA{40, 160, 60, 255}),
		writePNG(t, filepath.Join(dir, "3.png"), color.RGBA{40, 80, 200, 255}),
	}

	store := job.NewStore()
	q := queue.NewMem(4)
	br := events.NewBroker()
	pool := &worker.Pool{
		N: 1, Queue: q, Store: store,
		Encoder: media.FFmpegEncoder{Bin: tools.FFmpeg},
		Prober:  tools,
		Events:  br,
		OutDir:  dir,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)

	x := 0.5
	s := spec.Spec{
		Template: spec.TemplateSlideshow,
		Width:    320, Height: 240, FPS: 24,
		Slideshow: &spec.Slideshow{Images: imgs, SecondsPerImage: 2, CrossfadeSeconds: &x},
	}
	s.Normalize()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	j := store.Create(s)

	ch, unsub := br.Subscribe(j.ID)
	defer unsub()
	if err := q.Push(ctx, j.ID); err != nil {
		t.Fatal(err)
	}

	var final *job.Job
	deadline := time.After(60 * time.Second)
	for final == nil {
		select {
		case <-ch:
		case <-deadline:
			cur, _ := store.Get(j.ID)
			t.Fatalf("render did not finish in time: %+v", cur)
		case <-time.After(250 * time.Millisecond):
		}
		if cur, ok := store.Get(j.ID); ok && (cur.Status == job.StatusSucceeded || cur.Status == job.StatusFailed) {
			final = cur
		}
	}

	if final.Status != job.StatusSucceeded {
		t.Fatalf("render failed: %s", final.Error)
	}
	out := filepath.Join(dir, j.ID+".mp4")
	st, err := os.Stat(out)
	if err != nil || st.Size() == 0 {
		t.Fatalf("no artifact at %s (err=%v)", out, err)
	}

	// total = 3*2 - 2*0.5 = 5s; ffprobe should agree within a frame or two
	info, err := tools.Probe(ctx, out)
	if err != nil {
		t.Fatalf("probe output: %v", err)
	}
	if got := info.Duration.Seconds(); math.Abs(got-5.0) > 0.5 {
		t.Fatalf("output duration = %.2fs, want ~5s", got)
	}
	if !info.HasVideo {
		t.Fatal("output has no video stream")
	}
}

func writePNG(t *testing.T, path string, c color.Color) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}
