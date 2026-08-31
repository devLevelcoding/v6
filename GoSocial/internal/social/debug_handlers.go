package social

import (
	"net/http"
	"strconv"

	"gosocial/internal/events"
)

// InternalState handles GET /internal/state, serving the current
// FeedState projection as JSON so the graphql-backing service can fetch
// it without sharing this process's memory. Not exposed through the
// gateway; called directly, container-to-container.
func (h *Handler) InternalState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Store.State())
}

// BreakerStatus handles GET /debug/breaker, exposing the profilesvc
// circuit breaker's live state and metrics.
func (h *Handler) BreakerStatus(w http.ResponseWriter, r *http.Request) {
	if h.Profiles == nil {
		writeErr(w, http.StatusServiceUnavailable, "profilesvc client not configured")
		return
	}
	m := h.Profiles.Breaker().Metrics()
	writeJSON(w, http.StatusOK, map[string]any{
		"state": m.State.String(), "failures": m.Failures,
		"successes": m.Successes, "totalRequests": m.TotalRequests,
		"lastStateChange": m.LastStateChange,
	})
}

// ReplayDebug handles GET /debug/replay?upto=N: reconstructs feed state
// as of event index N, alongside the current (full) state, proving
// replay is re-derived from the event log rather than cached.
func (h *Handler) ReplayDebug(w http.ResponseWriter, r *http.Request) {
	evs := h.Store.Events()
	uptoStr := r.URL.Query().Get("upto")
	upto := len(evs) - 1
	if uptoStr != "" {
		if n, err := strconv.Atoi(uptoStr); err == nil {
			upto = n
		}
	}
	asOf := events.ReplayUpTo(evs, upto)
	current := events.ReplayAll(evs)
	writeJSON(w, http.StatusOK, map[string]any{
		"totalEvents":        len(evs),
		"replayedUpToIndex":  upto,
		"stateAsOfThatEvent": summarize(asOf),
		"currentFullState":   summarize(current),
	})
}

func summarize(f *events.FeedState) map[string]any {
	return map[string]any{
		"version":      f.Version,
		"userCount":    len(f.Users),
		"postCount":    len(f.Posts),
		"followEdges":  len(f.Follows),
		"messageCount": len(f.Messages),
	}
}
