package spec

import "testing"

func ptr(f float64) *float64 { return &f }

func TestNormalizeFillsDefaults(t *testing.T) {
	s := Spec{Template: TemplateSlideshow, Slideshow: &Slideshow{Images: []string{"a.jpg"}}}
	s.Normalize()
	if s.Width != DefaultWidth || s.Height != DefaultHeight || s.FPS != DefaultFPS {
		t.Fatalf("geometry defaults not applied: %+v", s)
	}
	if s.Slideshow.SecondsPerImage != DefaultSecondsPerImage {
		t.Fatalf("seconds_per_image default not applied: %v", s.Slideshow.SecondsPerImage)
	}
	if s.Slideshow.Crossfade() != DefaultCrossfadeSeconds {
		t.Fatalf("crossfade default not applied: %v", s.Slideshow.Crossfade())
	}
}

func TestNormalizeKeepsExplicitZeroCrossfade(t *testing.T) {
	s := Spec{Template: TemplateSlideshow, Slideshow: &Slideshow{Images: []string{"a.jpg", "b.jpg"}, CrossfadeSeconds: ptr(0)}}
	s.Normalize()
	if s.Slideshow.Crossfade() != 0 {
		t.Fatalf("explicit 0 crossfade should stay 0, got %v", s.Slideshow.Crossfade())
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		spec    Spec
		wantErr bool
	}{
		{"ok slideshow", Spec{Template: TemplateSlideshow, Width: 1080, Height: 1920, FPS: 30,
			Slideshow: &Slideshow{Images: []string{"a.jpg", "b.jpg"}, SecondsPerImage: 4, CrossfadeSeconds: ptr(0.5)}}, false},
		{"ok concat", Spec{Template: TemplateConcat, Width: 1920, Height: 1080, FPS: 30,
			Concat: &Concat{Clips: []string{"a.mp4", "b.mp4"}}}, false},
		{"no template", Spec{Width: 1080, Height: 1920, FPS: 30}, true},
		{"unknown template", Spec{Template: "montage", Width: 1080, Height: 1920, FPS: 30}, true},
		{"slideshow missing block", Spec{Template: TemplateSlideshow, Width: 1080, Height: 1920, FPS: 30}, true},
		{"slideshow no images", Spec{Template: TemplateSlideshow, Width: 1080, Height: 1920, FPS: 30,
			Slideshow: &Slideshow{SecondsPerImage: 4}}, true},
		{"slideshow blank image", Spec{Template: TemplateSlideshow, Width: 1080, Height: 1920, FPS: 30,
			Slideshow: &Slideshow{Images: []string{"a.jpg", "  "}, SecondsPerImage: 4, CrossfadeSeconds: ptr(0.5)}}, true},
		{"crossfade >= hold", Spec{Template: TemplateSlideshow, Width: 1080, Height: 1920, FPS: 30,
			Slideshow: &Slideshow{Images: []string{"a.jpg"}, SecondsPerImage: 2, CrossfadeSeconds: ptr(2)}}, true},
		{"concat one clip", Spec{Template: TemplateConcat, Width: 1920, Height: 1080, FPS: 30,
			Concat: &Concat{Clips: []string{"only.mp4"}}}, true},
		{"odd dimensions", Spec{Template: TemplateSlideshow, Width: 1081, Height: 1920, FPS: 30,
			Slideshow: &Slideshow{Images: []string{"a.jpg"}, SecondsPerImage: 4, CrossfadeSeconds: ptr(0)}}, true},
		{"both blocks", Spec{Template: TemplateSlideshow, Width: 1080, Height: 1920, FPS: 30,
			Slideshow: &Slideshow{Images: []string{"a.jpg"}, SecondsPerImage: 4, CrossfadeSeconds: ptr(0)},
			Concat:    &Concat{Clips: []string{"a.mp4", "b.mp4"}}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.spec.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}
