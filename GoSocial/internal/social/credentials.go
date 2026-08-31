package social

// credentials.go is the bcrypt-based user store, kept separate from the
// public, event-sourced User projection -- passwords never appear in
// the event stream.

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// Credential is a registered user's login record.
type Credential struct {
	ID           string
	Username     string
	PasswordHash string
}

// CredentialStore is a thread-safe in-memory credential store.
type CredentialStore struct {
	mu         sync.RWMutex
	byID       map[string]*Credential
	byUsername map[string]*Credential
}

func NewCredentialStore() *CredentialStore {
	return &CredentialStore{
		byID:       make(map[string]*Credential),
		byUsername: make(map[string]*Credential),
	}
}

// Create adds a new credential. Returns an error if the username is taken.
func (s *CredentialStore) Create(username, passwordHash string) (*Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byUsername[username]; exists {
		return nil, fmt.Errorf("username %q already registered", username)
	}
	c := &Credential{ID: uuid.NewString(), Username: username, PasswordHash: passwordHash}
	s.byID[c.ID] = c
	s.byUsername[username] = c
	return c, nil
}

func (s *CredentialStore) FindByUsername(username string) (*Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.byUsername[username]
	if !ok {
		return nil, fmt.Errorf("user %q not found", username)
	}
	return c, nil
}

// HashPassword hashes password with bcrypt at cost 12.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}

// CheckPassword reports whether password matches hash.
func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
