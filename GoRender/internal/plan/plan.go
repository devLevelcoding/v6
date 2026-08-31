// Package plan compiles a spec.Spec into a media.Plan — the ffmpeg argument
// list and filtergraph that produces the output. This is the part that replaces
// moviepy: one ffmpeg filtergraph instead of a Python frame loop.
package plan

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/levelcodingdev/gorender/internal/media"
	"github.com/levelcodingdev/gorender/internal/spec"
)

// Build turns a normalized, validated Spec into a runnable Plan writing to
// outPath. pr may be nil for templates that do not probe their inputs
// (slideshow); concat requires it.
func Build(ctx context.Context, s spec.Spec, pr media.Prober, outPath string) (media.Plan, error) {
	switch s.Template {
	case spec.TemplateSlideshow:
		return buildSlideshow(s, outPath)
	case spec.TemplateConcat:
		if pr == nil {
			return media.Plan{}, fmt.Errorf("concat needs a prober")
		}
		return buildConcat(ctx, s, pr, outPath)
	default:
		return media.Plan{}, fmt.Errorf("plan: unknown template %q", s.Template)
	}
}

// scaleChain normalizes any input frame to the target canvas: fit inside,
// letterbox with black, square pixels, fixed fps, yuv420p.
func scaleChain(w, h, fps int) string {
	return fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,"+
			"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black,"+
			"setsar=1,fps=%d,format=yuv420p",
		w, h, w, h, fps,
	)
}

func buildSlideshow(s spec.Spec, outPath string) (media.Plan, error) {
	sh := s.Slideshow
	n := len(sh.Images)
	d := sh.SecondsPerImage
	xf := sh.Crossfade()

	var args []string
	// Each image is a looped still held for d seconds. With a crossfade the
	// transition eats xf from the boundary, so total = n*d - (n-1)*xf.
	for _, img := range sh.Images {
		args = append(args, "-loop", "1", "-t", ff(d), "-i", img)
	}
	audioIdx := -1
	if sh.Audio != "" {
		audioIdx = n
		args = append(args, "-i", sh.Audio)
	}

	var fc strings.Builder
	norm := scaleChain(s.Width, s.Height, s.FPS)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&fc, "[%d:v]%s[v%d];", i, norm, i)
	}

	last := "v0"
	if n == 1 {
		// no transitions — v0 is the whole video
	} else if xf <= 0 {
		// hard cuts: concat the normalized streams
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "[v%d]", i)
		}
		fmt.Fprintf(&fc, "%sconcat=n=%d:v=1:a=0[vout];", b.String(), n)
		last = "vout"
	} else {
		// crossfade chain: offset of the k-th xfade is k*(d-xf)
		prev := "v0"
		for k := 1; k < n; k++ {
			out := fmt.Sprintf("vx%d", k)
			offset := float64(k) * (d - xf)
			fmt.Fprintf(&fc, "[%s][v%d]xfade=transition=fade:duration=%s:offset=%s[%s];",
				prev, k, ff(xf), ff(offset), out)
			prev = out
		}
		last = prev
	}

	graph := strings.TrimSuffix(fc.String(), ";")
	args = append(args, "-filter_complex", graph, "-map", "["+last+"]")

	var dur time.Duration
	if n == 1 {
		dur = seconds(d)
	} else if xf <= 0 {
		dur = seconds(float64(n) * d)
	} else {
		dur = seconds(float64(n)*d - float64(n-1)*xf)
	}

	if audioIdx >= 0 {
		args = append(args,
			"-map", fmt.Sprintf("%d:a", audioIdx),
			"-c:a", "aac", "-b:a", "192k",
			"-shortest",
		)
	}
	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outPath,
	)

	return media.Plan{Args: args, Output: outPath, Duration: dur}, nil
}

func buildConcat(ctx context.Context, s spec.Spec, pr media.Prober, outPath string) (media.Plan, error) {
	clips := s.Concat.Clips

	// Probe every clip concurrently (each Probe shells out to ffprobe — IO-bound).
	// errgroup gives fail-fast: the first bad clip cancels the rest. SetLimit
	// keeps a 50-clip concat from spawning 50 ffprobe processes at once.
	infos := make([]media.Info, len(clips))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(max(2, runtime.NumCPU()))
	for i, c := range clips {
		i, c := i, c
		g.Go(func() error {
			info, err := pr.Probe(gctx, c)
			if err != nil {
				return err
			}
			if !info.HasVideo {
				return fmt.Errorf("concat: clip %q has no video stream", c)
			}
			infos[i] = info
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return media.Plan{}, err
	}

	allAudio := true
	var total time.Duration
	for _, info := range infos {
		if !info.HasAudio {
			allAudio = false
		}
		total += info.Duration
	}

	var args []string
	for _, c := range clips {
		args = append(args, "-i", c)
	}

	var fc strings.Builder
	norm := scaleChain(s.Width, s.Height, s.FPS)
	var maps strings.Builder
	for i := range clips {
		fmt.Fprintf(&fc, "[%d:v]%s[v%d];", i, norm, i)
		maps.WriteString(fmt.Sprintf("[v%d]", i))
		if allAudio {
			fmt.Fprintf(&fc, "[%d:a]aresample=async=1:first_pts=0[a%d];", i, i)
			maps.WriteString(fmt.Sprintf("[a%d]", i))
		}
	}
	if allAudio {
		fmt.Fprintf(&fc, "%sconcat=n=%d:v=1:a=1[vout][aout]", maps.String(), len(clips))
	} else {
		fmt.Fprintf(&fc, "%sconcat=n=%d:v=1:a=0[vout]", maps.String(), len(clips))
	}

	args = append(args, "-filter_complex", fc.String(), "-map", "[vout]")
	if allAudio {
		args = append(args, "-map", "[aout]", "-c:a", "aac", "-b:a", "192k")
	}
	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outPath,
	)

	return media.Plan{Args: args, Output: outPath, Duration: total}, nil
}

// ff formats a float for an ffmpeg argument: fixed-point, trimmed, never
// scientific notation.
func ff(f float64) string {
	s := fmt.Sprintf("%.3f", f)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func seconds(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }
