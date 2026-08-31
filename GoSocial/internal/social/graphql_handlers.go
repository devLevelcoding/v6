package social

import (
	"io"
	"net/http"
)

// GraphQLHandler handles POST /graphql. Access is gated by the
// "graphql_api" feature flag (see flags.GraphQLGateMiddleware in main.go's
// router wiring); the query itself runs against internal/graphqlbacking.
func (h *Handler) GraphQLHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	result, err := h.GraphQL.Execute(string(body))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

// FlagsProxy exposes the flag store's HTTP handler (GET eval / PUT
// update) directly, so a flag can be flipped live via curl.
func (h *Handler) FlagsProxy() http.Handler { return h.Flags.Handler() }
