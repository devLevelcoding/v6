package ratelimit

import (
	"net/http"
	"sync"
)

// PostRateLimiter enforces a per-user token bucket on the create-post
// endpoint. Each user gets their own TokenBucket, created lazily on
// their first post.
type PostRateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*TokenBucket
	capacity float64
	refill   float64
}

// NewPostRateLimiter creates a limiter where each user may burst up to
// capacity posts and then refill at refill tokens/sec.
func NewPostRateLimiter(capacity, refill float64) *PostRateLimiter {
	return &PostRateLimiter{
		buckets:  make(map[string]*TokenBucket),
		capacity: capacity,
		refill:   refill,
	}
}

func (l *PostRateLimiter) bucketFor(userID string) *TokenBucket {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[userID]
	if !ok {
		b = NewTokenBucket(l.capacity, l.refill)
		l.buckets[userID] = b
	}
	return b
}

// Allow reports whether userID may create a post right now, consuming a
// token from their bucket if so.
func (l *PostRateLimiter) Allow(userID string) bool {
	return l.bucketFor(userID).Allow()
}

// Tokens returns the current token count for userID (for diagnostics /
// demo output).
func (l *PostRateLimiter) Tokens(userID string) float64 {
	return l.bucketFor(userID).Tokens()
}

// Middleware wraps a create-post handler, 429-ing when the caller's
// bucket is empty. getUserID extracts the authenticated user ID from the
// request (set by the JWT middleware upstream).
func (l *PostRateLimiter) Middleware(getUserID func(*http.Request) (string, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := getUserID(r)
			if !ok {
				http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
				return
			}
			if !l.Allow(userID) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded on posting, slow down"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
