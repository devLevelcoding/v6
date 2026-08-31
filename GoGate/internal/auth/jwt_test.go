package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var secret = []byte("test-secret")

func mint(t *testing.T, header, claims map[string]any, signWith []byte) string {
	t.Helper()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(header) + "." + enc(claims)
	mac := hmac.New(sha256.New, signWith)
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func h256() map[string]any { return map[string]any{"alg": "HS256", "typ": "JWT"} }

func fixedNow(ts int64) func() time.Time { return func() time.Time { return time.Unix(ts, 0) } }

func TestVerifyValid(t *testing.T) {
	v := HS256{Secret: secret, Now: fixedNow(1000)}
	tok := mint(t, h256(), map[string]any{"sub": "u42", "iss": "acme", "exp": float64(2000)}, secret)
	c, err := v.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "u42" || c.Issuer != "acme" || c.Raw["sub"] != "u42" {
		t.Fatalf("claims = %+v", c)
	}
}

func TestVerifyRejects(t *testing.T) {
	v := HS256{Secret: secret, Now: fixedNow(1000)}
	cases := map[string]string{
		"wrong sig":     mint(t, h256(), map[string]any{"sub": "u"}, []byte("other-secret")),
		"alg none":      mint(t, map[string]any{"alg": "none"}, map[string]any{"sub": "u"}, secret),
		"alg RS256":     mint(t, map[string]any{"alg": "RS256"}, map[string]any{"sub": "u"}, secret),
		"expired":       mint(t, h256(), map[string]any{"sub": "u", "exp": float64(900)}, secret),
		"not yet valid": mint(t, h256(), map[string]any{"sub": "u", "nbf": float64(1100)}, secret),
		"two parts":     "aaa.bbb",
		"garbage":       "not-a-token",
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Verify(tok); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("Verify(%s) = %v, want ErrUnauthenticated", name, err)
			}
		})
	}
}

func TestVerifyLeeway(t *testing.T) {
	// exp 960, now 1000: default 30s leeway → 1000 > 990, expired;
	// 120s leeway → 1000 < 1080, still accepted.
	tok := mint(t, h256(), map[string]any{"sub": "u", "exp": float64(960)}, secret)
	if _, err := (HS256{Secret: secret, Now: fixedNow(1000)}).Verify(tok); err == nil {
		t.Fatal("expected expiry with default leeway")
	}
	if _, err := (HS256{Secret: secret, Leeway: 120 * time.Second, Now: fixedNow(1000)}).Verify(tok); err != nil {
		t.Fatalf("120s leeway should accept: %v", err)
	}
}

func TestResolveAndInject(t *testing.T) {
	v := HS256{Secret: secret, Now: fixedNow(1000)}
	good := mint(t, h256(), map[string]any{"sub": "u7", "exp": float64(9999)}, secret)

	// header
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+good)
	c, ok, err := Resolve(v, r, "")
	if err != nil || !ok || c.Subject != "u7" {
		t.Fatalf("Resolve(header) = %+v %v %v", c, ok, err)
	}

	// cookie
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(&http.Cookie{Name: "sess", Value: good})
	if _, ok, _ := Resolve(v, r2, "sess"); !ok {
		t.Fatal("Resolve(cookie) should succeed")
	}

	// missing
	if _, ok, err := Resolve(v, httptest.NewRequest("GET", "/", nil), ""); ok || err != nil {
		t.Fatalf("Resolve(missing) = ok %v err %v", ok, err)
	}

	// bad token surfaces an error
	rb := httptest.NewRequest("GET", "/", nil)
	rb.Header.Set("Authorization", "Bearer aaa.bbb.ccc")
	if _, _, err := Resolve(v, rb, ""); err == nil {
		t.Fatal("Resolve(bad) should error")
	}

	// Inject strips client-set headers then sets its own
	fwd := httptest.NewRequest("GET", "/", nil)
	fwd.Header.Set(HeaderSubject, "spoofed")
	Inject(fwd, c, true)
	if fwd.Header.Get(HeaderSubject) != "u7" {
		t.Fatalf("Inject subject = %q", fwd.Header.Get(HeaderSubject))
	}
	Inject(fwd, Claims{}, false)
	if fwd.Header.Get(HeaderSubject) != "" {
		t.Fatal("Inject(unauth) should clear the subject header")
	}
}
