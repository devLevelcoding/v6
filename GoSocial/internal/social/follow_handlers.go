package social

import (
	"encoding/json"
	"net/http"

	"gosocial/internal/events"
)

// RequestFollowHandler handles POST /follow/{userId} and notifies the
// target user over WebSocket.
func (h *Handler) RequestFollowHandler(w http.ResponseWriter, r *http.Request) {
	followerID, ok := UserIDFromRequest(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	followeeID := r.PathValue("userId")
	reqID, err := h.Store.RequestFollow(followerID, followeeID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	fromName := usernameFromRequest(r)
	h.Notifier.Push("follow_request", followeeID, followerID, fromName, fromName+" wants to follow you")
	writeJSON(w, http.StatusCreated, map[string]string{"requestId": reqID})
}

// AcceptFollowHandler handles POST /follow/{requestId}/accept.
func (h *Handler) AcceptFollowHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromRequest(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	reqID := r.PathValue("requestId")
	if err := h.Store.AcceptFollow(reqID, userID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	state := h.Store.State()
	if edge, ok := state.Follows[reqID]; ok {
		fromName := usernameFromRequest(r)
		h.Notifier.Push("follow_accepted", edge.FollowerID, userID, fromName, fromName+" accepted your follow request")
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// RejectFollowHandler handles POST /follow/{requestId}/reject.
func (h *Handler) RejectFollowHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromRequest(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	reqID := r.PathValue("requestId")
	if err := h.Store.RejectFollow(reqID, userID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// FollowRequests handles GET /follow/requests -- pending incoming requests.
func (h *Handler) FollowRequests(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromRequest(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	state := h.Store.State()
	var out []*events.FollowEdge
	for _, edge := range state.Follows {
		if edge.FolloweeID == userID && edge.Status == "pending" {
			out = append(out, edge)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

// SendMessage handles POST /messages/{toUserId} {content}: 403 unless
// the two users mutually follow each other, notifies on success.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	fromID, ok := UserIDFromRequest(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	toID := r.PathValue("toUserId")
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	msgID, err := h.Store.SendMessage(fromID, toID, body.Content)
	if err != nil {
		writeErr(w, http.StatusForbidden, err.Error())
		return
	}
	fromName := usernameFromRequest(r)
	h.Notifier.Push("new_message", toID, fromID, fromName, fromName+": "+body.Content)
	writeJSON(w, http.StatusCreated, map[string]string{"messageId": msgID})
}
