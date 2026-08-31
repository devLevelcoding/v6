# GoUptime — Feature Roadmap (V5)

A from-scratch **uptime monitoring & dead-route checker** in Go: probe targets
on independent intervals, detect and track incidents, notify, and publish status
pages — reusing the platform layer already built in `GoAdmin` for identity,
RBAC, audit and secrets.

This file is the north star. Phase 0 (a compiling, tested walking skeleton) is
in this repo now; everything past it is planned, not built.

---

## 1. Why this, why Go

Two questions were asked, and they turn out to be the same engine:

1. **"Can we use Go to check all dead routes of a project?"** — yes. A crawler
   that starts at a base URL, follows `<a>/<link>/<script>/<img>/<form>` and
   `sitemap.xml`, records every non-2xx/3xx, timeout and redirect loop, and
   (manifest mode) cross-checks a declared route list — Next.js
   `routes-manifest.json`, an OpenAPI spec, a Go router's registered patterns —
   to catch routes that nothing links to.
2. **"Can we build an uptime service like the paid ones?"** — yes. A scheduler
   firing checks on an interval, each check a goroutine doing HTTP/TCP/ICMP/
   DNS/SSL probes, results to Postgres, incident detection, notifications, and a
   read-only status page over incident history.

Go is the right tool: cheap goroutines for thousands of concurrent probes,
precise `context` timeouts, a static binary to drop on a probe node, a strong
`net/http` and `crypto/tls` standard library. The commercial tier sheet that
prompted this (60s→15s intervals, monitor counts, retention windows,
integrations, white-label status pages) is entirely a product-packaging problem
on top of this engine, not a technical one.

### The "Free tier" we're matching

The reference pricing page's paid tiers gate on: monitoring interval (60s / 30s
/ 15s), monitor count (10 / 100 / 200), data retention (12 months), monitor
types (HTTP, TCP, ping, keyword, SSL, DNS), status pages (count, custom domain,
white-label), and integrations (webhook, Zapier, PagerDuty, Slack). GoUptime's
own "free" baseline: unlimited monitors, HTTP + TCP + SSL + DNS, 60s minimum
interval, 30-day retention, one status page, webhook + log notifications. Tier
gating (`internal/plan`) is Phase 6 and deliberately last.

---

## 2. Architecture

```
                    ┌───────────────────────────────────────┐
   clients ───────► │  REST API  (Go, internal/server)      │
   (console, CLI)   └──────────────┬────────────────────────┘
                                   │  monitor CRUD, mutate → Sync()
                    ┌──────────────▼────────────────┐
                    │  Scheduler  (Go)             │
                    │  one goroutine per enabled   │
                    │  monitor, fires on interval  │
                    └───┬───────────────┬──────────┘
              probe      │               │  every Result
        ┌────────────────▼──┐        ┌───▼──────────────────────┐
        │  Check (Go)       │        │  Incident detector (Go)  │
        │  http / tcp /     │        │  N fails → open,         │
        │  ssl / dns / ping │        │  M ok → resolve          │
        └──────────────────┘        └───┬──────────────────────┘
                                        │  state change
        ┌──────────────────┐        ┌───▼──────────────────────┐
        │  History (Go)    │        │  Notify (Go)             │
        │  results +       │        │  webhook, log → Slack,   │
        │  uptime rollups  │        │  email, PagerDuty        │
        │  → Postgres      │        └──────────────────────────┘
        └──────────────────┘

  cross-cutting (reuse GoAdmin gateway): identity, RBAC, hash-chained audit,
  GoSecrets for webhook/SMTP credentials, config hot-reload.
  probe nodes: gouptimed runs in "agent" mode in N regions, reports to core.
```

Core principle: **a probe is stateless and cheap; the value is in the timeline.**
The scheduler decides *when*, `check` decides *up or down right now*, `incident`
decides *what a run of results means*, and everything else reads the stored
timeline.

---

## 3. Phased roadmap

### Phase 0 — walking skeleton ✅ (in this repo)

A compiling, tested `gouptimed` that wires the real seams with in-memory
implementations.

- `cmd/gouptimed` — HTTP server, graceful shutdown, `-addr` / `-webhook` /
  `-fail-threshold` / `-recover-threshold` / `-retain` flags + env vars.
- `internal/monitor` — `Monitor` model, `Validate`, `Store` interface +
  `MemStore`. Stand-in for the Postgres store. `MinInterval` is a var so tests
  run fast.
