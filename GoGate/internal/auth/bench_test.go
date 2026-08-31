package auth

import "testing"

// BenchmarkHS256Verify is the CoverGo S0 baseline anchor: the per-request token
// check on GoGate's hot path. U1 expands this to the full middleware chain.
func BenchmarkHS256Verify(b *testing.B) {
	v := HS256{Secret: secret, Now: fixedNow(1000)}
	tok := signSeed(
		map[string]any{"alg": "HS256", "typ": "JWT"},
		map[string]any{"sub": "u42", "iss": "acme", "exp": float64(1 << 40)},
	)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := v.Verify(tok); err != nil {
			b.Fatal(err)
		}
	}
}
