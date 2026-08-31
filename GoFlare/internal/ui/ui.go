// Package ui serves GoFlare's web console: a dependency-free single-page app
// (no build step, no framework) that sits on the /api/0/* dashboard API. It is
// the Phase 8 "web console" in future.md, kept minimal — project list + DSN,
// issue stream, issue detail with stack trace, and triage.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var files embed.FS

// Handler returns an http.Handler that serves the console. Unknown paths fall
// through to index.html so the client-side router owns navigation.
func Handler() http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err) // embedded path is a compile-time constant
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if _, err := fs.Stat(sub, r.URL.Path[1:]); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
