package snapshot

import (
	"time"

	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/project"
)

// Run flushes every interval while state has changed since the last write. It
// returns when done is closed WITHOUT a final flush — the caller owns the
// shutdown Save so the two never race on the file.
func (s *Store) Run(done <-chan struct{}, interval time.Duration, projects *project.MemStore, groups *group.Store) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.flushIfDirty(projects, groups)
		case <-done:
			return
		}
	}
}

func (s *Store) flushIfDirty(projects *project.MemStore, groups *group.Store) {
	s.writeMu.Lock()
	clean := s.changes.Load() == s.saved
	s.writeMu.Unlock()
	if clean {
		return
	}
	if err := s.Save(projects, groups); err != nil {
		s.log.Error("snapshot: save failed", "path", s.path, "err", err)
		return
	}
	s.log.Debug("snapshot: saved", "path", s.path)
}
