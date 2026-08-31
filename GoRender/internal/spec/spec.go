// Package spec is the render request: what GoRender should produce. A Spec names
// a template and carries that template's inputs. It is the JSON body of
// POST /v1/jobs. Validation and default-filling live here; turning a Spec into
// an ffmpeg invocation is internal/plan's job.
package spec

import (
	"errors"
	"fmt"
	"strings"
)

// Template names the kind of render.
const (
	TemplateSlideshow = "slideshow" // N still images (+ optional audio) → video
	TemplateConcat    = "concat"    // join video clips end to end
)

// Spec is one render request.
type Spec struct {
	Template string `json:"template"`

	// Output geometry. Zero means "use the default" (see Normalize).
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	FPS    int `json:"fps,omitempty"`

	Slideshow *Slideshow `json:"slideshow,omitempty"`
	Concat    *Concat    `json:"concat,omitempty"`
}

// Slideshow turns still images into a video, optionally over an audio bed, with
// a crossfade between consecutive images.
type Slideshow struct {
	Images          []string `json:"images"`                      // file paths on the render host
	Audio           string   `json:"audio,omitempty"`             // optional audio bed
	SecondsPerImage float64  `json:"seconds_per_image,omitempty"` // hold time per image
	// CrossfadeSeconds: nil → default; 0 → crossfade disabled; >0 → that duration.
	CrossfadeSeconds *float64 `json:"crossfade_seconds,omitempty"`
}

// Weight is a rough encode-cost estimate (1–4) used by the worker pool's
// cost-weighted concurrency limiter (CoverGo U18): heavier jobs hold more of
// the pool's budget so a box isn't oversubscribed by several 4K renders at once.
func (s *Spec) Weight() int64 {
	px := s.Width * s.Height
	switch {
	case px >= 3840*2160: // 4K and up
		return 4
	case px >= 1920*1080: // 1080p / portrait 1080×1920
		return 2
	default:
		return 1
	}
}

// Crossfade returns the effective crossfade duration in seconds (0 = disabled).
func (s *Slideshow) Crossfade() float64 {
	if s.CrossfadeSeconds == nil {
		return DefaultCrossfadeSeconds
	}
	return *s.CrossfadeSeconds
}

// Concat joins video clips in order.
type Concat struct {
	Clips []string `json:"clips"` // file paths on the render host
}

// Defaults. A portrait 1080×1920 canvas at 30 fps — the reel/story shape the
// leadMarketing generator and crm6 both target.
const (
	DefaultWidth            = 1080
	DefaultHeight           = 1920
	DefaultFPS              = 30
	DefaultSecondsPerImage  = 4.0
	DefaultCrossfadeSeconds = 0.5
)

// Normalize fills zero-valued fields with defaults. Call it before Validate and
// before handing the Spec to internal/plan.
func (s *Spec) Normalize() {
	if s.Width <= 0 {
		s.Width = DefaultWidth
	}
	if s.Height <= 0 {
		s.Height = DefaultHeight
	}
	if s.FPS <= 0 {
		s.FPS = DefaultFPS
	}
	if s.Slideshow != nil {
		if s.Slideshow.SecondsPerImage <= 0 {
			s.Slideshow.SecondsPerImage = DefaultSecondsPerImage
		}
		if s.Slideshow.CrossfadeSeconds == nil {
			d := DefaultCrossfadeSeconds
			s.Slideshow.CrossfadeSeconds = &d
		}
	}
}

// Validate reports the first problem with the Spec, or nil. It checks shape and
// consistency only; whether the input files exist is discovered later, by
// ffprobe/ffmpeg, and surfaces as a job failure.
func (s *Spec) Validate() error {
	switch s.Template {
	case TemplateSlideshow:
		if s.Slideshow == nil {
			return errors.New("template is \"slideshow\" but the slideshow block is missing")
		}
		if s.Concat != nil {
			return errors.New("template is \"slideshow\" but a concat block was also given")
		}
		if len(s.Slideshow.Images) == 0 {
			return errors.New("slideshow.images is empty")
		}
		for i, p := range s.Slideshow.Images {
			if strings.TrimSpace(p) == "" {
				return fmt.Errorf("slideshow.images[%d] is blank", i)
			}
		}
		if s.Slideshow.SecondsPerImage <= 0 {
			return errors.New("slideshow.seconds_per_image must be > 0")
		}
		if xf := s.Slideshow.Crossfade(); xf < 0 {
			return errors.New("slideshow.crossfade_seconds must be >= 0")
		} else if xf >= s.Slideshow.SecondsPerImage {
			return fmt.Errorf("slideshow.crossfade_seconds (%.2f) must be < seconds_per_image (%.2f)", xf, s.Slideshow.SecondsPerImage)
		}
	case TemplateConcat:
		if s.Concat == nil {
			return errors.New("template is \"concat\" but the concat block is missing")
		}
		if s.Slideshow != nil {
			return errors.New("template is \"concat\" but a slideshow block was also given")
		}
		if len(s.Concat.Clips) < 2 {
			return errors.New("concat.clips needs at least 2 entries")
		}
		for i, p := range s.Concat.Clips {
			if strings.TrimSpace(p) == "" {
				return fmt.Errorf("concat.clips[%d] is blank", i)
			}
		}
	case "":
		return errors.New("template is required (\"slideshow\" or \"concat\")")
	default:
		return fmt.Errorf("unknown template %q", s.Template)
	}

	if s.Width < 16 || s.Height < 16 {
		return errors.New("width and height must each be >= 16")
	}
	if s.Width%2 != 0 || s.Height%2 != 0 {
		return errors.New("width and height must be even (yuv420p requirement)")
	}
	if s.FPS < 1 || s.FPS > 240 {
		return errors.New("fps must be between 1 and 240")
	}
	return nil
}
