package blob

import (
	"context"
	"errors"
	"testing"
	"time"
)

func testStore(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	if _, err := s.Get(ctx, "missing"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("Get(missing) = %v, want ErrNotExist", err)
	}
	if err := s.Delete(ctx, "missing"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("Delete(missing) = %v, want ErrNotExist", err)
	}

	body := []byte(`{"event_id":"abc","message":"boom"}`)
	key := EventKey("proj1", "abc", time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC))
	if key != "events/proj1/2026/08/30/abc.json" {
		t.Fatalf("EventKey = %q", key)
	}
	if err := s.Put(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, key)
	if err != nil || string(got) != string(body) {
		t.Fatalf("Get after Put = %q, %v", got, err)
	}

	// Put returns a copy: mutating the source must not change the stored blob.
	body[0] = 'X'
	got, _ = s.Get(ctx, key)
	if got[0] == 'X' {
		t.Fatal("stored blob aliased the caller's slice")
	}

	_ = s.Put(ctx, EventKey("proj1", "def", time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)), []byte("2"))
	_ = s.Put(ctx, EventKey("proj2", "ghi", time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)), []byte("3"))

	keys, err := s.List(ctx, "events/proj1/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("List(events/proj1/) = %v, want 2", keys)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, key); !errors.Is(err, ErrNotExist) {
		t.Fatalf("Get after Delete = %v, want ErrNotExist", err)
	}
}

func TestMemStore(t *testing.T) { testStore(t, NewMemStore()) }
func TestLocalStore(t *testing.T) {
	s, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testStore(t, s)
}

func TestLocalStoreRejectsBadKeys(t *testing.T) {
	s, _ := NewLocalStore(t.TempDir())
	ctx := context.Background()
	for _, k := range []string{"", "../escape", "a/../../b"} {
		if err := s.Put(ctx, k, []byte("x")); err == nil {
			t.Errorf("Put(%q) should be rejected", k)
		}
	}
}
