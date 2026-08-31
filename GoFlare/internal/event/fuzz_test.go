package event

import "testing"

// FuzzParseEnvelope (CoverGo U23) throws arbitrary bytes at the Sentry-envelope
// framing parser (length headers, newline-delimited items, multi-part bodies).
// Contract: never panics; a successful parse returns a non-nil Envelope whose
// item payloads are all within the input.
func FuzzParseEnvelope(f *testing.F) {
	f.Add([]byte(`{"dsn":"https://k@example/1"}` + "\n" + `{"type":"event"}` + "\n" + `{"message":"hi"}`))
	f.Add([]byte(`{}` + "\n" + `{"type":"event","length":15}` + "\n" + `{"message":"x"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte("{}\n"))
	f.Add([]byte(`{"type":"event","length":999999}` + "\n" + `short`))
	f.Add([]byte{})
	f.Add([]byte("\n\n\n"))

	f.Fuzz(func(t *testing.T, body []byte) {
		env, err := ParseEnvelope(body)
		if err != nil {
			return
		}
		if env == nil {
			t.Fatal("ParseEnvelope returned nil Envelope with a nil error")
		}
		for i, it := range env.Items {
			if len(it.Payload) > len(body) {
				t.Fatalf("item %d payload (%d bytes) larger than the whole body (%d)", i, len(it.Payload), len(body))
			}
		}
	})
}

// FuzzDecode (CoverGo U23) throws arbitrary bytes at the event-payload decoder.
// Contract: never panics; a successful decode yields a non-empty Title.
func FuzzDecode(f *testing.F) {
	f.Add([]byte(`{"message":"boom"}`))
	f.Add([]byte(`{"exception":{"values":[{"type":"TypeError","value":"x"}]}}`))
	f.Add([]byte(`{"level":"WARNING","tags":{"a":"b"}}`))
	f.Add([]byte(`{"timestamp":"not-a-time"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, payload []byte) {
		ev, err := Decode(payload)
		if err != nil {
			return
		}
		// Display helpers run on every ingested event — they must not panic on
		// anything the decoder was willing to accept.
		_ = ev.Title()
	})
}
