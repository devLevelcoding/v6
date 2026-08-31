# go-social

An Instagram-style social backend split into four independent
processes/containers: a JWT-secured HTTP API with an event-sourced feed,
an API gateway, an internal profile service (gRPC), and a GraphQL
read-only backing service.

| Piece | Notes |
|---|---|
| JWT auth | `internal/auth` -- HS256 tokens via `golang-jwt/v5`, passwords hashed with `bcrypt` |
| API gateway | `internal/gateway/router` + `plugin`, with a JWT-auth plugin gating access |
| Rate limiting on posting | `internal/ratelimit` -- one token bucket per user, wrapping `POST /posts` |
| Event-sourced feed | `internal/events` -- an append-only store + projector; `/debug/replay?upto=N` reconstructs state as of a given event |
| Realtime notifications | `internal/notify` -- a WebSocket hub, authenticated with a JWT |
| Internal profile service | `internal/profilesvc` -- a gRPC service (own proto), called from the main API |
| Resilience | `internal/breaker` -- a generic circuit breaker wrapping the profilesvc gRPC client |
| GraphQL read API | `internal/graphql` (parser + executor) against a separate `graphql-backing` service |
| Feature-gated rollout | `internal/flags` -- gates `POST /graphql` live, no restart |

## Architecture: four separate processes

```
client -> gateway (:8410)  --/api/social/*-->  main API (:8400)
                                                    |   |
                                              gRPC  |   | HTTP
                                                    v   v
                                            profilesvc   graphql-backing (:8452)
                                              (:9091)         |
                                                               | HTTP
                                                               v
                                                       main API's own
                                                       GET /internal/state
```

Four separate binaries/containers (`Dockerfile`, `Dockerfile.gateway`,
`Dockerfile.profilesvc`, `Dockerfile.graphqlbacking`), each talking to
the others over the network -- no shared Go memory anywhere.
`graphql-backing` doesn't share the main API's in-memory event store: it
fetches the live `FeedState` over HTTP from the main API's own
`GET /internal/state` (`internal/statefetch`) on every request.

```
docker compose up go-social-v2-profilesvc go-social-v2-graphql-backing go-social-v2 go-social-v2-gateway

# or natively, in 4 terminals from this directory:
go run ./cmd/profilesvc
go run ./cmd/graphqlbacking
go run .
go run ./cmd/gateway
```

## A known gotcha: breaker + gRPC error model

The circuit breaker treats any non-nil error from the wrapped call as a
failure -- including a legitimate `NotFound` gRPC status for a profile
that genuinely doesn't exist (e.g. after profilesvc restarts and loses
its in-memory data). A burst of real "not found" lookups against a
perfectly healthy profilesvc can therefore keep the breaker OPEN
indefinitely. This is a real characteristic of composing a generic
circuit breaker with gRPC's richer error model, not a bug -- a breaker
wrapping a gRPC client should generally only count transport-level
failures (`Unavailable`, `DeadlineExceeded`, etc.) against it, not
application-level "not found" responses.

## Scope notes

- `profilesvc` is in-memory (no real database) -- restarting it loses its data.
- `/internal/state` is deliberately not routed through the gateway; it's
  a container-to-container call.
- The gateway's JWT-auth plugin and the main API's own `RequireAuth` both
  validate the same token independently (defense in depth) -- a request
  reaching the main API directly (bypassing the gateway) is still
  protected.
- `golang.org/x/crypto`'s `go.mod` requires `go >= 1.25.0`, hence
  `golang:1.25-alpine` in all four Dockerfiles.
