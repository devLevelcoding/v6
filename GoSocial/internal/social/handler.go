// Package social wires auth, rate limiting, events, notifications,
// profileclient, graphql, and flags into the go-social HTTP API:
// register/login, follow-gated DMs, posts with songs, and a feed. All
// read state (internal/events.FeedState) is a projection over an
// event-sourced store, never mutated directly by handlers.
package social

import (
	"encoding/json"
	"net/http"

	"gosocial/internal/flags"
	"gosocial/internal/graphql"
	"gosocial/internal/notify"
	"gosocial/internal/profileclient"
	"gosocial/internal/ratelimit"
)

// Handler holds every dependency the HTTP layer needs.
type Handler struct {
	Store       *SocialStore
	Creds       *CredentialStore
	Secret      string
	PostLimiter *ratelimit.PostRateLimiter
	Notifier    *notify.Notifier
	Profiles    *profileclient.Client
	GraphQL     *graphql.Executor
	Flags       *flags.Store
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
