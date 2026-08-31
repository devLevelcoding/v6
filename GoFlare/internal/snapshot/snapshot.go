// Package snapshot gives GoFlare's in-memory stores durability across a
// restart without pulling in a database: the whole state (projects, issues and
// each issue's sampled events) is written to one JSON file, atomically, on a
// ticker and on shutdown, and read back on boot.
//
// This is deliberately the Phase-0 answer to future.md's Phase 1 ("durable
// storage"). It is fine for a single-instance deployment and for local dev; a
// Cloud Run / multi-instance deployment still wants Postgres + an object store
// for events, and the store Snapshot/Restore methods this package uses are the
// same seam that layer would plug into.
package snapshot

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/project"
)

// file is the on-disk shape. Version guards against a future format change.
type file struct {
	Version  int               `json:"version"`
	SavedAt  time.Time         `json:"saved_at"`
	Projects []project.Project `json:"projects"`
	Groups   group.Snapshot    `json:"groups"`
}

const formatVersion = 1

// Store persists and restores a (projects, groups) pair to path.
type Store struct {
	path    string
	log     *slog.Logger
	changes atomic.Int64 // bumped by the caller via Touch; a no-op flush is skipped
	writeMu sync.Mutex   // serializes Save so the ticker and shutdown never race the file
	saved   int64        // changes counter as of the last successful Save; guarded by writeMu
}

// New returns a Store writing to path. path's directory is created on the first
// Save.
func New(path string, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{path: path, log: log}
}

// Touch marks the state dirty so the next scheduled flush actually writes.
// Call it from the ingest path after a successful group.Ingest.
func (s *Store) Touch() { s.changes.Add(1) }

// Load reads the snapshot into the stores. A missing file is not an error
// (first run). If the primary file is corrupt — a write torn by a crash, which
// on a GCS-FUSE mount is a real possibility since the swap is not atomic there
// — the `.bak` from the previous successful Save is tried before giving up.
func (s *Store) Load(projects *project.MemStore, groups *group.Store) error {
	f, from, err := s.readOne(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		if f, from, err = s.readOne(s.path + ".bak"); errors.Is(err, fs.ErrNotExist) {
			s.log.Info("snapshot: no file yet, starting empty", "path", s.path)
			return nil
		}
	}
	if err != nil {
		// primary unreadable/corrupt — fall back to the backup
		s.log.Error("snapshot: primary unreadable, trying .bak", "path", s.path, "err", err)
		f, from, err = s.readOne(s.path + ".bak")
		if errors.Is(err, fs.ErrNotExist) {
			s.log.Error("snapshot: no usable file, starting empty", "path", s.path)
			return nil
		}
		if err != nil {
			s.log.Error("snapshot: .bak also unreadable, starting empty", "err", err)
			return nil
		}
	}
	projects.Restore(f.Projects)
	groups.Restore(f.Groups)
	s.writeMu.Lock()
	s.saved = s.changes.Load()
	s.writeMu.Unlock()
	s.log.Info("snapshot: restored",
		"from", from, "projects", len(f.Projects), "issues", len(f.Groups.Issues), "saved_at", f.SavedAt)
	return nil
}

func (s *Store) readOne(path string) (file, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return file{}, path, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return file{}, path, err
	}
	return f, path, nil
}

// Save writes the current store state to path. It keeps the previous good copy
// as `path.bak` and writes the new state via a `.tmp` + rename when the
// filesystem supports it, falling back to a direct write (with the `.bak` as
// the recovery point) when rename fails — e.g. on a GCS-FUSE mount.
// Concurrent callers are serialized.
func (s *Store) Save(projects *project.MemStore, groups *group.Store) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	cur := s.changes.Load()
	f := file{
		Version:  formatVersion,
		SavedAt:  time.Now().UTC(),
		Projects: projects.Snapshot(),
		Groups:   groups.Snapshot(),
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	// Roll the current good file to .bak (best effort — absent on first run).
	if _, statErr := os.Stat(s.path); statErr == nil {
		if cp, rErr := os.ReadFile(s.path); rErr == nil {
			_ = os.WriteFile(s.path+".bak", cp, 0o644)
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		// rename unsupported/failed (some FUSE mounts) — write in place instead.
		_ = os.Remove(tmp)
		if err := os.WriteFile(s.path, b, 0o644); err != nil {
			return err
		}
	}
	s.saved = cur
	return nil
}
