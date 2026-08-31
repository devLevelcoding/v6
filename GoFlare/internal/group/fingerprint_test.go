package group

import (
	"testing"

	"github.com/levelcodingdev/goflare/internal/event"
)

func excEvent(typ, val string, frames ...event.Frame) event.Event {
	return event.Event{Exceptions: []event.Exception{{Type: typ, Value: val, Frames: frames}}}
}

func TestFingerprintGroupsBySameStack(t *testing.T) {
	f := event.Frame{Module: "app.svc", Function: "charge", InApp: true}
	a := Hash(Fingerprint(excEvent("PaymentError", "card 4111 declined", f)))
	b := Hash(Fingerprint(excEvent("PaymentError", "card 5555 declined", f)))
	if a != b {
		t.Errorf("same type+stack should group regardless of value: %s vs %s", a, b)
	}
}

func TestFingerprintSplitsByDifferentStack(t *testing.T) {
	a := Hash(Fingerprint(excEvent("E", "x", event.Frame{Module: "a", Function: "f", InApp: true})))
	b := Hash(Fingerprint(excEvent("E", "x", event.Frame{Module: "b", Function: "g", InApp: true})))
	if a == b {
		t.Error("different stacks should not group")
	}
}

func TestFingerprintMessageNormalization(t *testing.T) {
	a := Hash(Fingerprint(event.Event{Message: "user 4171 not found"}))
	b := Hash(Fingerprint(event.Event{Message: "user 5522 not found"}))
	if a != b {
		t.Error("messages differing only by an embedded number should group")
	}
	c := Hash(Fingerprint(event.Event{Message: "user not found"}))
	if a == c {
		t.Error("structurally different messages should not group")
	}
}

func TestFingerprintNoFramesFallsBackToValue(t *testing.T) {
	a := Hash(Fingerprint(excEvent("Timeout", "deadline exceeded after 30s")))
	b := Hash(Fingerprint(excEvent("Timeout", "deadline exceeded after 90s")))
	if a != b {
		t.Error("frameless exceptions should group on normalized value")
	}
}

func TestFingerprintCustomRespected(t *testing.T) {
	e1 := excEvent("E", "x", event.Frame{Module: "a", Function: "f", InApp: true})
	e1.Fingerprint = []string{"my-group"}
	e2 := excEvent("TOTALLY", "different", event.Frame{Module: "z", Function: "q", InApp: true})
	e2.Fingerprint = []string{"my-group"}
	if Hash(Fingerprint(e1)) != Hash(Fingerprint(e2)) {
		t.Error("explicit matching fingerprints must group")
	}
}

func TestFingerprintDefaultToken(t *testing.T) {
	base := excEvent("E", "x", event.Frame{Module: "a", Function: "f", InApp: true})

	withToken := base
	withToken.Fingerprint = []string{"{{ default }}"}
	if Hash(Fingerprint(withToken)) != Hash(Fingerprint(base)) {
		t.Error("a lone {{ default }} token should equal the default grouping")
	}

	scoped := base
	scoped.Fingerprint = []string{"tenant-7", "{{ default }}"}
	if Hash(Fingerprint(scoped)) == Hash(Fingerprint(base)) {
		t.Error("prefixing {{ default }} with a scope should change the group")
	}
}
