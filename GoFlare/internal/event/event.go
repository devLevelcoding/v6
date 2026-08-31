// Package event is GoFlare's model of an error event and the parser for the
// Sentry ingest formats (envelope and the legacy store endpoint). The model is
// a deliberately small subset of Sentry's event schema — enough to group,
// title and display an error — with tolerant decoding because real SDK
// payloads vary by language and version.
//
// This file holds the model and its display helpers (Title, Culprit). Tolerant
// payload decoding is in decode.go / decode_fields.go; the envelope framing and
// ingest auth are in envelope.go / auth.go.
package event

import (
	"sort"
	"strings"
	"time"
)

// Level is the severity of an event.
type Level string

// Known levels. Unknown values are kept as-is.
const (
	LevelFatal   Level = "fatal"
	LevelError   Level = "error"
	LevelWarning Level = "warning"
	LevelInfo    Level = "info"
	LevelDebug   Level = "debug"
)

// Frame is one stack frame.
type Frame struct {
	Filename string `json:"filename,omitempty"`
	Function string `json:"function,omitempty"`
	Module   string `json:"module,omitempty"`
	AbsPath  string `json:"abs_path,omitempty"`
	Lineno   int    `json:"lineno,omitempty"`
	Colno    int    `json:"colno,omitempty"`
	InApp    bool   `json:"in_app"`
}

// Exception is one thrown error, possibly one of a chain.
type Exception struct {
	Type   string  `json:"type"`
	Value  string  `json:"value"`
	Module string  `json:"module,omitempty"`
	Frames []Frame `json:"frames,omitempty"`
}

// Event is a normalized error event.
type Event struct {
	EventID     string            `json:"event_id"`
	ProjectID   string            `json:"project_id"`
	Timestamp   time.Time         `json:"timestamp"`
	Received    time.Time         `json:"received"`
	Platform    string            `json:"platform,omitempty"`
	Level       Level             `json:"level,omitempty"`
	Logger      string            `json:"logger,omitempty"`
	ServerName  string            `json:"server_name,omitempty"`
	Release     string            `json:"release,omitempty"`
	Environment string            `json:"environment,omitempty"`
	Transaction string            `json:"transaction,omitempty"`
	Message     string            `json:"message,omitempty"`
	Exceptions  []Exception       `json:"exceptions,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Fingerprint []string          `json:"fingerprint,omitempty"`
	SDK         string            `json:"sdk,omitempty"`
}

// Title is the one-line summary shown in the issue list. It mirrors Sentry's
// rules: the last exception's "Type: value", else the message, else a fallback.
func (e Event) Title() string {
	if x, ok := e.lastException(); ok {
		switch {
		case x.Type != "" && x.Value != "":
			return truncate(x.Type+": "+x.Value, 200)
		case x.Type != "":
			return x.Type
		case x.Value != "":
			return truncate(x.Value, 200)
		}
	}
	if e.Message != "" {
		return truncate(firstLine(e.Message), 200)
	}
	if e.Level != "" {
		return string(e.Level) + " event"
	}
	return "<unknown>"
}

// Culprit is the code location blamed for the event: the top in-app frame, or
// the transaction name.
func (e Event) Culprit() string {
	if x, ok := e.lastException(); ok {
		frames := x.Frames
		if top, ok := topInApp(frames); ok {
			return frameLabel(top)
		}
		if len(frames) > 0 {
			return frameLabel(frames[len(frames)-1])
		}
	}
	return e.Transaction
}

func (e Event) lastException() (Exception, bool) {
	if len(e.Exceptions) == 0 {
		return Exception{}, false
	}
	return e.Exceptions[len(e.Exceptions)-1], true
}

func topInApp(frames []Frame) (Frame, bool) {
	// Sentry lists frames oldest-first; the crash site is last.
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].InApp {
			return frames[i], true
		}
	}
	return Frame{}, false
}

func frameLabel(f Frame) string {
	loc := f.Module
	if loc == "" {
		loc = f.Filename
	}
	switch {
	case f.Function != "" && loc != "":
		return f.Function + " (" + loc + ")"
	case f.Function != "":
		return f.Function
	default:
		return loc
	}
}

// SortedTagKeys returns an event's tag keys in stable order.
func (e Event) SortedTagKeys() []string {
	keys := make([]string, 0, len(e.Tags))
	for k := range e.Tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
