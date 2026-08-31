package event

import "testing"

// BenchmarkDecode is the CoverGo S0 baseline anchor for GoFlare: the per-event
// payload decode on the ingest hot path. U1 adds the full pipeline benchmark
// (decode -> fingerprint -> group -> persist).
func BenchmarkDecode(b *testing.B) {
	payload := []byte(`{"level":"error","message":"connection refused",` +
		`"exception":{"values":[{"type":"OperationalError","value":"could not connect to server",` +
		`"stacktrace":{"frames":[{"function":"connect","filename":"db.go","lineno":42},` +
		`{"function":"main","filename":"main.go","lineno":11}]}}]},` +
		`"tags":{"server_name":"web-1","release":"1.4.2"}}`)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Decode(payload); err != nil {
			b.Fatal(err)
		}
	}
}
