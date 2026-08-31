package event

import (
	"testing"
	"time"
)

func TestDecodeExceptionWrapped(t *testing.T) {
	payload := []byte(`{
		"event_id":"abc-123",
		"level":"error",
		"platform":"python",
		"timestamp": 1700000000.5,
		"exception":{"values":[
			{"type":"ValueError","value":"bad input","stacktrace":{"frames":[
				{"filename":"app/main.py","function":"handle","in_app":true,"lineno":42}
			]}}
		]}
	}`)
	e, err := Decode(payload)
	if err != nil {
		t.Fatal(err)
	}
	if e.EventID != "abc123" {
		t.Errorf("event_id not dash-stripped: %q", e.EventID)
	}
	if len(e.Exceptions) != 1 || e.Exceptions[0].Type != "ValueError" {
		t.Fatalf("exception decode: %+v", e.Exceptions)
	}
	if len(e.Exceptions[0].Frames) != 1 || !e.Exceptions[0].Frames[0].InApp {
		t.Fatalf("frame decode: %+v", e.Exceptions[0].Frames)
	}
	if e.Timestamp.UTC().Unix() != 1700000000 {
		t.Errorf("timestamp = %v", e.Timestamp)
	}
	if got := e.Title(); got != "ValueError: bad input" {
		t.Errorf("title = %q", got)
	}
	if got := e.Culprit(); got != "handle (app/main.py)" {
		t.Errorf("culprit = %q", got)
	}
}

func TestDecodeExceptionArrayAndStringTimestamp(t *testing.T) {
	payload := []byte(`{
		"exception":[{"type":"TypeError","value":"x"}],
		"timestamp":"2023-11-14T22:13:20Z"
	}`)
	e, err := Decode(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Exceptions) != 1 || e.Exceptions[0].Type != "TypeError" {
		t.Fatalf("array exception: %+v", e.Exceptions)
	}
	if !e.Timestamp.Equal(time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)) {
		t.Errorf("string timestamp = %v", e.Timestamp)
	}
	if e.Level != LevelError {
		t.Errorf("default level = %q, want error", e.Level)
	}
}

func TestDecodeMessageAndTags(t *testing.T) {
	t.Run("logentry + object tags", func(t *testing.T) {
		e, err := Decode([]byte(`{"logentry":{"message":"raw %s","formatted":"raw val"},"tags":{"env":"prod","code":500}}`))
		if err != nil {
			t.Fatal(err)
		}
		if e.Message != "raw val" {
			t.Errorf("message = %q", e.Message)
		}
		if e.Tags["env"] != "prod" || e.Tags["code"] != "500" {
			t.Errorf("tags = %+v", e.Tags)
		}
	})
	t.Run("string message + pair tags", func(t *testing.T) {
		e, err := Decode([]byte(`{"message":"just a string","tags":[["a","1"],["b","2"]]}`))
		if err != nil {
			t.Fatal(err)
		}
		if e.Message != "just a string" {
			t.Errorf("message = %q", e.Message)
		}
		if e.Tags["a"] != "1" || e.Tags["b"] != "2" {
			t.Errorf("pair tags = %+v", e.Tags)
		}
	})
}

func TestDecodeLevelNormalization(t *testing.T) {
	for in, want := range map[string]Level{
		"critical": LevelFatal,
		"warn":     LevelWarning,
		"WARNING":  LevelWarning,
		"log":      LevelInfo,
	} {
		e, err := Decode([]byte(`{"level":"` + in + `","message":"x"}`))
		if err != nil {
			t.Fatal(err)
		}
		if e.Level != want {
			t.Errorf("level %q → %q, want %q", in, e.Level, want)
		}
	}
}

func TestTitleFallbacks(t *testing.T) {
	if got := (Event{Message: "boom\nsecond line"}).Title(); got != "boom" {
		t.Errorf("message title = %q", got)
	}
	if got := (Event{Level: LevelWarning}).Title(); got != "warning event" {
		t.Errorf("level title = %q", got)
	}
	if got := (Event{}).Title(); got != "<unknown>" {
		t.Errorf("empty title = %q", got)
	}
}
