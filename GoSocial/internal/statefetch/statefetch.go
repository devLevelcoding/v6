// Package statefetch fetches the current FeedState projection from the
// main API's /internal/state endpoint over HTTP, so the graphql-backing
// service can run as a separate process without sharing memory.
package statefetch

import (
	"encoding/json"
	"net/http"

	"gosocial/internal/events"
)

type Fetcher struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string) *Fetcher {
	return &Fetcher{baseURL: baseURL, client: &http.Client{}}
}

// State fetches the current FeedState from the main API. A fetch/decode
// failure (main API unreachable) degrades to an empty state rather than
// panicking.
func (f *Fetcher) State() *events.FeedState {
	resp, err := f.client.Get(f.baseURL + "/internal/state")
	if err != nil {
		return events.NewFeedState()
	}
	defer resp.Body.Close()

	var state events.FeedState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return events.NewFeedState()
	}
	return &state
}
