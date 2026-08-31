package storage

import (
	"context"
	"errors"
	"testing"
)

func TestMemStore(t *testing.T) { runStoreSuite(t, NewMemStore()) }

func TestLocalStore(t *testing.T) {
	s, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	runStoreSuite(t, s)
}

func runStoreSuite(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	if err := s.Put(ctx, "a/b.txt", []byte("hello")); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.Get(ctx, "a/b.txt")
	if err != nil || string(got) != "hello" {
		t.Fatalf("get = %q err=%v", got, err)
	}
	if err := s.Put(ctx, "a/c.txt", []byte("x")); err != nil {
		t.Fatalf("put c: %v", err)
	}
	keys, err := s.List(ctx, "a/")
	if err != nil || len(keys) != 2 {
		t.Fatalf("list = %v err=%v", keys, err)
	}
	if err := s.Delete(ctx, "a/b.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, "a/b.txt"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}
