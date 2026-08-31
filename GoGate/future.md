# GoGate — Feature Roadmap (V5)

A from-scratch **edge gateway / BFF** in Go: one binary in front of any app that
matches routes, verifies tokens, rate-limits, caches, and bridges HTTP to a
message queue — reusing the platform layer already built in `GoAdmin` for
identity, RBAC, audit and secrets, and sharing the reverse-proxy machinery with
`GoAdmin/gateway`.

This file is the north star. Phase 0 (a compiling, tested walking skeleton) is
in this repo now; everything past it is planned, not built.

---

## 1. Why this, why Go

Three projects on the drive each have a gateway-shaped problem:

- **crm3-micro** runs a **NestJS gateway** (~512 MiB) whose real job is: verify a
  JWT, translate an HTTP call into a Pub/Sub request/reply, wait for the answer,
  write it back. That is `internal/bridge` + `internal/auth` — a few hundred
  lines of Go, not a Node process per instance. The Node event loop caps a
  single instance at a few thousand rps with GC pauses; `net/http` +
  `httputil.ReverseProxy` do 10–20× that with flat tail latency.
- **ShopFloor3D**, **CrmLara**, **SassDesk** each expose an API directly with no
  shared layer for auth, rate limiting or caching — so each reimplements a
  subset, differently.
- **GoAdmin/gateway** is itself a reverse proxy with routing, TLS and
  config-reload. GoGate is the same shape aimed at app traffic rather than the
  admin plane; it should *reuse* that code, not fork it.

Go is the right tool: `net/http` + `httputil.ReverseProxy` + `crypto/*` cover
the whole surface with no dependencies, cheap goroutines make per-connection
work free, and a static binary drops onto any box or scale-to-zero platform.

### What GoGate replaces, concretely

| Today | GoGate |
|---|---|
| NestJS `ClientsModule` + a Pub/Sub client per service | one `bridge.Transport`, one route per subject |
| a JWT guard reimplemented in every service | `auth.HS256` at the edge, identity as `X-Auth-*` headers downstream |
| no rate limiting, or a Redis counter per app | `ratelimit` token bucket, per-subject or per-IP, per route |
| `@nestjs/cache-manager` / none | `cache` read-through TTL + request coalescing |
| 512 MiB Node instance | ~48 MiB static Go binary |

---

## 2. Architecture

```
  client ─► gogated (Go, internal/server)
             │  1 match route      (internal/route: host + longest prefix)
             │  2 verify token     (internal/auth: HS256 now, JWKS later)
             │  3 rate-limit       (internal/ratelimit: token bucket, keyed)
             │  4 cache + coalesce (internal/cache: TTL store + singleflight)
             │  5 dispatch
             ├──────────────► internal/proxy ─► HTTP upstream
             └──────────────► internal/bridge ─► Transport ─► queue subject ─► app
                                                 (Loopback now; Pub/Sub/NATS later)

  control plane: GET/POST/DELETE /_gogate/routes, /_gogate/stats, /_gogate/healthz

  cross-cutting (reuse GoAdmin gateway): TLS, config hot-reload, RBAC on the
  control plane, hash-chained audit of route changes, GoSecrets for the JWT
  secret / broker credentials.
```

The seams that matter: `auth.Verifier`, `bridge.Transport` and `route.Store`
are interfaces; `cache` and `ratelimit` are swappable structs. Each later phase
is a new file behind one of them.

---

## 3. Phases

### Phase 0 — walking skeleton — **in repo**
`route` (model + `Match` + `MemStore`), `auth` (HS256 `Verifier`, token
resolution, header injection), `ratelimit` (keyed token bucket), `cache`
(read-through TTL + coalescing), `bridge` (`Message`/`Reply`, `Transport`,
`Loopback`), `proxy` (per-upstream `ReverseProxy`), `server` (the chain + the
`/_gogate` control-plane API), `gogated`. `go test ./...` green + a live smoke.

