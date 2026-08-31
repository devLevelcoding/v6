// Command go-social-v2 is the main HTTP API: auth, posts, follows, feed,
// messages, GraphQL, feature flags, debug/replay, debug/breaker, and a
// WebSocket notification endpoint.
//
// profilesvc (the internal gRPC service), the graphql-backing service
// (cmd/graphqlbacking), and the gateway (cmd/gateway) are separate
// binaries/containers -- see docker-compose.yml. The GraphQL executor
// talks to graphql-backing over HTTP, and graphql-backing fetches this
// process's live event-sourced state over HTTP too (GET /internal/state,
// internal/statefetch) -- no shared memory between the three.
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"gosocial/internal/env"
	"gosocial/internal/flags"
	"gosocial/internal/graphql"
	"gosocial/internal/notify"
	"gosocial/internal/profileclient"
	"gosocial/internal/ratelimit"
	"gosocial/internal/social"
)

func main() {
	addr := env.Getenv("ADDR", ":8400")
	secret := env.Getenv("JWT_SECRET", "go-social-v2-shared-secret-change-me")
	profilesvcAddr := env.Getenv("PROFILESVC_ADDR", "localhost:9091")
	backingAddr := env.Getenv("GRAPHQL_BACKING_ADDR", "http://localhost:8452")

	store := social.NewSocialStore()
	creds := social.NewCredentialStore()

	postLimiter := ratelimit.NewPostRateLimiter(5, 1) // burst 5, refill 1/sec

	hub := notify.New()
	go hub.Run()
	notifier := notify.NewNotifier(hub)
	wsHandler := notify.NewWSHandler(hub, secret)

	flagStore := flags.NewStore()
	flagStore.Set(flags.Flag{Name: "graphql_api", Enabled: false, RolloutPercent: 0})

	// GraphQL executor talks to the separate graphql-backing service/container.
	executor := graphql.NewExecutor(backingAddr)

	// profilesvc client behind a circuit breaker.
	profileClient, err := profileclient.Dial(profilesvcAddr)
	if err != nil {
		log.Printf("[profileclient] dial warning (continuing without it): %v", err)
		profileClient = nil
	}

	h := &social.Handler{
		Store: store, Creds: creds, Secret: secret,
		PostLimiter: postLimiter, Notifier: notifier,
		Profiles: profileClient, GraphQL: executor, Flags: flagStore,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/register", h.Register)
	mux.HandleFunc("POST /auth/login", h.Login)

	authMW := social.RequireAuth(secret)

	mux.Handle("POST /posts", authMW(postLimiter.Middleware(social.UserIDFromRequest)(http.HandlerFunc(h.CreatePost))))
	mux.Handle("GET /feed", authMW(http.HandlerFunc(h.Feed)))
	mux.Handle("POST /follow/{userId}", authMW(http.HandlerFunc(h.RequestFollowHandler)))
	mux.Handle("POST /follow/{requestId}/accept", authMW(http.HandlerFunc(h.AcceptFollowHandler)))
	mux.Handle("POST /follow/{requestId}/reject", authMW(http.HandlerFunc(h.RejectFollowHandler)))
	mux.Handle("GET /follow/requests", authMW(http.HandlerFunc(h.FollowRequests)))
	mux.Handle("POST /messages/{toUserId}", authMW(http.HandlerFunc(h.SendMessage)))
	mux.Handle("GET /users/{id}/profile", authMW(http.HandlerFunc(h.Profile)))

	// GraphQL access is gated live by the "graphql_api" feature flag.
	graphQLGate := flagStore.GraphQLGateMiddleware("graphql_api", func(r *http.Request) string {
		id, _ := social.UserIDFromRequest(r)
		return id
	})
	mux.Handle("POST /graphql", authMW(graphQLGate(http.HandlerFunc(h.GraphQLHandler))))

	// Debug / admin surfaces -- no auth.
	mux.HandleFunc("GET /debug/breaker", h.BreakerStatus)
	mux.HandleFunc("GET /debug/replay", h.ReplayDebug)
	mux.Handle("/flags/", h.FlagsProxy())

	// Internal, container-to-container only (not routed through the
	// gateway): lets the separate graphql-backing service fetch live
	// state over HTTP instead of sharing process memory.
	mux.HandleFunc("GET /internal/state", h.InternalState)

	mux.Handle("/ws/notifications", wsHandler)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second}
	fmt.Printf("go-social-v2 listening on %s\n", addr)
	fmt.Printf("  profilesvc:      %s\n", profilesvcAddr)
	fmt.Printf("  graphql backing: %s (separate service)\n", backingAddr)
	log.Fatal(srv.ListenAndServe())
}
