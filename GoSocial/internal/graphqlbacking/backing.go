// Package graphqlbacking is a standalone HTTP JSON service exposing the
// event-sourced FeedState read model, giving the GraphQL layer
// (internal/graphql) a real object to resolve nested/related fields
// against, e.g. `post { author { username } }`.
package graphqlbacking

import (
	"encoding/json"
	"net/http"
	"strings"

	"gosocial/internal/events"
)

// Service serves the current FeedState projection over HTTP. stateFn is
// called on every request so the backing service always reflects the
// latest replayed event stream -- there is no separate database, the
// event store is the source of truth.
type Service struct {
	stateFn func() *events.FeedState
}

func New(stateFn func() *events.FeedState) *Service {
	return &Service{stateFn: stateFn}
}

func (s *Service) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/users/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/users/")
		state := s.stateFn()
		u, ok := state.Users[id]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		followerCount, followingCount, postCount := 0, 0, 0
		for _, edge := range state.Follows {
			if edge.Status != "accepted" {
				continue
			}
			if edge.FolloweeID == id {
				followerCount++
			}
			if edge.FollowerID == id {
				followingCount++
			}
		}
		for _, p := range state.Posts {
			if p.AuthorID == id {
				postCount++
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": u.ID, "username": u.Username, "displayName": u.DisplayName,
			"followerCount": followerCount, "followingCount": followingCount, "postCount": postCount,
		})
	})
	mux.HandleFunc("/posts/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/posts/")
		state := s.stateFn()
		for _, p := range state.Posts {
			if p.ID == id {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"id": p.ID, "authorId": p.AuthorID, "type": p.Type,
					"content": p.Content, "songId": p.SongID,
					"occurredAt": p.OccurredAt,
				})
				return
			}
		}
		http.Error(w, "not found", http.StatusNotFound)
	})
	return mux
}
