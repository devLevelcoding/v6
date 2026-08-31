package event

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestParseEnvelopeWithLength(t *testing.T) {
	item := `{"message":"hello world","level":"warning"}`
	body := fmt.Sprintf("{\"event_id\":\"9ec79c33ec9942ab8353589fcb2e04dc\"}\n"+
		"{\"type\":\"event\",\"length\":%d,\"content_type\":\"application/json\"}\n%s\n", len(item), item)

	env, err := ParseEnvelope([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if env.Headers["event_id"] != "9ec79c33ec9942ab8353589fcb2e04dc" {
		t.Errorf("headers = %+v", env.Headers)
	}
	if len(env.Items) != 1 || env.Items[0].Type != "event" {
		t.Fatalf("items = %+v", env.Items)
	}
	ev, err := env.EventItem()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Message != "hello world" || ev.Level != LevelWarning {
		t.Errorf("event = %+v", ev)
	}
}

func TestParseEnvelopeNewlineDelimited(t *testing.T) {
	body := "{\"sdk\":{\"name\":\"sentry.javascript.browser\"}}\n" +
		"{\"type\":\"event\"}\n" +
		"{\"exception\":{\"values\":[{\"type\":\"Error\",\"value\":\"nope\"}]}}\n"

	env, err := ParseEnvelope([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	ev, err := env.EventItem()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Title() != "Error: nope" {
		t.Errorf("title = %q", ev.Title())
	}
}

func TestParseEnvelopeHeadersOnly(t *testing.T) {
	env, err := ParseEnvelope([]byte(`{"event_id":"abc"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Items) != 0 {
		t.Errorf("items = %+v", env.Items)
	}
	if _, err := env.EventItem(); err != ErrNoEvent {
		t.Errorf("EventItem err = %v, want ErrNoEvent", err)
	}
}

func TestParseEnvelopeMultiItemPicksEvent(t *testing.T) {
	body := "{}\n" +
		"{\"type\":\"attachment\",\"length\":3}\nabc\n" +
		"{\"type\":\"event\"}\n{\"message\":\"found me\"}\n"
	env, err := ParseEnvelope([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Items) != 2 {
		t.Fatalf("items = %d", len(env.Items))
	}
	ev, err := env.EventItem()
	if err != nil || ev.Message != "found me" {
		t.Fatalf("event = %+v, %v", ev, err)
	}
}

func TestParseAuth(t *testing.T) {
	t.Run("x-sentry-auth header", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/api/1/envelope/", nil)
		r.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key=abc123, sentry_client=sentry.python/1.2")
		a, err := ParseAuth(r)
		if err != nil || a.PublicKey != "abc123" || a.Version != "7" {
			t.Fatalf("auth = %+v, %v", a, err)
		}
	})
	t.Run("query param", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/api/1/envelope/?sentry_key=qwe", nil)
		a, err := ParseAuth(r)
		if err != nil || a.PublicKey != "qwe" {
			t.Fatalf("auth = %+v, %v", a, err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/api/1/envelope/", nil)
		if _, err := ParseAuth(r); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestAuthFromDSN(t *testing.T) {
	key, pid, err := AuthFromDSN("https://pub@flare.example.com/42")
	if err != nil || key != "pub" || pid != "42" {
		t.Fatalf("key=%q pid=%q err=%v", key, pid, err)
	}
	if _, _, err := AuthFromDSN("https://flare.example.com/42"); err == nil {
		t.Error("expected error for DSN without key")
	}
}