### Phase 1 — real config & the broker bridge
- `route.Store` backed by a YAML/JSON file with **hot reload** (fsnotify-free:
  poll + checksum, or reuse `GoAdmin/gateway`'s reloader) and by Postgres.
- `bridge.Transport` for **Google Pub/Sub** and **NATS**: publish to `subject`,
  await a reply on a per-request inbox with a timeout. This is the drop-in for
  crm3-micro's gateway — same subjects (`crm3-<svc>` / `crm3-gateway-<svc>-reply`).
- Structured access logs + a `/metrics` endpoint (Prometheus text, stdlib).

### Phase 2 — identity, for real
- **JWKS**: fetch and cache a provider's keys, verify RS256/ES256/EdDSA, honour
  `kid`, rotate. `auth.HS256` becomes one `Verifier` among several.
- Audience / issuer allow-lists per route; required-scope / required-claim
  checks (`policy.require_claims`).
- Opaque-token **introspection** (RFC 7662) as an alternative `Verifier`.
- mTLS client-cert auth for service-to-service routes.

### Phase 3 — traffic control
- Rate limiting: sliding-window option, cost per route (a heavy endpoint spends
  more tokens), a **shared limiter** over Redis so N instances share a budget,
  `429` bodies with a machine-readable retry hint.
- **Circuit breaker** per upstream (open on an error-rate threshold, half-open
  probe), and **retries** with budget + jittered backoff for idempotent methods.
- Load balancing across multiple upstreams per route: round-robin / least-conn /
  EWMA, active health checks, outlier ejection.
- Request/response size limits, timeouts and header normalisation per route.

### Phase 4 — the cache, properly
- Shared cache backend (Redis / memcached) behind the `cache` interface;
  stale-while-revalidate and stale-if-error; `Vary` support; explicit
  invalidation API (`POST /_gogate/cache/purge` by key or tag); `ETag` /
  `If-None-Match` pass-through and 304 synthesis.
- Negative caching (short TTL on 404/5xx) with a separate budget.

### Phase 5 — BFF composition
- A route can **fan out**: call several upstreams/subjects and merge the
  responses per a small declarative spec (JSONata-ish or a Go plugin), so a
  mobile screen is one request. Partial-failure policy per field.
- Response transforms: field projection, header/body rewrites, redaction of
  claims the client shouldn't see.

### Phase 6 — edge features (borrow from GoFlare's edge)
- TLS termination with ACME (`golang.org/x/crypto/acme/autocert`); HTTP/3.
- A minimal WAF hook (method/path/header/geo rules) — or delegate to GoFlare's
  edge when both are deployed.
- CORS handling per route; compression; request coalescing extended to any
  safe method with an idempotency key.

### Phase 7 — governance (reuse, not rebuild)
- Mount the control plane behind **GoAdmin's gateway**: identity via
  `gobase_session`, RBAC (`gateway:route:write`, `gateway:cache:purge`),
  **hash-chained audit** of every route/policy change, **GoSecrets** for the JWT
  secret and broker credentials, config hot-reload.
- Multi-tenant route namespaces; per-tenant quotas; a read-only status page.

### Phase 8 — web console (Node)
- Route editor, live traffic (rps, p50/p95/p99, error rate per route),
  rate-limit and cache dashboards, a request tracer. SPA behind the gateway like
  gofile / GoObserv.

---

## 4. What to reuse from V5 (don't rebuild)

| Need | Reuse |
|---|---|
| Reverse proxy, host routing, TLS, config hot-reload | `GoAdmin/gateway` |
| RBAC engine `(role, action, resource)` | `GoAdmin/gateway/internal/rbac` |
| Tamper-evident audit log (hash chain) | `GoAdmin/gateway/internal/auditlog` |
| Secrets at rest (JWT secret, broker creds) | `GoAdmin/gateway/internal/secrets` (GoSecrets) |
| Pub/Sub subjects + reply topics | crm3-micro's existing `crm3-<svc>` naming |
| Load-testing the gateway | `GoAdmin/GoLoad` |
| Error capture on 5xx it proxies | `GoFlare` edge (cross-link an issue → "rate-limit this route") |
| Uptime of the upstreams it fronts | `GoUptime` incidents |

## 5. Explicitly out of scope

- A service mesh / sidecar model — GoGate is a centralized edge, not a per-pod proxy.
- gRPC transcoding beyond simple passthrough (until an app needs it).
- Being an API-design tool (schemas, mocking, portals) — it runs traffic.
- Its own identity provider — it *verifies* tokens, it doesn't issue them
  (auth-svc / GoAdmin do).

## 6. Status

- [x] **Phase 0 — walking skeleton** — `gogated` builds, `go test ./...` green;
  match → verify → rate-limit → cache+coalesce → proxy|bridge runs end to end;
  in-memory route table, loopback bridge, HS256 only.
- [ ] Phase 1 — file/Postgres route store + hot reload; Pub/Sub & NATS bridge; metrics
- [ ] Phase 2 — JWKS / RS256 / introspection / mTLS; per-route claim checks
- [ ] Phase 3 — circuit breaker, retries, LB, shared rate limiter
- [ ] Phase 4 — shared cache, SWR, purge API, conditional requests
- [ ] Phase 5 — BFF fan-out & response composition
- [ ] Phase 6 — TLS/ACME, WAF hook, CORS, HTTP/3
- [ ] Phase 7 — governance via GoAdmin gateway
- [ ] Phase 8 — web console
