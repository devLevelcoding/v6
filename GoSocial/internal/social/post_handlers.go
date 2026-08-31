package social

import (
	"encoding/json"
	"net/http"
	"sort"

	"gosocial/internal/events"
)

// CreatePost handles POST /posts {type,content,songId?}. Rate-limited
// per-user before this handler ever runs (see router wiring in main.go).
func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromRequest(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var body struct {
		Type    string `json:"type"`
		Content string `json:"content"`
		SongID  int    `json:"songId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Type == "" {
		body.Type = "post"
	}
	postID, err := h.Store.CreatePost(userID, body.Type, body.Content, body.SongID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": postID, "authorId": userID, "type": body.Type})
}

// Feed handles GET /feed, newest posts first.
func (h *Handler) Feed(w http.ResponseWriter, r *http.Request) {
	state := h.Store.State()
	posts := make([]*events.Post, len(state.Posts))
	copy(posts, state.Posts)
	sort.Slice(posts, func(i, j int) bool { return posts[i].OccurredAt.After(posts[j].OccurredAt) })
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts, "eventVersion": state.Version})
}
