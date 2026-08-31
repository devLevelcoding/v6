package events

import "time"

// ReplayAll reconstructs the full FeedState by replaying every event in order.
func ReplayAll(evs []EventRecord) *FeedState {
	return Rebuild(evs)
}

// ReplayUpTo reconstructs FeedState using only events[0..uptoIndex] (inclusive).
func ReplayUpTo(evs []EventRecord, uptoIndex int) *FeedState {
	f := NewFeedState()
	if uptoIndex < 0 {
		return f
	}
	if uptoIndex >= len(evs) {
		uptoIndex = len(evs) - 1
	}
	for _, ev := range evs[:uptoIndex+1] {
		f.Apply(ev)
	}
	return f
}

// ReplayUntil reconstructs FeedState using only events whose OccurredAt is
// on or before cutoff.
func ReplayUntil(evs []EventRecord, cutoff time.Time) *FeedState {
	f := NewFeedState()
	for _, ev := range evs {
		if ev.OccurredAt.After(cutoff) {
			break
		}
		f.Apply(ev)
	}
	return f
}
