package event

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ErrNoEvent means an ingest payload carried no usable event item.
var ErrNoEvent = errors.New("event: payload has no event item")

// Envelope is a parsed Sentry envelope: a headers object followed by
// length-or-newline-delimited items.
type Envelope struct {
	Headers map[string]any
	Items   []Item
}

// Item is one envelope item.
//
// Payload aliases the body slice passed to ParseEnvelope (CoverGo U7 — no
// defensive copy). Callers that keep an Item past the lifetime of that body
// must copy Payload themselves. The ingest handlers decode synchronously and
// discard the envelope, so they don't.
type Item struct {
	Type    string
	Headers map[string]any
	Payload []byte
}

// DSN returns the envelope-header DSN, if the SDK included one.
func (e Envelope) DSN() string {
	if v, ok := e.Headers["dsn"].(string); ok {
		return v
	}
	return ""
}

// ParseEnvelope parses the newline-framed envelope format. It supports items
// with an explicit "length" header and items delimited only by newlines (both
// occur in the wild).
func ParseEnvelope(body []byte) (*Envelope, error) {
	nl := bytes.IndexByte(body, '\n')
	if nl < 0 {
		// A headers-only body with no trailing newline.
		nl = len(body)
	}
	var headers map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(body[:nl]), &headers); err != nil {
		return nil, fmt.Errorf("event: envelope headers: %w", err)
	}
	env := &Envelope{Headers: headers}

	rest := body[min(nl+1, len(body)):]
	for len(bytes.TrimSpace(rest)) > 0 {
		hEnd := bytes.IndexByte(rest, '\n')
		if hEnd < 0 {
			return nil, errors.New("event: envelope item header not terminated")
		}
		var ih map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(rest[:hEnd]), &ih); err != nil {
			return nil, fmt.Errorf("event: envelope item header: %w", err)
		}
		rest = rest[hEnd+1:]

		var payload []byte
		if lv, ok := ih["length"]; ok {
			n := int(toFloat(lv))
			if n > len(rest) {
				return nil, errors.New("event: envelope item length past end of body")
			}
			payload = rest[:n]
			rest = rest[n:]
			if len(rest) > 0 && rest[0] == '\n' {
				rest = rest[1:]
			}
		} else {
			pEnd := bytes.IndexByte(rest, '\n')
			if pEnd < 0 {
				payload = rest
				rest = nil
			} else {
				payload = rest[:pEnd]
				rest = rest[pEnd+1:]
			}
		}
		itemType, _ := ih["type"].(string)
		// Payload aliases body — see the Item doc.
		env.Items = append(env.Items, Item{Type: itemType, Headers: ih, Payload: payload})
	}
	return env, nil
}

// EventFromEnvelope returns the decoded event from the first "event" item.
func (e Envelope) EventItem() (Event, error) {
	for _, it := range e.Items {
		if it.Type == "event" {
			return Decode(it.Payload)
		}
	}
	return Event{}, ErrNoEvent
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}
