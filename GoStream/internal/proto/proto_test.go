package proto

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, b []byte) Event {
	t.Helper()
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("not JSON: %v (%s)", err, b)
	}
	return e
}

func TestMessageWrapsJSON(t *testing.T) {
	e := decode(t, Message("room", []byte(`{"x":1}`)))
	if e.Type != "message" || e.Topic != "room" || string(e.Data) != `{"x":1}` || e.TS == 0 {
		t.Fatalf("event = %+v", e)
	}
}

func TestMessageWrapsNonJSONAsString(t *testing.T) {
	e := decode(t, Message("room", []byte("plain text")))
	var s string
	if err := json.Unmarshal(e.Data, &s); err != nil || s != "plain text" {
		t.Fatalf("data = %s (%v)", e.Data, err)
	}
}

func TestMessageEmptyIsNull(t *testing.T) {
	e := decode(t, Message("t", nil))
	if string(e.Data) != "null" {
		t.Fatalf("empty data = %s, want null", e.Data)
	}
}

func TestWelcomeAckPongError(t *testing.T) {
	if e := decode(t, Welcome("cid")); e.Type != "welcome" || e.ID != "cid" {
		t.Fatalf("welcome = %+v", e)
	}
	if e := decode(t, Ack("subscribed", "room")); e.Type != "subscribed" || e.Topic != "room" {
		t.Fatalf("ack = %+v", e)
	}
	if e := decode(t, Pong()); e.Type != "pong" {
		t.Fatalf("pong = %+v", e)
	}
	if e := decode(t, Errorf("bad %s", "thing")); e.Type != "error" || e.Error != "bad thing" {
		t.Fatalf("error = %+v", e)
	}
}
