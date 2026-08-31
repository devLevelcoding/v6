package group

// Fingerprinting: reducing an event to the ordered components that decide which
// issue it groups into, then hashing them into a stable key.

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/levelcodingdev/goflare/internal/event"
)

// defaultToken is the placeholder an SDK puts in a custom fingerprint to mean
// "also mix in whatever GoFlare would have grouped by".
const defaultToken = "{{ default }}"

var (
	wsRE  = regexp.MustCompile(`\s+`)
	numRE = regexp.MustCompile(`\d+`)
	hexRE = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
)

// Fingerprint returns the ordered grouping components for an event. Callers
// hash it with Hash.
func Fingerprint(e event.Event) []string {
	if len(e.Fingerprint) > 0 && !isJustDefault(e.Fingerprint) {
		var out []string
		for _, part := range e.Fingerprint {
			if strings.TrimSpace(part) == defaultToken {
				out = append(out, defaultComponents(e)...)
				continue
			}
			out = append(out, part)
		}
		return out
	}
	return defaultComponents(e)
}

// Hash reduces fingerprint components to a stable hex digest.
func Hash(components []string) string {
	sum := sha1.Sum([]byte(strings.Join(components, "\n")))
	return hex.EncodeToString(sum[:])
}

func isJustDefault(fp []string) bool {
	return len(fp) == 1 && strings.TrimSpace(fp[0]) == defaultToken
}

func defaultComponents(e event.Event) []string {
	if len(e.Exceptions) > 0 {
		x := e.Exceptions[len(e.Exceptions)-1]
		comps := []string{"exception"}
		if x.Type != "" {
			comps = append(comps, x.Type)
		}
		if sig := stackSignature(x.Frames); len(sig) > 0 {
			return append(comps, sig...)
		}
		// No frames — fall back to the (normalized) message so two identical
		// throws still group, but a varying id in the message does not split.
		if x.Value != "" {
			return append(comps, normalizeText(x.Value))
		}
		return comps
	}
	if e.Message != "" {
		return []string{"message", normalizeText(e.Message)}
	}
	if e.Level != "" {
		return []string{"level", string(e.Level)}
	}
	return []string{"platform", orUnknown(e.Platform)}
}

// stackSignature builds a frame-by-frame signature, preferring in-app frames.
func stackSignature(frames []event.Frame) []string {
	if len(frames) == 0 {
		return nil
	}
	pick := make([]event.Frame, 0, len(frames))
	for _, f := range frames {
		if f.InApp {
			pick = append(pick, f)
		}
	}
	if len(pick) == 0 {
		pick = frames
	}
	sig := make([]string, 0, len(pick))
	for _, f := range pick {
		loc := f.Module
		if loc == "" {
			loc = f.Filename
		}
		sig = append(sig, loc+":"+f.Function)
	}
	return sig
}

// normalizeText collapses whitespace and masks embedded numbers and hex ids so
// "user 4171 not found" and "user 5522 not found" group together.
func normalizeText(s string) string {
	s = wsRE.ReplaceAllString(strings.TrimSpace(s), " ")
	s = hexRE.ReplaceAllString(s, "<hex>")
	s = numRE.ReplaceAllString(s, "<num>")
	return s
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
