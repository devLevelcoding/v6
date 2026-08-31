package media

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestTailWriterKeepsOnlyTheTail(t *testing.T) {
	w := &tailWriter{limit: 16}
	for i := 0; i < 100; i++ {
		_, _ = io.WriteString(w, "0123456789")
	}
	got := w.String()
	if len(got) != 16 {
		t.Fatalf("tail length = %d, want 16", len(got))
	}
	if !strings.HasSuffix("0123456789012345678901234567890123456789", got) {
		t.Fatalf("tail = %q, not a suffix of the stream", got)
	}
}

func TestSlogWriterSplitsLines(t *testing.T) {
	var n int
	h := countingHandler{fn: func() { n++ }}
	w := &slogWriter{log: slog.New(h), level: slog.LevelInfo, prefix: "x"}
	// a line split across two writes, plus a complete one, plus a dangling partial
	_, _ = io.WriteString(w, "frame=1\nframe=")
	_, _ = io.WriteString(w, "2\nframe=3")
	if n != 2 {
		t.Fatalf("logged %d lines, want 2 (the 3rd is still buffered)", n)
	}
	_, _ = io.WriteString(w, "\n")
	if n != 3 {
		t.Fatalf("after the newline, logged %d lines, want 3", n)
	}
}

type countingHandler struct{ fn func() }

func (h countingHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (h countingHandler) Handle(context.Context, slog.Record) error { h.fn(); return nil }
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler        { return h }
func (h countingHandler) WithGroup(string) slog.Handler             { return h }
