// Package auth verifies bearer tokens at the edge and hands the downstream
// service a trustworthy identity. Phase 0 speaks HS256 JWT with the standard
// registered claims; RS256/EdDSA and a JWKS fetcher are a later phase (see
// ../../future.md §3). Verifier is an interface so that swap is a new file, not
// a rewrite.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrUnauthenticated is returned for any token that does not verify.
var ErrUnauthenticated = errors.New("auth: token did not verify")

// Claims is the identity a verified token carries.
type Claims struct {
	Subject string         `json:"sub"`
	Issuer  string         `json:"iss"`
	Expiry  time.Time      `json:"-"`
	Raw     map[string]any `json:"-"` // every claim, for downstream propagation
}

// Verifier turns a token string into Claims.
type Verifier interface {
	Verify(token string) (Claims, error)
}

// HS256 verifies HMAC-SHA256 JWTs against a shared secret.
type HS256 struct {
	Secret []byte
	Leeway time.Duration // clock-skew tolerance for exp/nbf; default 30s
	Now    func() time.Time
}

// Verify parses and checks a compact JWS: the header must be alg=HS256, the
// signature must match, and exp/nbf (when present) must hold within Leeway.
func (h HS256) Verify(token string) (Claims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("%w: not a three-part JWT", ErrUnauthenticated)
	}
	hdrBytes, err := b64.DecodeString(parts[0])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: bad header encoding", ErrUnauthenticated)
	}
	var hdr struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return Claims{}, fmt.Errorf("%w: bad header JSON", ErrUnauthenticated)
	}
	if hdr.Alg != "HS256" {
		// Reject "none" and any asymmetric alg outright — the classic JWT bug.
		return Claims{}, fmt.Errorf("%w: unsupported alg %q", ErrUnauthenticated, hdr.Alg)
	}

	mac := hmac.New(sha256.New, h.Secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want := mac.Sum(nil)
	got, err := b64.DecodeString(parts[2])
	if err != nil || subtle.ConstantTimeCompare(want, got) != 1 {
		return Claims{}, fmt.Errorf("%w: signature mismatch", ErrUnauthenticated)
	}

	payload, err := b64.DecodeString(parts[1])
	if err != nil {
		return Claims{}, fmt.Errorf("%w: bad payload encoding", ErrUnauthenticated)
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return Claims{}, fmt.Errorf("%w: bad payload JSON", ErrUnauthenticated)
	}

	now := h.now()
	leeway := h.Leeway
	if leeway <= 0 {
		leeway = 30 * time.Second
	}
	c := Claims{Raw: raw}
	c.Subject, _ = raw["sub"].(string)
	c.Issuer, _ = raw["iss"].(string)
	if exp, ok := unixClaim(raw["exp"]); ok {
		c.Expiry = exp
		if now.After(exp.Add(leeway)) {
			return Claims{}, fmt.Errorf("%w: token expired", ErrUnauthenticated)
		}
	}
	if nbf, ok := unixClaim(raw["nbf"]); ok && now.Add(leeway).Before(nbf) {
		return Claims{}, fmt.Errorf("%w: token not yet valid", ErrUnauthenticated)
	}
	return c, nil
}

func (h HS256) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

var b64 = base64.RawURLEncoding

func unixClaim(v any) (time.Time, bool) {
	switch n := v.(type) {
	case float64:
		return time.Unix(int64(n), 0), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return time.Unix(i, 0), true
		}
	}
	return time.Time{}, false
}
