package plan

import (
	"context"
	"testing"
)

// CoverGo U1 — the Go-side cost of turning a render spec into one ffmpeg
// filtergraph (ffmpeg itself is never invoked here). The queue-throughput
// benchmark lives in internal/queue.

func BenchmarkBuildSlideshow(b *testing.B) {
	for _, n := range []int{3, 12, 40} {
		b.Run(name(n), func(b *testing.B) {
			s := mkSlideshow(n, 0.5, "bed.m4a")
			ctx := context.Background()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Build(ctx, s, nil, "out.mp4"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func name(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n)) + "img"
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10)) + "img"
}
