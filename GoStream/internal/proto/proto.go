// Package proto is GoStream's small JSON wire protocol. Clients send Commands
// over the socket; the server sends Events. Publishers POST a bare JSON value
// and the server wraps it in a "message" Event. Everything is one JSON object
// per WebSocket text frame.
package proto

import (
	"encoding/json"
	"fmt"
	"time"
)

// Command is a client → server message.
type Command struct {
	Type  string          `json:"type"` // subscribe | unsubscribe | publish | ping
	Topic string          `json:"topic,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Event is a server → client message.
type Event struct {
	Type  string          `json:"type"` // welcome | message | subscribed | unsubscribed | pong | error
	Topic string          `json:"topic,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
	ID    string          `json:"id,omitempty"`
	TS    int64           `json:"ts,omitempty"`
}

func marshal(e Event) []byte {
	b, err := json.Marshal(e)
	if err != nil {
		return []byte(`{"type":"error","error":"internal encode failure"}`)
	}
	return b
}

// Welcome greets a new connection with its assigned client id.
func Welcome(clientID string) []byte {
	return marshal(Event{Type: "welcome", ID: clientID, TS: nowMS()})
}

// Message is the fan-out payload delivered to a topic's subscribers. data must
// be valid JSON (a bare value is fine); non-JSON is wrapped as a JSON string.
func Message(topic string, data []byte) []byte {
	return marshal(Event{Type: "message", Topic: topic, Data: asJSON(data), TS: nowMS()})
}

// Ack confirms a subscribe / unsubscribe.
func Ack(typ, topic string) []byte {
	return marshal(Event{Type: typ, Topic: topic, TS: nowMS()})
}

// Pong answers a client ping.
func Pong() []byte { return marshal(Event{Type: "pong", TS: nowMS()}) }

// Errorf reports a problem to the client without closing the socket.
func Errorf(format string, a ...any) []byte {
	return marshal(Event{Type: "error", Error: fmt.Sprintf(format, a...), TS: nowMS()})
}

// asJSON returns b unchanged if it is already valid JSON, otherwise b quoted as
// a JSON string.
func asJSON(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	if json.Valid(b) {
		return json.RawMessage(b)
	}
	q, _ := json.Marshal(string(b))
	return json.RawMessage(q)
}

func nowMS() int64 { return time.Now().UnixMilli() }
