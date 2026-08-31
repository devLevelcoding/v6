// Package job is the render job: a Spec plus its lifecycle state. The Store is
// an in-memory stand-in for the Postgres table a later phase adds — see
// ../../future.md §3.
package job

import (
	"sort"
	"sync"
	"time"

	"github.com/levelcodingdev/gorender/internal/spec"
	"github.com/levelcodingdev/gorender/internal/uid"
)

// Status is where a job is in its lifecycle.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Job is one render request and everything we know about how it went.
type Job struct {
	ID         string     `json:"id"`
	Spec       spec.Spec  `json:"spec"`
	Status     Status     `json:"status"`
	Progress   float64    `json:"progress"` // 0..1
	Artifact   string     `json:"artifact,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (j *Job) clone() *Job {
	c := *j
	if j.StartedAt != nil {
		t := *j.StartedAt
		c.StartedAt = &t
	}
	if j.FinishedAt != nil {
		t := *j.FinishedAt
		c.FinishedAt = &t
	}
	return &c
}

// Store holds jobs in memory. All methods are safe for concurrent use and hand
// back copies, so callers can never mutate stored state behind the lock.
type Store struct {
	mu  sync.RWMutex
	m   map[string]*Job
	now func() time.Time
}

func NewStore() *Store {
	return &Store{m: make(map[string]*Job), now: time.Now}
}

// Create records a new queued job for s.
func (s *Store) Create(sp spec.Spec) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := &Job{
		ID:        uid.New(),
		Spec:      sp,
		Status:    StatusQueued,
		CreatedAt: s.now(),
	}
	s.m[j.ID] = j
	return j.clone()
}

// Get returns a copy of the job, or false.
func (s *Store) Get(id string) (*Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.m[id]
	if !ok {
		return nil, false
	}
	return j.clone(), true
}

// List returns all jobs, newest first.
func (s *Store) List() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Job, 0, len(s.m))
	for _, j := range s.m {
		out = append(out, j.clone())
	}
	sort.Slice(out, func(i, k int) bool { return out[i].CreatedAt.After(out[k].CreatedAt) })
	return out
}

// Update applies fn to the stored job under the lock. It is the only way to
// mutate a job. Returns false if the id is unknown.
func (s *Store) Update(id string, fn func(*Job)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.m[id]
	if !ok {
		return false
	}
	fn(j)
	return true
}

// MarkRunning is a convenience Update: status→running, StartedAt→now.
func (s *Store) MarkRunning(id string) {
	s.Update(id, func(j *Job) {
		t := s.now()
		j.Status = StatusRunning
		j.StartedAt = &t
	})
}

// MarkDone is a convenience Update: on err, status→failed + Error; otherwise
// status→succeeded + Artifact + Progress 1. FinishedAt→now either way.
func (s *Store) MarkDone(id, artifact string, err error) {
	s.Update(id, func(j *Job) {
		t := s.now()
		j.FinishedAt = &t
		if err != nil {
			j.Status = StatusFailed
			j.Error = err.Error()
			return
		}
		j.Status = StatusSucceeded
		j.Artifact = artifact
		j.Progress = 1
	})
}
