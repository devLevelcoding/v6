# GoGate

A from-scratch **edge gateway / BFF** in Go — one static binary in front of any
app that does route matching, **JWT verification**, **token-bucket rate
limiting**, a **coalescing TTL response cache**, and an **HTTP↔queue bridge**
(turn an HTTP request into a request/reply over a message subject).

It exists to replace hand-rolled gateways. crm3-micro's NestJS gateway is 512
MiB of Node whose whole job is "verify a JWT, translate HTTP into a Pub/Sub
request/reply, await the answer" — that's `internal/bridge` plus
`internal/auth`, ~40 KiB of Go. ShopFloor3D, CrmLara and SassDesk each have a
bare API front that wants the same rate-limit + cache + auth layer.

Full plan and rationale: [`future.md`](future.md).

## Status: Phase 0 — walking skeleton

`gogated` compiles, `go test ./...` is green, and the chain runs end to end:
**match → verify → rate-limit → cache(+coalesce) → proxy | bridge**, plus a
control-plane API for routes. Everything is in-memory: the route table doesn't
survive a restart, the bridge is an in-process `Loopback` (subjects → Go funcs),
and the cache is per-instance. HS256 only, a config file or DB for routes, a
real broker Transport (Pub/Sub / NATS), a shared cache and OpenTelemetry are
later phases — see `future.md` §3.

Zero external dependencies — standard library only.

## Layout

| Path | Role |
|---|---|
| `cmd/gogated` | server entrypoint, flags, graceful shutdown |
| `internal/route` | the routing table: `Route` (host, prefix, target, policy) + `Match` + in-memory `Store` |
| `internal/auth` | HS256 JWT `Verifier`, token resolution (header / cookie), identity header injection |
| `internal/ratelimit` | keyed token-bucket limiter, lazy refill, idle-key GC |
| `internal/cache` | read-through TTL response cache **with request coalescing** (a stampede is one upstream call) |
| `internal/bridge` | HTTP↔queue translation: `Message`/`Reply`, a `Transport` interface, an in-process `Loopback` |
| `internal/proxy` | one `httputil.ReverseProxy` per upstream, X-Forwarded, 502 on failure |
| `internal/server` | the policy chain + the `/_gogate` control-plane API |
| `internal/uid` | random ids over `crypto/rand` |

## Run

```bash
cd GoGate
go test ./...

# proxy everything to one app, cache GETs for 30s:
go run ./cmd/gogated -upstream http://localhost:3000 -upstream-cache 30s

# with JWT verification and a config file of routes:
go run ./cmd/gogated -jwt-secret "$JWT_SECRET" -config routes.json
```

Flags (each also an env var — `GOGATE_ADDR`, `GOGATE_JWT_SECRET`,
`GOGATE_JWT_COOKIE`, `GOGATE_CONFIG`, `GOGATE_UPSTREAM`, `GOGATE_UPSTREAM_AUTH`,
`GOGATE_UPSTREAM_CACHE`, `GOGATE_CACHE_ENTRIES`):

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8090` | listen address |
| `-jwt-secret` | _(none)_ | HS256 secret; empty = no verification, `require_auth` routes 401 |
| `-jwt-cookie` | _(none)_ | also read the token from this cookie |
| `-config` | _(none)_ | JSON file: an array of `Route` objects to load at boot |
| `-upstream` | _(none)_ | shorthand: proxy `/` to this URL |
| `-upstream-auth` | `false` | require a valid token for the `-upstream` route |
| `-upstream-cache` | `0` | cache GET/HEAD on the `-upstream` route this long |

## Try it

```bash
curl localhost:8090/_gogate/healthz
curl localhost:8090/_gogate/stats          # routes, cache hits/misses/coalesced, live rate-limit keys

# add a route at runtime
curl -s localhost:8090/_gogate/routes -d '{
  "host": "api.example.com",
  "path_prefix": "/v1",
  "strip_prefix": true,
  "target": { "upstream": "http://users-svc:8080" },
  "policy": {
    "require_auth": true,
    "rate_limit": { "per_second": 50, "burst": 100 },
    "cache_ttl": 15000000000
  }
}'

# a bridge route — HTTP in, request/reply over a subject
curl -s localhost:8090/_gogate/routes -d '{
  "path_prefix": "/crm",
  "target": { "subject": "crm3-crm" }
}'

curl -s localhost:8090/_gogate/routes
curl -s -X DELETE localhost:8090/_gogate/routes/<id>
```

### Control-plane API (under `/_gogate`)

| Method + path | Purpose |
|---|---|
| `GET /_gogate/healthz`, `GET /_gogate/version` | liveness, build version |
| `GET /_gogate/stats` | route count, cache stats, rate-limit key count |
| `GET /_gogate/routes` | the table, most-specific first |
| `POST /_gogate/routes` | add a route (`422` on an invalid one) |
| `GET /_gogate/routes/{id}` · `DELETE /_gogate/routes/{id}` | one route |

### The request chain

1. **Match** — longest `path_prefix` wins; a `host`-specific route beats a
   wildcard; `/apixyz` does **not** match prefix `/api` (segment boundary).
2. **Verify** — a bearer/cookie token is checked (HS256, `exp`/`nbf` with 30 s
   leeway, `alg: none`/`RS256` rejected). A present-but-invalid token is always
   `401`; a missing one is only fatal on `require_auth`.
3. **Rate-limit** — keyed by the authenticated subject, else client IP; per-route
   `per_second` + `burst`; `429` + `Retry-After` when the bucket is empty.
4. **Cache + coalesce** — for a cacheable GET/HEAD, one goroutine fills while the
   rest wait on it (`X-Cache: HIT` / `MISS`). `no-store` / `private` and non-2xx
   are never cached; the key varies by subject so responses don't leak between
   users.
5. **Dispatch** — reverse-proxy to the `upstream`, or bridge to the `subject`.
   Verified identity reaches the app as `X-Auth-Subject` / `X-Auth-Issuer` /
   `X-Auth-Claims` (client-supplied copies are stripped first).
