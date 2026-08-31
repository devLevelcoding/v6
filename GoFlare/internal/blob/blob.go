// Package blob is GoFlare's object store for raw event payloads. Events are the
// high-volume data: a thin row per event lives in Postgres for search and
// filtering (internal/group), the full JSON body goes here. The Store contract
// is opaque keys to byte blobs; MemStore (mem.go) and LocalStore (local.go)
// ship in-tree, S3Store (s3.go, any S3-compatible endpoint) is behind the same
// interface — the same shape GoSnow/internal/storage uses.
package blob

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNotExist is returned by Get/Delete when a key is absent.
var ErrNotExist = errors.New("blob: key does not exist")

// Store is the object-store contract.
type Store interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
}

// EventKey is the canonical key for a stored event payload:
// events/{projectID}/{YYYY}/{MM}/{DD}/{eventID}.json — time-prefixed so a
// retention sweep is a prefix range and a project's events are one prefix.
func EventKey(projectID, eventID string, at time.Time) string {
	at = at.UTC()
	return fmt.Sprintf("events/%s/%04d/%02d/%02d/%s.json",
		projectID, at.Year(), at.Month(), at.Day(), eventID)
}
