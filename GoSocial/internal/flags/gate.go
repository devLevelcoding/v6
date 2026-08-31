package flags

import "net/http"

// GraphQLGateMiddleware wraps a handler and 404s (as if the route didn't
// exist) when flagName is off for the requesting user.
func (s *Store) GraphQLGateMiddleware(flagName string, getUserID func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := getUserID(r)
			if !s.Evaluate(flagName, userID) {
				http.Error(w, `{"error":"graphql API is not enabled for this user (feature flag off)"}`, http.StatusNotFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
