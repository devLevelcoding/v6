package media

import (
	"testing"
	"time"
)

func TestParseSeconds(t *testing.T) {
	cases := map[string]time.Duration{
		"3.5":     3500 * time.Millisecond,
		"10":      10 * time.Second,
		"0":       0,
		"N/A":     0,
		"":        0,
		"  2.0  ": 2 * time.Second,
		"garbage": 0,
	}
	for in, want := range cases {
		if got := parseSeconds(in); got != want {
			t.Errorf("parseSeconds(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestClamp(t *testing.T) {
	for in, want := range map[float64]float64{-1: 0, 0: 0, 0.5: 0.5, 1: 1, 2: 1} {
		if got := clamp(in); got != want {
			t.Errorf("clamp(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestLastLines(t *testing.T) {
	in := "a\nb\nc\nd\ne\n"
	if got := lastLines(in, 2); got != "d\ne" {
		t.Fatalf("lastLines = %q", got)
	}
	if got := lastLines("only", 5); got != "only" {
		t.Fatalf("lastLines short = %q", got)
	}
}

func TestLocateTrustsOverrides(t *testing.T) {
	// An explicit path is taken as-is; whether it runs is ffmpeg's problem.
	tools, err := Locate("/opt/custom/ffmpeg", "/opt/custom/ffprobe")
	if err != nil {
		t.Fatalf("Locate with both overrides should not error: %v", err)
	}
	if tools.FFmpeg != "/opt/custom/ffmpeg" || tools.FFprobe != "/opt/custom/ffprobe" {
		t.Fatalf("overrides not honoured: %+v", tools)
	}
}

func TestLocateFromPath(t *testing.T) {
	tools, err := Locate("", "")
	if err != nil {
		t.Skipf("ffmpeg/ffprobe not installed: %v", err)
	}
	if tools.FFmpeg == "" || tools.FFprobe == "" {
		t.Fatalf("Locate returned empty paths: %+v", tools)
	}
}
