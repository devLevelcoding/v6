// Package media is the boundary to ffmpeg and ffprobe. Nothing else in the tree
// shells out or parses ffmpeg output. A Plan is a ready-to-run ffmpeg argument
// list plus the expected output duration (used for progress); internal/plan
// builds Plans, an Encoder runs them.
package media

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// tailWriter keeps only the last `limit` bytes written to it — a bounded stderr
// capture so a chatty ffmpeg run can't grow memory without limit (CoverGo P8).
type tailWriter struct {
	limit int
	buf   []byte
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.limit {
		t.buf = t.buf[len(t.buf)-t.limit:]
	}
	return len(p), nil
}

func (t *tailWriter) String() string { return string(t.buf) }

// slogWriter turns line-oriented output into debug log records.
type slogWriter struct {
	log    *slog.Logger
	level  slog.Level
	prefix string
	partial []byte
}

func (w *slogWriter) Write(p []byte) (int, error) {
	w.partial = append(w.partial, p...)
	for {
		i := strings.IndexByte(string(w.partial), '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.partial[:i]), "\r")
		w.partial = w.partial[i+1:]
		if line != "" {
			w.log.Log(context.Background(), w.level, "ffmpeg output", "src", w.prefix, "line", line)
		}
	}
	return len(p), nil
}

// Toolset is a located ffmpeg + ffprobe pair.
type Toolset struct {
	FFmpeg  string
	FFprobe string
}

// Locate finds ffmpeg and ffprobe on PATH (or honours the given absolute paths
// when non-empty). It returns an error naming whichever is missing.
func Locate(ffmpeg, ffprobe string) (Toolset, error) {
	resolve := func(name, override string) (string, error) {
		if override != "" {
			return override, nil
		}
		p, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("%s not found on PATH", name)
		}
		return p, nil
	}
	var t Toolset
	var err error
	if t.FFmpeg, err = resolve("ffmpeg", ffmpeg); err != nil {
		return Toolset{}, err
	}
	if t.FFprobe, err = resolve("ffprobe", ffprobe); err != nil {
		return Toolset{}, err
	}
	return t, nil
}

// Info is what ffprobe tells us about one input file.
type Info struct {
	Path     string
	Duration time.Duration
	Width    int
	Height   int
	HasVideo bool
	HasAudio bool
}

// Prober is the subset of Toolset internal/plan needs. It exists so plan-building
// is testable without ffprobe on the box.
type Prober interface {
	Probe(ctx context.Context, path string) (Info, error)
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		Duration  string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// Probe runs ffprobe and parses the streams/format it reports.
func (t Toolset) Probe(ctx context.Context, path string) (Info, error) {
	cmd := exec.CommandContext(ctx, t.FFprobe,
		"-v", "error",
		"-print_format", "json",
		"-show_format", "-show_streams",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: %w", path, cmdErr(err))
	}
	var raw ffprobeOutput
	if err := json.Unmarshal(out, &raw); err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: parsing output: %w", path, err)
	}
	info := Info{Path: path}
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			info.HasVideo = true
			if s.Width > 0 {
				info.Width, info.Height = s.Width, s.Height
			}
		case "audio":
			info.HasAudio = true
		}
	}
	if d := parseSeconds(raw.Format.Duration); d > 0 {
		info.Duration = d
	}
	return info, nil
}

// Plan is a ready-to-run ffmpeg invocation.
type Plan struct {
	Args     []string      // ffmpeg arguments, ending with the output path
	Output   string        // the output path (also the last Arg)
	Duration time.Duration // expected output length, for progress reporting
}

// Encoder runs a Plan. FFmpegEncoder is the real one; tests substitute a fake.
type Encoder interface {
	Encode(ctx context.Context, p Plan, onProgress func(fraction float64)) error
}

// FFmpegEncoder runs Plans with a real ffmpeg binary, translating ffmpeg's
// -progress stream into fraction-of-duration callbacks.
type FFmpegEncoder struct {
	Bin string
	// Log, if set, receives ffmpeg's -progress lines at debug level. It is
	// wired with io.TeeReader so the progress parser still sees every line
	// (CoverGo P8).
	Log *slog.Logger
}

func (e FFmpegEncoder) Encode(ctx context.Context, p Plan, onProgress func(float64)) error {
	args := append([]string{
		"-hide_banner", "-nostdin", "-y",
		"-progress", "pipe:1", "-nostats",
	}, p.Args...)

	cmd := exec.CommandContext(ctx, e.Bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	// Bounded stderr tail — a chatty run can't balloon memory (CoverGo P8);
	// io.MultiWriter also mirrors it to the debug log when one is set.
	stderr := &tailWriter{limit: 8 << 10}
	if e.Log != nil {
		cmd.Stderr = io.MultiWriter(stderr, &slogWriter{log: e.Log, level: slog.LevelDebug, prefix: "ffmpeg"})
	} else {
		cmd.Stderr = stderr
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var src io.Reader = stdout
	if e.Log != nil {
		src = io.TeeReader(stdout, &slogWriter{log: e.Log, level: slog.LevelDebug, prefix: "progress"})
	}
	scan := bufio.NewScanner(src)
	for scan.Scan() {
		line := scan.Text()
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "out_time_us", "out_time_ms": // ffmpeg spells it out_time_us but has shipped both
			if onProgress != nil && p.Duration > 0 {
				if us, e2 := strconv.ParseInt(strings.TrimSpace(val), 10, 64); e2 == nil {
					done := time.Duration(us) * time.Microsecond
					if key == "out_time_ms" { // some builds emit ms despite the name
						done = time.Duration(us) * time.Millisecond
					}
					onProgress(clamp(float64(done) / float64(p.Duration)))
				}
			}
		case "progress":
			if strings.TrimSpace(val) == "end" && onProgress != nil {
				onProgress(1)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		msg := lastLines(stderr.String(), 8)
		return fmt.Errorf("ffmpeg failed: %w\n%s", cmdErr(err), msg)
	}
	return nil
}

func clamp(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func parseSeconds(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}

func cmdErr(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
