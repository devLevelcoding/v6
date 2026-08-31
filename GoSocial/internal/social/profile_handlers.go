package social

import (
	"context"
	"net/http"
	"time"
)

// Profile handles GET /users/{id}/profile via an internal gRPC call to
// profilesvc, through the circuit breaker.
func (h *Handler) Profile(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if h.Profiles == nil {
		writeErr(w, http.StatusServiceUnavailable, "profilesvc client not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	profile, err := h.Profiles.GetProfile(ctx, userID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "profilesvc call failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, profile)
}
