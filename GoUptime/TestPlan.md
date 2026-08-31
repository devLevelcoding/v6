# GoUptime — Test Plan

## Automated (present)

`cd GoUptime && go build ./... && go vet ./... && go test ./...`

- `internal/monitor` — `Validate` table (type/target/interval/status-range
  rules), derived defaults (interval 60s, timeout min(interval,10s)),
  `AcceptsStatus`, `MemStore` CRUD, duplicate ID → `ErrExists`, missing →
  `ErrNotFound`, `CreatedAt` preserved on update, `List` sorted by name.
- `internal/check` — against an `httptest` server: 2xx → up, 500 → down with
  status, slow response vs short `Timeout` → `"timeout"`, dead port →
  connection error. TCP: real `net.Listen` → `"connected"`, closed port → down.
- `internal/incident` — opens only after `FailThreshold` consecutive fails,
  resolves only after `RecoverThreshold` consecutive successes, a single
  success mid-streak does not resolve, a single failure resets the streak,
  `FailCount` accumulates across an open incident, per-monitor isolation,
  `Incidents("")` vs `Incidents(id)` filter.
- `internal/history` — `Ring` evicts oldest at capacity, results newest-first,
  `limit` honored, `Summary` counts / uptime ratio / avg latency / last,
  empty-monitor summary.
- `internal/notify` — `WebhookNotifier` posts JSON and parses back, non-2xx →
  error, empty URL → no-op; `Multi` calls every child and aggregates errors;
  `LogNotifier` handles both event types.
- `internal/scheduler` (`TestMain` sets `monitor.MinInterval = 1ms`) —
  `RunNow` records the result and fires the incident notification, unknown
  monitor → error, `Sync` launches a loop that probes repeatedly, `Sync` after
  delete/disable stops the loop.
- `internal/server` — health/version, monitor CRUD round trip (create→get→
  update→list→delete→404), validation error → 400, unknown JSON field → 400,
  `Sync` called on every mutation, `/check` returns the `Result`,
  `/results` + `/summary` (and 404 for a missing monitor), `/incidents` with
  and without the `monitor` filter.

Race detector (`go test -race ./...`) needs CGo/gcc — run it in CI where a C
toolchain is present.

## Automated (to add as phases land)

- Phase 1: Postgres `Store` / history / incident logs against a throwaway
  schema; retention sweep trims the right rows; scheduler rebuilds loop state
  from the store on boot; `LISTEN/NOTIFY` fan-out to a second API replica.
- Phase 2: SSL monitor against a cert with a known `NotAfter` (warn/critical
  thresholds); DNS monitor vs a stub resolver; keyword match/no-match.
- Phase 3: crawler finds a seeded broken link and names the referring page;
  manifest diff reports a route that exists in `routes-manifest.json` but 404s;
  `gouptime crawl` exits non-zero on findings.
- Phase 4: escalation fires after the unresolved window; flap suppression after
  K open/resolve cycles in an hour; per-channel policy routing.
- Phase 6: quorum incident logic — one region down, others up → no incident;
  quorum down → incident with per-region latency.
- Phase 7: RBAC denial through the gateway; audit chain `Verify` after a run;
  plan gate rejects an 11th monitor on the free tier.

## Manual

- `go run ./cmd/gouptimed`; run the `curl` calls in `README.md`; confirm probes
  start immediately on create, `SIGINT` shuts down cleanly, and a `tcp` monitor
  to `127.0.0.1:2` opens an incident after `-fail-threshold` probes.
- `go run ./cmd/gouptimed -webhook https://webhook.site/<id>`; kill a monitored
  target; confirm the `opened` payload arrives, then restore it and confirm
  `resolved` with a `duration`.

## Priority

Keep Phase 0 green on every change — it locks the pipeline seams
(monitor / check / incident / history / notify / scheduler) that every later
phase builds on.
