package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// signSeed builds a compact JWS for the fuzz seed corpus (no *testing.T, unlike
// the test-package `mint` helper).
func signSeed(header, claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(header) + "." + enc(claims)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// FuzzHS256Verify (CoverGo U23) drives adversarial token strings through the
// verifier. The contract: Verify never panics, and a nil error means the token
// really is a valid, unexpired, correctly-signed HS256 JWT.
func FuzzHS256Verify(f *testing.F) {
	v := HS256{Secret: secret, Now: fixedNow(1000)}

	f.Add("")
	f.Add("a.b.c")
	f.Add("not-a-token")
	f.Add("aaa.bbb")
	f.Add("...")
	f.Add(signSeed(map[string]any{"alg": "HS256", "typ": "JWT"}, map[string]any{"sub": "u", "exp": float64(9999)}))
	f.Add(signSeed(map[string]any{"alg": "none"}, map[string]any{"sub": "u"}))
	f.Add(signSeed(map[string]any{"alg": "HS256"}, map[string]any{"exp": "not-a-number"}))

	f.Fuzz(func(t *testing.T, token string) {
		c, err := v.Verify(token)
		if err != nil {
			return
		}
		if !c.Expiry.IsZero() && c.Expiry.Add(v.Leeway).Before(time.Unix(1000, 0)) {
			t.Fatalf("Verify accepted an expired token: exp=%v", c.Expiry)
		}
		if c.Raw == nil {
			t.Fatal("Verify returned nil Claims.Raw on success")
		}
	})
}