- `internal/check` — `Prober` interface + `DefaultProber` with real `http` and
  `tcp` probes; `classifyErr` normalizes transport errors to stable phrases
  (`timeout`, `dns failure`, `connection refused`, `tls error: …`).
- `internal/incident` — `Detector` with per-monitor streak state and an
  incident log; `Policy{FailThreshold, RecoverThreshold}` hysteresis; emits
  `Opened` / `Resolved` events.
- `internal/history` — `Ring` (bounded per-monitor `Recorder`) + `Summary`
  (total, up, uptime ratio, avg latency, last result).
- `internal/notify` — `Notifier` interface, `LogNotifier`, `WebhookNotifier`
  (JSON POST, non-2xx = error), `Multi` (fan-out, aggregate errors).
- `internal/scheduler` — `New` + `Start` + `Sync` (reconcile loops with the
  store) + `RunNow` (synchronous single probe through the full pipeline).
- `internal/server` — REST: health/version, monitor CRUD, `/check`, `/results`,
  `/summary`, `/incidents`.

Run: `cd GoUptime && go test ./... && go run ./cmd/gouptimed`

### Phase 1 — durable persistence

- Postgres-backed `monitor.Store`, `history` and `incident` logs (same instance
  and pattern as GoAdmin 2.0's `goadmin` schema — one connection, `gouptime`
  schema). Migrations.
- Retention: a sweep job trims results older than the plan's window; downsample
  to per-minute / per-hour rollups for the timeline view.
- Result write path batched (channel → bulk insert) so a probe never blocks on
  the DB.
- Scheduler rebuilds loop state from the store on boot; `Sync` triggered by a
  Postgres `LISTEN/NOTIFY` so multiple API replicas stay consistent.

### Phase 2 — more monitor types

- **SSL certificate** — connect, read the leaf cert, alert at N days to expiry
  (warn + critical thresholds). This is the highest-value cheap check.
- **DNS** — resolve a name, assert record type / expected values / resolver.
- **Keyword** — HTTP body must / must not contain a string or match a regex.
- **ICMP ping** — raw socket (needs `CAP_NET_RAW` / setuid); fall back to TCP.
- **HTTP advanced** — method, headers, body, auth, follow-redirect policy,
  response-time SLA as a soft failure, mTLS client cert from GoSecrets.
- **Heartbeat / cron** — inverse monitor: a job must ping *us* every N minutes
  or we open an incident ("dead man's switch").

### Phase 3 — the dead-route checker

- `internal/crawl` — a new monitor type `crawl`: BFS from a base URL with a
  worker pool, `golang.org/x/net/html` tokenizer, a `visited` set, same-origin
  scope by default, configurable depth and rate limit, `robots.txt` respected.
- Report: broken links (with the referring page), redirect chains/loops, mixed
  content, orphan pages, slow pages, pages over a size budget.
- **Manifest mode** — feed a declared route list and probe each:
  Next.js `.next/routes-manifest.json` + `app/` dir walk, `sitemap.xml`,
  OpenAPI `paths`, a Go `http.ServeMux` pattern dump, a Rails/Laravel routes
  export. Diff "declared" vs "reachable by crawl" → dead routes and 404s.
- CLI front end `gouptime crawl https://site --manifest .next/routes-manifest.json`
  for CI use (exit non-zero on findings); same engine as the hosted monitor.

### Phase 4 — notifications & escalation

- Channels: email (SMTP, reuse **GoEmail** if present), Slack, Discord,
  PagerDuty Events API, Opsgenie, generic webhook (done), SMS (Twilio).
- Per-monitor notification policy: which channels, quiet hours, escalation
  after X minutes unresolved, re-notify interval, auto-resolve notice.
