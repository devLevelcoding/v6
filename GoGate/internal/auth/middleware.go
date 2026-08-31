package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type ctxKey int

const claimsKey ctxKey = 0

// Downstream headers GoGate sets from verified claims. Any client-supplied copy
// is stripped first so an upstream can trust them.
const (
	HeaderSubject = "X-Auth-Subject"
	HeaderClaims  = "X-Auth-Claims" // compact JSON of every claim
	HeaderIssuer  = "X-Auth-Issuer"
)

// Resolve extracts a bearer token from the request (Authorization header, or the
// named cookie if cookieName != "") and verifies it. A missing token yields
// (Claims{}, false, nil); a present-but-bad token yields an error. It never
// writes a response — enforcement is the caller's choice per route.
func Resolve(v Verifier, r *http.Request, cookieName string) (Claims, bool, error) {
	tok := bearer(r)
	if tok == "" && cookieName != "" {
		if c, err := r.Cookie(cookieName); err == nil {
			tok = c.Value
		}
	}
	if tok == "" {
		return Claims{}, false, nil
	}
	c, err := v.Verify(tok)
	if err != nil {
		return Claims{}, false, err
	}
	return c, true, nil
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// WithClaims stores verified claims on the request context.
func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, claimsKey, c)
}

// FromContext returns the claims stored by WithClaims, if any.
func FromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}

// Inject rewrites r's auth headers: strip anything the client sent, then set
// GoGate's own from c. Call it just before forwarding upstream.
func Inject(r *http.Request, c Claims, authed bool) {
	r.Header.Del(HeaderSubject)
	r.Header.Del(HeaderClaims)
	r.Header.Del(HeaderIssuer)
	if !authed {
		return
	}
	r.Header.Set(HeaderSubject, c.Subject)
	if c.Issuer != "" {
		r.Header.Set(HeaderIssuer, c.Issuer)
	}
	if len(c.Raw) > 0 {
		if b, err := json.Marshal(c.Raw); err == nil {
			r.Header.Set(HeaderClaims, string(b))
		}
	}
}
