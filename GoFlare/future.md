# GoFlare — Feature Roadmap (V5)

A from-scratch **error-tracking platform fused with an edge network** in Go:
Sentry's job (SDK ingest → grouping → alerting → releases → performance) sitting
behind Cloudflare's position (a reverse proxy, WAF, cache and DNS your traffic
already flows through) — so the platform can see and capture failures without an
SDK, and the SDK-reported failures land in the same place.

This file is the north star. Phase 0 (a compiling, tested walking skeleton) is
in this repo now; everything past it is planned, not built.

---

## 1. Why fuse the two

Sentry and Cloudflare are complementary halves of "what happened to a request":

- **Sentry** knows the *inside* — the exception, the stack, the release, the
  user, the span timings — but only for apps that installed an SDK, and only
  for errors the SDK's hooks caught.
- **Cloudflare** sits *in the path* — it sees every request, every upstream
  5xx, every TLS failure, every timeout, every blocked attack — but has no idea
  what the app was trying to do or why it broke.

Put the error platform *at* the edge and each half fills the other's gap:

- The edge auto-captures upstream 5xx, connection failures, timeouts and WAF
  blocks as events — **instrumentation-free error tracking** for any app you
  proxy, including ones in languages with no good SDK.
- The SDK enriches those same issues with the stack trace and context when it
  *is* installed — the edge event and the SDK event group together by route.
- One identity for a request end to end: the edge assigns a trace id, injects
  it downstream, and the SDK reports against it. Waterfall from CDN cache
  through WAF through app through DB, in one view.
- The WAF and rate limiter become *product features of the observability tool*:
  "this issue is a spike of 500s — one click to rate-limit that route at the
  edge while you fix it."

Go is the right tool for both halves: the edge is a proxy (net/http,
httputil.ReverseProxy, crypto/tls, cheap goroutines per connection) and the
platform is ingest + fan-out + storage (the same strengths GoUptime and GoObserv
lean on). One binary, two listeners.

### Relationship to the other V5 projects

- **GoObserv** aggregates *external* tools (it has a Sentry *adapter*). GoFlare
  *is* the Sentry. GoObserv can add a GoFlare adapter and treat it as a source.
- **GoUptime** watches endpoints from outside; GoFlare watches them from in the
  path and from inside the app. Shared incident vocabulary; a GoUptime outage
  and a GoFlare error spike for the same service should cross-link.
- **GoAdmin gateway** is itself a reverse proxy — GoFlare's edge is the same
  shape and should reuse its routing / TLS / config-reload machinery rather
  than reinvent it.

---

## 2. Architecture

```
                 ┌──────────────────────────────────────────────┐
   internet ───► │  Edge (Go, internal/edge)                    │
                 │  host routing · WAF · rate limit · cache ·   │
                 │  TLS · captures 5xx/timeouts/blocks as events │
                 └──────┬───────────────────────────┬───────────┘
                proxied  │                            │ synthetic events
                         ▼                            │
                 ┌───────────────┐                    │
                 │  your app     │  SDK events        │
                 │  (+ Sentry SDK)├───────────────────┤
                 └───────────────┘                    ▼
                                    ┌────────────────────────────────┐
                                    │  Ingest (Go, internal/ingest)  │
                                    │  envelope/store · DSN auth ·    │
                                    │  gzip · rate limit · quotas     │
                                    └───────────────┬────────────────┘
                                                    ▼
                                    ┌────────────────────────────────┐
                                    │  Grouping (Go, internal/group) │
                                    │  fingerprint → issue upsert →   │
                                    │  regression → event sample      │
                                    │  → Postgres + object store      │
                                    └───────┬────────────────┬───────┘
                                            ▼                ▼
                              ┌───────────────────┐  ┌─────────────────────┐
                              │  Dashboard API    │  │  Alerting (Go)      │
                              │  (internal/api)   │  │  rules → notify     │
                              └───────────────────┘  └─────────────────────┘

  cross-cutting (reuse GoAdmin gateway): identity, RBAC, hash-chained audit,
  GoSecrets for notification + TLS credentials, config hot-reload.
```

Core principle: **a request has one story, told from two vantage points.** The
edge sees the request; the SDK sees the code; grouping is where the two meet.

---

## 3. Phased roadmap

### Phase 0 — walking skeleton ✅ (in this repo)

A compiling, tested `goflared` that wires the real seams with in-memory stores.

- `cmd/goflared` — core listener (ingest + API) + optional edge listener;
  flags/env; graceful shutdown; `-seed-project` logs a working DSN.
- `internal/project` — `Project` + DSN `Key`s, `Store` interface + `MemStore`,
  `Authenticate(projectID, publicKey)`, DSN rendering.
