// Package plugins holds gateway plugin implementations.
package plugins

import (
	"log"
	"net/http"
	"strings"

	"gosocial/internal/auth"
)

// JWTAuthPlugin validates a JWT and injects the authenticated user's ID
// as a header for the upstream service.
type JWTAuthPlugin struct {
	Secret       string
	ExcludePaths []string
}

func NewJWTAuthPlugin(secret string, excludePaths []string) *JWTAuthPlugin {
	return &JWTAuthPlugin{Secret: secret, ExcludePaths: excludePaths}
}

func (j *JWTAuthPlugin) Name() string { return "auth" }

func (j *JWTAuthPlugin) Process(w http.ResponseWriter, r *http.Request, next http.Handler) {
	for _, prefix := range j.ExcludePaths {
		if strings.HasPrefix(r.URL.Path, prefix) {
			next.ServeHTTP(w, r)
			return
		}
	}

	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, `{"error":"missing or invalid Authorization header"}`, http.StatusUnauthorized)
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	claims, err := auth.ValidateToken(token, j.Secret)
	if err != nil {
		http.Error(w, `{"error":"invalid token: `+err.Error()+`"}`, http.StatusUnauthorized)
		return
	}

	r.Header.Set("X-User-ID", claims.Subject)
	r.Header.Set("X-Username", claims.Username)

	next.ServeHTTP(w, r)
}

// LoggerPlugin logs every request the gateway proxies.
type LoggerPlugin struct{}

func NewLoggerPlugin() *LoggerPlugin { return &LoggerPlugin{} }
func (l *LoggerPlugin) Name() string { return "logger" }
func (l *LoggerPlugin) Process(w http.ResponseWriter, r *http.Request, next http.Handler) {
	log.Printf("[gateway] %s %s", r.Method, r.URL.Path)
	next.ServeHTTP(w, r)
}
