# GoGate — Test Plan

## Automated (present)

`cd GoGate && go build ./... && go vet ./... && go test ./...`

- `internal/uid` — 1000 draws are 32-char lowercase hex and unique.
- `internal/route` — `Validate` table (prefix must start `/`, exactly one of
  upstream/subject, upstream must be absolute, rate needs both fields, no
  negative TTL); `MemStore` CRUD + `ErrNotFound` on missing / double delete;
  **`Match`**: wildcard `/` catches all, longest prefix wins
  (`/api/users` over `/api` over `/`), `/apixyz` does *not* match `/api`
  (segment boundary), a `host`-specific route beats a wildcard, `:port` stripped
  from the Host; `Cacheable` (GET/HEAD + TTL only).
- `internal/auth` — HS256 `Verify`: valid token → subject/issuer/raw; rejects
  wrong signature, `alg: none`, `alg: RS256`, expired, not-yet-valid (`nbf`),
  two-part and garbage tokens; leeway boundary (default 30 s vs a 120 s
  override). `Resolve` from the `Authorization` header, from a named cookie,
  missing (`ok=false, err=nil`), present-but-bad (`err != nil`). `Inject` strips
  a client-supplied `X-Auth-Subject` then sets its own, and clears it for an
  unauthenticated request.
- `internal/ratelimit` — a zero `Rate` always allows; burst is spent then `429`
  with a plausible `Retry-After`; tokens refill at `per_second` over an injected
  clock; per-key isolation; idle keys are swept by `gc`.
- `internal/cache` — miss → fill → hit → expire after TTL (injected clock);
  **coalesce**: 20 concurrent misses on one key do exactly 1 fill and
  `Stats.Coalesced ≥ 19`; a 5xx and a `Cache-Control: no-store` response are not
  stored; the size cap resets the store instead of growing; a fill error is
  propagated and not cached.
- `internal/bridge` — `Loopback` dispatches to the registered handler, returns
  `ErrNoHandler` for an unknown subject, honours context cancellation on a slow
  handler. `Handler.ServeSubject` flattens the request into a `Message`
  (method/path/query/body) and writes the `Reply` (status/headers/body); a
  missing handler → `502`, an oversized body → `413`.
- `internal/proxy` — `For` returns the same `*ReverseProxy` per base and rejects
  a relative URL; a real `httptest` upstream is reached with `X-Forwarded-*`
  set and its status/headers/body flow back; a dead upstream → `502`.
- `internal/server` — full chain over an `httptest` upstream + a `Loopback`
  bridge: no route → `404`; proxy + `strip_prefix` (upstream sees `/users/7`
  for a request to `/api/users/7`); `require_auth` → `401` with no token, `401`
  with a bad token, `200` with a valid one *and the upstream sees
  `X-Auth-Subject`*; rate limit `[200 200 429 429]` with `Retry-After`;
  cache `MISS` then `HIT` with the upstream hit exactly once; a bridge route
  returns the handler's `Reply`; control-plane API add (`201`) / invalid
  (`422`) / the added route serves / delete (`204`) / then `404`.

Race detector (`go test -race ./...`) needs a C toolchain — run it in CI. The
coalescing and rate-limit tests are the ones worth `-race`.

## Manual smoke

```bash
go run ./cmd/gogated -jwt-secret dev -upstream http://localhost:3000 -upstream-cache 30s &
curl -s localhost:8090/_gogate/healthz
curl -si localhost:8090/thing | grep -i x-cache   # MISS
curl -si localhost:8090/thing | grep -i x-cache   # HIT
curl -s localhost:8090/_gogate/routes -d '{"path_prefix":"/q","target":{"subject":"demo"},"policy":{"require_auth":true}}'
curl -s -o /dev/null -w '%{http_code}\n' localhost:8090/q/x   # 401
curl -s localhost:8090/_gogate/stats
```

## Automated (to add as phases land)

- **Phase 1**: a route-file change is picked up within the poll interval without
  a restart; the Pub/Sub `Transport` does a real request/reply against the
  emulator; `/metrics` exposes per-route counters.
- **Phase 2**: an RS256 token verifies against a JWKS fixture; `kid` rotation;
  a route with `require_claims: {scope: "admin"}` rejects a token that lacks it.
- **Phase 3**: the breaker opens after the error-rate threshold and half-opens
  after the cooldown; a retried idempotent request succeeds on the second try;
  LB spreads load and ejects an unhealthy upstream.
- **Phase 4**: stale-while-revalidate serves the stale body and refreshes in the
  background; `POST /_gogate/cache/purge` by tag drops the right keys; a 304 is
  synthesised from a matching `If-None-Match`.
- **Phase 5**: a fan-out route merges two upstreams; one upstream failing yields
  the declared partial response, not a 500.
- **Phase 7**: RBAC denies `POST /_gogate/routes` without `gateway:route:write`;
  every route change appends one verifiable audit entry.
