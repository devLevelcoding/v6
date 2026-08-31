package event

// Tolerant decoding of a raw SDK event payload. Real payloads vary by language
// and SDK version, so every sub-structure that can take more than one shape is
// parsed defensively here (and in decode_fields.go) rather than with a single
// rigid struct.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rawEventPool reuses the decode scratch struct (CoverGo U7). json.Unmarshal
// overwrites every field, so a zeroed reused struct is equivalent to a fresh
// one; Decode copies everything it needs into the returned Event before the
// struct goes back.
var rawEventPool = sync.Pool{New: func() any { return new(rawEvent) }}

type logEntry struct {
	Message   string `json:"message"`
	Formatted string `json:"formatted"`
}

type rawEvent struct {
	EventID     string          `json:"event_id"`
	Timestamp   json.RawMessage `json:"timestamp"`
	Platform    string          `json:"platform"`
	Level       string          `json:"level"`
	Logger      string          `json:"logger"`
	ServerName  string          `json:"server_name"`
	Release     string          `json:"release"`
	Environment string          `json:"environment"`
	Transaction string          `json:"transaction"`
	Message     json.RawMessage `json:"message"`
	Logentry    *logEntry       `json:"logentry"`
	Exception   json.RawMessage `json:"exception"`
	Tags        json.RawMessage `json:"tags"`
	Fingerprint []string        `json:"fingerprint"`
	SDK         *struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"sdk"`
}

// Decode parses one raw event payload (the JSON an SDK sends) into an Event.
// Unknown or malformed sub-structures are dropped, not fatal.
func Decode(payload []byte) (Event, error) {
	r := rawEventPool.Get().(*rawEvent)
	*r = rawEvent{}
	defer rawEventPool.Put(r)
	if err := json.Unmarshal(payload, r); err != nil {
		return Event{}, fmt.Errorf("event: decode: %w", err)
	}
	e := Event{
		EventID:     strings.ReplaceAll(r.EventID, "-", ""),
		Platform:    r.Platform,
		Level:       normalizeLevel(r.Level),
		Logger:      r.Logger,
		ServerName:  r.ServerName,
		Release:     r.Release,
		Environment: r.Environment,
		Transaction: r.Transaction,
		Timestamp:   parseTimestamp(r.Timestamp),
		Message:     decodeMessage(r.Message, r.Logentry),
		Exceptions:  decodeException(r.Exception),
		Tags:        decodeTags(r.Tags),
		Fingerprint: r.Fingerprint,
	}
	if r.SDK != nil && r.SDK.Name != "" {
		e.SDK = r.SDK.Name + "/" + r.SDK.Version
	}
	if e.Level == "" {
		e.Level = LevelError
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	return e, nil
}

func normalizeLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "fatal", "critical":
		return LevelFatal
	case "error", "":
		if s == "" {
			return ""
		}
		return LevelError
	case "warning", "warn":
		return LevelWarning
	case "info", "log":
		return LevelInfo
	case "debug":
		return LevelDebug
	default:
		return Level(strings.ToLower(s))
	}
}

func parseTimestamp(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}
	// number: unix seconds (maybe fractional)
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil {
		sec := int64(num)
		nsec := int64((num - float64(sec)) * 1e9)
		return time.Unix(sec, nsec).UTC()
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t.UTC()
			}
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return time.Unix(int64(f), 0).UTC()
		}
	}
	return time.Time{}
}
