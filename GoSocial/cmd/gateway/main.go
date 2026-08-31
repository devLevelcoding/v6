// Command gateway runs an API gateway in front of the main go-social-v2
// HTTP API, with an auth plugin that verifies JWTs before requests reach
// the upstream.
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"gosocial/internal/env"
	"gosocial/internal/gateway/plugin"
	"gosocial/internal/gateway/plugins"
	"gosocial/internal/gateway/router"
)

func main() {
	addr := env.Getenv("GATEWAY_ADDR", ":8410")
	upstream := env.Getenv("SOCIAL_API_UPSTREAM", "http://localhost:8400")
	secret := env.Getenv("JWT_SECRET", "go-social-v2-shared-secret-change-me")

	registry := make(plugin.Registry)
	registry.Register(plugins.NewLoggerPlugin())
	registry.Register(plugins.NewJWTAuthPlugin(secret, []string{
		"/api/social/auth/", // register/login must be reachable unauthenticated
		"/api/social/debug/",
		"/api/social/flags/",
		"/api/social/ws/",
	}))

	routes := []router.RouteConfig{
		{
			Prefix:      "/api/social/",
			Upstream:    upstream,
			Plugins:     []string{"logger", "auth"},
			StripPrefix: true,
		},
	}

	r, err := router.New(routes, registry)
	if err != nil {
		log.Fatalf("build router: %v", err)
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	fmt.Printf("go-social-v2 gateway listening on %s -> %s\n", addr, upstream)
	fmt.Println("Route: /api/social/* -> upstream, plugins=[logger auth], auth excludes /auth/, /debug/, /flags/, /ws/")
	log.Fatal(srv.ListenAndServe())
}