- `internal/event` — a small, tolerant subset of Sentry's event schema;
  `Decode` copes with exception-as-`{values}`/array/object, timestamp as
  epoch/ISO, message as string/logentry, tags as object/pairs, level aliases;
  `ParseEnvelope` handles length-framed and newline-framed items; `ParseAuth`
  reads the DSN key from header or query; `Title` / `Culprit` rules.
- `internal/group` — `Fingerprint` (custom + `{{ default }}`, exception stack
  signature preferring in-app frames, number/hex-masked message fallback),
  `Store.Ingest` → `(Issue, Outcome{new|regression|recurring})`, per-issue
  bounded event sample, `List`/`SetStatus`/`Events`/`LatestEvent`.
- `internal/ingest` — `POST /api/{id}/envelope/` and `/store/`, gzip/deflate,
  DSN auth against the project store, 401/404 as appropriate.
- `internal/api` — projects (list/create/get, DSN in the view), issues
  (list with status/query filter, get, PUT status), events (list, latest).
- `internal/edge` — host-routed `httputil.ReverseProxy`; `ModifyResponse`
  captures upstream ≥500, `ErrorHandler` captures connection failures — both as
  synthetic events fingerprinted by route, fed to the same `group.Store`.

Run: `cd GoFlare && go test ./... && go run ./cmd/goflared -seed-project demo`

### Phase 1 — durable storage

