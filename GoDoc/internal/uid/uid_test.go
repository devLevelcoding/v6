package uid

import "testing"

func TestNewIsUniqueAndHex(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := New()
		if len(id) != 32 {
			t.Fatalf("want 32 hex chars, got %d (%q)", len(id), id)
		}
		for _, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Fatalf("non-hex char %q in %q", c, id)
			}
		}
		if seen[id] {
			t.Fatalf("duplicate id %q after %d draws", id, i)
		}
		seen[id] = true
	}
}
