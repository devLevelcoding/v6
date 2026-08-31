package event

// Resolving the DSN public key an ingest request presents — from the
// X-Sentry-Auth / Authorization header, the ?sentry_key= query parameter, or a
// full DSN string an envelope header may carry.

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Auth is the credential an ingest request carries.
type Auth struct {
	PublicKey string
	Client    string
	Version   string
}

// ParseAuth extracts the DSN public key from an ingest request: the
// X-Sentry-Auth header, an Authorization: Sentry ... header, or the
// ?sentry_key= query parameter, in that order.
func ParseAuth(r *http.Request) (Auth, error) {
	for _, h := range []string{r.Header.Get("X-Sentry-Auth"), r.Header.Get("Authorization")} {
		if a, ok := parseSentryAuthHeader(h); ok {
			return a, nil
		}
	}
	if k := r.URL.Query().Get("sentry_key"); k != "" {
		return Auth{PublicKey: k}, nil
	}
	return Auth{}, errors.New("event: no sentry_key in request")
}

// AuthFromDSN parses a full DSN string (as an envelope header may carry) and
// returns its public key and project id.
func AuthFromDSN(dsn string) (publicKey, projectID string, err error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", "", err
	}
	if u.User == nil {
		return "", "", errors.New("event: DSN has no public key")
	}
	projectID = strings.Trim(u.Path, "/")
	return u.User.Username(), projectID, nil
}

func parseSentryAuthHeader(h string) (Auth, bool) {
	h = strings.TrimSpace(h)
	if !strings.HasPrefix(strings.ToLower(h), "sentry ") {
		return Auth{}, false
	}
	var a Auth
	for _, part := range strings.Split(h[len("sentry "):], ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "sentry_key":
			a.PublicKey = strings.TrimSpace(v)
		case "sentry_client":
			a.Client = strings.TrimSpace(v)
		case "sentry_version":
			a.Version = strings.TrimSpace(v)
		}
	}
	return a, a.PublicKey != ""
}