- Postgres for projects, issues, issue-status history (same instance/pattern as
  GoAdmin 2.0's `goadmin` schema — one connection, `goflare` schema).
  Migrations.
- Events are the high-volume table: raw event JSON to an object store
  (`S3Store`, reuse `GoSnow`/`gofile` `minio-go` client), a thin Postgres row
  per event for search/filtering, retention window per plan.
- Org → team → project hierarchy; projects scoped to a team.
- Ingest write path: request → validate/auth → enqueue → async group + persist,
  so a burst never blocks the SDK. Backpressure = 429 with `Retry-After`.

### Phase 2 — the event, fully

- **Stack trace UI data**: source context lines, `in_app` refinement rules per
  platform, frame variables, `<anonymous>` collapsing, recursion folding.
- **Source maps / debug files**: upload endpoint, `release`-scoped resolution
  of minified JS frames; symbolication seam for native (later).
- **Breadcrumbs, request, user, contexts, attachments** — store and render.
- **Grouping v2**: hierarchical grouping (app frames → system frames →
  message), "merge issues", "split issue", per-project grouping config, and a
  grouping-hash history so a config change can re-key without losing counts.

### Phase 3 — alerting & workflow

- Issue alerts (a new issue, a regression, a rate/threshold over a window) and
  metric alerts. Rule builder: conditions + filters + actions.
- Notification channels: email (reuse **GoEmail**), Slack, Discord, PagerDuty,
  Opsgenie, generic webhook, MS Teams.
- Ownership rules (path/glob → team), auto-assignment, issue states
  (`resolved in next release`, `ignored until N events / T time`), comments,
  "resolve via commit message" from a VCS integration.
- Spike protection — dynamic per-project ingest cap when volume jumps.

### Phase 4 — releases & deploys

- `release` as a first-class object: commits, authors, files changed, deploy
  markers on the issue timeline.
- "Regression in 2.4.1", "resolved in 2.5.0", suspect-commit detection from the
  stack frames touched by a release's diff.
- CLI (`goflare releases new/set-commits/finalize`, `goflare sourcemaps
  upload`) for CI pipelines.

### Phase 5 — performance & tracing

- Ingest `transaction` envelope items: spans, `trace_id`, `parent_span_id`.
- Trace view spanning edge → app → downstream, seeded by the edge's own span
  (it already sees the whole request). p50/p75/p95/p99 per transaction,
  throughput, failure rate, slow-span breakdown.
- **This is the payoff of the fusion**: the edge contributes the outermost span
  for free, for every proxied request, SDK or not.

### Phase 6 — the edge, for real

- **WAF**: rule sets (OWASP CRS-style), custom rules (method/path/header/geo/
  ASN/body), managed rules, per-rule log/challenge/block, and every block is an
  event you can see next to your errors.
- **Rate limiting**: per-IP / per-key / per-route token buckets; "rate-limit
  this route" as a one-click action from an issue.
- **Cache**: response caching with TTL / stale-while-revalidate / cache keys /
  purge API.
- **TLS**: ACME (`golang.org/x/crypto/acme/autocert`) for proxied hostnames.
- **DNS**: authoritative DNS for managed zones (the `ClaudeflareLink.md` zone is
  a candidate first tenant), health-checked failover records.
- **Load balancing**: multiple upstreams per route, health checks, sticky
  sessions.
- Multi-PoP: `goflared -mode edge` at N locations, config + event stream from a
  central `goflared -mode core`.

### Phase 7 — governance & multi-tenant (reuse, not rebuild)

- Mount behind the **GoAdmin gateway**: identity via `gobase_session` →
  `/api/auth/me`, `(role, action, resource)` RBAC (project/team scoped),
  hash-chained audit log, **GoSecrets** for channel tokens and TLS keys.
- API tokens (org / project scoped), SSO, SCIM.
- **Plan gating** (`internal/plan`): event volume / month, retention window,
  edge request volume, WAF rule count, PoP count, seat count, data residency —
  the pricing model, and it comes last.

### Phase 8 — web console (Node)

- Issue stream, issue detail (stack, breadcrumbs, tags, events, activity),
  dashboards, alert-rule builder, release view, trace explorer, and the edge
  console (routes, WAF, cache, DNS, analytics). SPA behind the gateway like
  gofile/gobase/GoObserv.

---

## 4. What to reuse from V5 (don't rebuild)

| Need | Reuse |
|---|---|
| Reverse proxy, host routing, TLS, config reload | `GoAdmin/gateway` |
| RBAC engine `(role, action, resource)` | `GoAdmin/gateway/internal/rbac` |
| Tamper-evident audit log (hash chain) | `GoAdmin/gateway/internal/auditlog` |
| Secrets at rest (AES-256-GCM, PG) | `GoAdmin/gateway/internal/secrets` (GoSecrets) |
| Postgres store pattern, migrations | `GoAdmin/gobase/backend/internal/*/pgstore.go` |
| Object storage for raw events | `GoSnow/internal/storage`, `GoAdmin/gofile` (`minio-go`) |
| Outbound email for alerts | `GoAdmin/GoEmail` |
| Load-testing ingest & the edge | `GoAdmin/GoLoad` |
| Treating GoFlare as one source among many | `GoAdmin/GoObserv` (add an adapter) |
| Cross-linking outages ↔ error spikes | `GoUptime` incidents |
| JWT / JSON helpers | `GoAdmin/pkg/apikit` |

## 5. Explicitly out of scope

- Reimplementing a vectorized analytics engine for event search — lean on
  Postgres + object store first, a columnar store (`GoSnow`?) only if needed.
- Native/mobile symbolication before there is a web/backend user asking.
- Being a full CDN (static asset optimization, image resizing, edge KV) — the
  edge exists to see and shape traffic, caching is as far as it goes for now.
- A billing/payments product — plan gating and a usage view, stop there.
- Bug-for-bug Sentry API compatibility — compatible enough that SDKs and the
  `sentry-cli` mostly work.

## 6. Open questions

1. Edge and core in one binary with two listeners (Phase 0) vs always two
   processes? Multi-PoP forces the split at Phase 6 — do it sooner?
2. Event storage: Postgres row + object-store blob, or a real event store
   (ClickHouse-shaped) from Phase 1? Sentry itself moved to ClickHouse for a
   reason.
3. Grouping config changes: re-key existing issues (expensive, correct) or only
   apply going forward (cheap, confusing)?
4. Trace id: mint at the edge and require downstream propagation, or accept
   W3C `traceparent` if the client already sends one?
5. WAF rule language — adopt an existing grammar (Coraza / ModSecurity rules)
   or a small typed DSL of our own?
6. Does the DNS product pull its weight in a v1, or is it a Phase 9 "because we
   can"?

## 7. Status

- [x] **Phase 0 — walking skeleton** — `goflared` builds, `go test ./...`
  green; SDK envelope ingest → fingerprint grouping → issue API works;
  edge proxy captures upstream 5xx / outages into the same issue store;
  regression detection; all in-memory + JSON snapshot.
- [x] **Phase 1 — durable storage** —
  `internal/blob` (Mem / Local / S3 object store for raw event bodies),
  `internal/pg` (shared `goflare`-schema connection),
  `project.PGStore` + `org` (Org→Team→Project, Mem + PG) + `group.UsePostgres`
  (issues, issue-status history, an event index in PG; bodies in the blob store;
  events no longer sample-bounded),
  `ingest.Pipeline` (bounded queue + worker pool, `202` on accept, `429` +
  `Retry-After` on a full queue, drains on shutdown),
  `goflared -database-url` / `-events-dir` / `-blob-*` wiring.
  Postgres store tests run against `GOFLARE_TEST_DATABASE_URL`.
  *Not yet: org/team REST endpoints, the async write-path reaper for a
  worker that died mid-group, per-plan retention.*
- [ ] Phase 2 — full event model, source maps, grouping v2
- [ ] Phase 3 — alerting, channels, workflow
- [ ] Phase 4 — releases & deploys + CLI
- [ ] Phase 5 — performance & distributed tracing (edge contributes the root span)
- [ ] Phase 6 — real edge: WAF, rate limit, cache, TLS, DNS, multi-PoP
- [ ] Phase 7 — governance via gateway + plan gating
- [ ] Phase 8 — web console