- On-call schedules and rotations (or integrate PagerDuty's and stay out of it).
- Alert de-dupe and flap detection (already have hysteresis; add a flap counter
  that suppresses notifications for a monitor that opened/resolved > K times in
  an hour).

### Phase 5 — status pages

- Public read-only page per "status page" object: component list, current
  state, uptime % over 90 days, active + historical incidents, subscribe for
  updates (email / RSS / webhook / Slack).
- Custom domain (CNAME + ACME via `golang.org/x/crypto/acme/autocert`),
  white-label (logo, colors, custom CSS), scheduled-maintenance windows.
- Incident updates: a lightweight authored timeline ("investigating →
  identified → monitoring → resolved") separate from the auto-detected incident.

### Phase 6 — multi-region probing

- `gouptimed -mode agent -core https://core…` runs only the scheduler + check,
  streams results to core over gRPC/HTTP. Core owns the DB, incidents, API.
- A monitor probed from ≥ 2 regions; an incident opens only when a quorum of
  regions agree it's down (kills false positives from one bad network path).
- Region latency breakdown on the timeline.

### Phase 7 — governance & multi-tenant (reuse, not rebuild)

- Mount GoUptime behind the **GoAdmin gateway**: identity via `gobase_session`
  → `/api/auth/me`, `(role, action, resource)` RBAC, hash-chained audit log,
  **GoSecrets** for SMTP/Twilio/PagerDuty keys and mTLS client certs.
- Organizations / teams / projects; monitors and status pages scoped to a team.
- **Plan gating** (`internal/plan`): interval floor, monitor cap, retention
  window, monitor-type allowlist, status-page count, integration allowlist per
  tier. This is the pricing sheet, and it comes last on purpose.

### Phase 8 — dependency monitors & synthetics

- **Dependency monitor** (the pricing sheet's "Sept '26" feature): a monitor
  whose state is derived from other monitors (`all up`, `any up`, `k of n`),
  for "is the checkout flow's whole dependency graph healthy".
- **Synthetic / transaction monitors**: a scripted multi-step browser or HTTP
  flow (login → add to cart → checkout), each step asserted. Browser steps via
  a headless Chrome pool (chromedp) on the agent.

### Phase 9 — web console (Node)

- Monitor list with sparklines, monitor detail (timeline, incidents, response
  breakdown), incident view, status-page editor, notification-policy editor,
  team/role admin, usage dashboard. SPA behind the gateway like gofile/gobase.

---

## 4. What to reuse from V5 (don't rebuild)

| Need | Reuse |
|---|---|
| Reverse proxy, one origin, identity | `GoAdmin/gateway` |
| RBAC engine `(role, action, resource)` | `GoAdmin/gateway/internal/rbac` |
| Tamper-evident audit log (hash chain) | `GoAdmin/gateway/internal/auditlog` |
| Secrets at rest (AES-256-GCM, PG) | `GoAdmin/gateway/internal/secrets` (GoSecrets) |
| Config hot-reload pattern | `GoAdmin` Phase 1.5 |
| Postgres store pattern, migrations | `GoAdmin/gobase/backend/internal/*/pgstore.go` |
| Outbound email | `GoAdmin/GoEmail` |
| Load-testing the probe fan-out | `GoAdmin/GoLoad` |
| Metrics / tracing | `GoAdmin/GoObserv`, `GoPlatform` modules |
| JWT / JSON helpers | `GoAdmin/pkg/apikit` |

## 5. Explicitly out of scope

- A full APM / RUM product — this watches endpoints, it does not instrument apps.
- Log aggregation / metrics storage — that's `GoObserv`.
- Being a PagerDuty replacement — integrate on-call, don't rebuild it.
- A billing/payments system — plan gating and a usage view, stop there.
- Probing at < 10s intervals in Phase 0–5 — the engine can, the product won't
  until multi-region (Phase 6) makes it meaningful.

## 6. Open questions

1. Incident model: keep the auto-detected incident and the authored
   status-page incident as one row with a status enum, or two linked objects?
2. Result storage at scale — raw rows + rollups in Postgres, or push raw to a
   TSDB (`GoObserv`?) and keep only incidents/rollups relational?
3. ICMP ping — ship it (needs raw sockets / setuid / agent privileges) or punt
   to "TCP is close enough"?
4. Agent ↔ core transport — gRPC (streaming, typed) vs plain HTTP POST batches
   (trivial to firewall-allow)?
5. Crawl scope for the dead-route checker — same-origin only, or allow an
   allowlist of extra hosts (CDN, auth domain)?
6. Do status-page custom domains justify running our own ACME, or front it with
   the gateway / a managed proxy?

## 7. Status

- [x] **Phase 0 — walking skeleton** — `gouptimed` builds, `go test ./...`
  green, probe → detect → notify pipeline runs end to end with in-memory
  stores; `http` + `tcp` monitors; webhook + log notifications.
- [ ] Phase 1 — Postgres persistence + retention + rollups
- [ ] Phase 2 — SSL / DNS / keyword / ping / heartbeat monitor types
- [ ] Phase 3 — dead-route crawler (`crawl` monitor + `gouptime crawl` CLI)
- [ ] Phase 4 — notification channels & escalation
- [ ] Phase 5 — status pages
- [ ] Phase 6 — multi-region probing
- [ ] Phase 7 — governance via gateway + plan gating
- [ ] Phase 8 — dependency & synthetic monitors
- [ ] Phase 9 — web console
