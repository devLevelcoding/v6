package event

// Per-field tolerant decoders for the polymorphic parts of an event payload:
// message (string | logentry object), exception ({values} | array | object),
// and tags (object | array-of-pairs).

import (
	"encoding/json"
	"strconv"
)

func decodeMessage(raw json.RawMessage, logentry *logEntry) string {
	if logentry != nil {
		if logentry.Formatted != "" {
			return logentry.Formatted
		}
		if logentry.Message != "" {
			return logentry.Message
		}
	}
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Message   string `json:"message"`
		Formatted string `json:"formatted"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if obj.Formatted != "" {
			return obj.Formatted
		}
		return obj.Message
	}
	return ""
}

func decodeException(raw json.RawMessage) []Exception {
	if len(raw) == 0 {
		return nil
	}
	type rawStacktrace struct {
		Frames []Frame `json:"frames"`
	}
	type rawExc struct {
		Type       string         `json:"type"`
		Value      string         `json:"value"`
		Module     string         `json:"module"`
		Stacktrace *rawStacktrace `json:"stacktrace"`
	}
	collect := func(list []rawExc) []Exception {
		out := make([]Exception, 0, len(list))
		for _, x := range list {
			ex := Exception{Type: x.Type, Value: x.Value, Module: x.Module}
			if x.Stacktrace != nil {
				ex.Frames = x.Stacktrace.Frames
			}
			out = append(out, ex)
		}
		return out
	}

	// { "values": [ ... ] }
	var wrapped struct {
		Values []rawExc `json:"values"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Values != nil {
		return collect(wrapped.Values)
	}
	// [ ... ]
	var arr []rawExc
	if err := json.Unmarshal(raw, &arr); err == nil && arr != nil {
		return collect(arr)
	}
	// single object
	var one rawExc
	if err := json.Unmarshal(raw, &one); err == nil && (one.Type != "" || one.Value != "") {
		return collect([]rawExc{one})
	}
	return nil
}

func decodeTags(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := map[string]string{}
	// object form
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		for k, v := range obj {
			out[k] = stringify(v)
		}
		if len(out) > 0 {
			return out
		}
	}
	// array-of-pairs form: [["k","v"], ...]
	var pairs [][]any
	if err := json.Unmarshal(raw, &pairs); err == nil {
		for _, p := range pairs {
			if len(p) == 2 {
				out[stringify(p[0])] = stringify(p[1])
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
