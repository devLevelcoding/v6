package social

import (
	"context"
	"net/http"
	"strings"

	"gosocial/internal/auth"
)

type ctxKey string

const userCtxKey ctxKey = "social_user"

// RequireAuth validates a JWT (internal/auth.ValidateToken) and injects
// the claims into the request context. It's applied both directly on
// this API and again behind the gateway (internal/gateway), which also
// validates the token and injects X-User-ID -- defense in depth.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(h, "Bearer ")
			claims, err := auth.ValidateToken(token, secret)
			if err != nil {
				http.Error(w, `{"error":"invalid token: `+err.Error()+`"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), userCtxKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromRequest extracts the authenticated user ID set by RequireAuth
// (falls back to the X-User-ID header the gateway's auth plugin injects,
// for handlers reachable ONLY through the gateway).
func UserIDFromRequest(r *http.Request) (string, bool) {
	if claims, ok := r.Context().Value(userCtxKey).(*auth.Claims); ok {
		return claims.Subject, true
	}
	if id := r.Header.Get("X-User-ID"); id != "" {
		return id, true
	}
	return "", false
}

func usernameFromRequest(r *http.Request) string {
	if claims, ok := r.Context().Value(userCtxKey).(*auth.Claims); ok {
		return claims.Username
	}
	return r.Header.Get("X-Username")
}
