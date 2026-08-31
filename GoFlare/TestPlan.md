# GoFlare — Test Plan

## Automated (present)

`cd GoFlare && go build ./... && go vet ./... && go test ./...`

- `internal/project` — create → slug + generated key + DSN rendering; blank /
  unsluggable name → `ErrInvalid`; duplicate slug → `ErrExists`;
  `Authenticate` good key / wrong key (`ErrAuth`) / missing project
  (`ErrNotFound`); `BySlug`; `List` newest-first (stable via insertion seq).
- `internal/event` — `Decode`: exception as `{values}` and as array, timestamp
  as float epoch and RFC3339 string, message via `logentry` and as a string,
  tags as object and as pair-array, level aliases (`critical`→fatal,
  `warn`→warning, `log`→info), `event_id` dash stripping; `Title`/`Culprit`
  incl. fallbacks. `ParseEnvelope`: length-framed, newline-framed, headers-only
  (`ErrNoEvent`), multi-item picks the event. `ParseAuth`: `X-Sentry-Auth`,
  `?sentry_key=`, missing. `AuthFromDSN`.
- `internal/group` — fingerprint: same type+stack groups regardless of value;
  different stack splits; message number/hex masking groups; frameless falls
  back to normalized value; explicit `fingerprint` respected; `{{ default }}`
  lone-token equals default, scoped-prefix differs. store: new → `new`, repeat
  → `recurring` + `TimesSeen`++; resolve then ingest → `regression` +
  `Regressed`, cleared on move to unresolved; per-project isolation; `List`
  status/query filters; event sample newest-first + capped while `TimesSeen`
  keeps the true count; `SetStatus` validation + `ErrNotFound`.
- `internal/ingest` — envelope happy path → 200 + issue created; auth via
  `X-Sentry-Auth` and via `?sentry_key=`; bad key → 401; unknown project →
  404; gzip body; legacy `/store/` endpoint; envelope with only a non-event
  item → 200 and no issue.
- `internal/api` — project create (201 + DSN) / list / 404; issue lifecycle
  over HTTP: two events → one issue seen twice, detail, resolve, events list,
  latest event; PUT with a bad status → 400.
- `internal/edge` — upstream 5xx passes through *and* is captured as one
  `error` issue keyed by route; upstream down → 502 + a `fatal` issue; Host
  header routes to the right upstream; no matching route → 502.
- `internal/blob` — `Store` conformance run against `MemStore` and `LocalStore`:
  missing key → `ErrNotExist` on Get/Delete, `EventKey` shape
  (`events/{proj}/YYYY/MM/DD/{id}.json`), Put stores a copy (source mutation
  doesn't leak), `List(prefix)` scoping, delete round-trip; `LocalStore` rejects
  `""`, `../escape`, traversal keys.
- `internal/org` — `Store` conformance run against `MemStore` **and** the
  Postgres `PGStore` (skips without `GOFLARE_TEST_DATABASE_URL`): org create +
  slug, duplicate org → `ErrExists`, `GetOrg` missing → `ErrNotFound`, team
  scoped to its org, team slug unique **per-org not globally**, team under a
  missing org → `ErrNotFound`, `ListTeams(orgID)` vs `ListTeams("")`.
- `internal/project` **PGStore** (skips without the env var) — the full
  `Store` contract against real Postgres: create/get/by-slug, duplicate →
  `ErrExists`, `Authenticate` good/bad-key/bad-project, `Seed` idempotent by
  slug and honouring a fixed id + key, `List` newest-first, DSN render;
  `var _ project.Store = (*PGStore)(nil)`.
- `internal/group` **PGStore** (skips without the env var) — `UsePostgres` then
  the ingest state machine against Postgres: new → recurring (`TimesSeen`
  grows, worst level wins) → separate fingerprint splits → project isolation →
  resolve then re-ingest = `regression` + `Regressed`, cleared on unresolved;
  `Get`/`List` filters; **`Events` returns *all* stored events, not the bounded
  sample** (ingest 12, read back ≥ 12); raw bodies really land in the blob
  store (`blob.List` count); `LatestEvent`.
- `internal/ingest` **Pipeline** — 40 events Submitted → all grouped into one
  issue with `TimesSeen == 40`; a full queue returns `ErrBusy` and `Depth`
  reports the backlog; cancelling the context drains what's queued before the
  workers exit (no accepted event lost).

Race detector (`go test -race ./...`) needs a C toolchain — run it in CI.

## Automated (to add as phases land)

- Phase 1 (remaining): retention sweep by blob-key prefix range; a reaper that
  requeues a `running` group op whose worker died; org/team REST endpoints.
- Phase 2: source-map resolution of a minified frame for a given release;
  grouping-config change re-keys issues without losing `TimesSeen`; merge/split.
- Phase 3: alert rule fires once per issue per window; ownership rule assigns;
  "ignore until N events" reopens on the N+1th.
- Phase 5: a trace stitched from an edge root span + two SDK child spans renders
  in order; p95 math on a known distribution.
- Phase 6: WAF rule blocks a crafted request and emits an event; rate limiter
  returns 429 after the bucket drains; cache serves a second request without
  hitting the upstream; ACME cert issued for a proxied host (staging CA).
- Phase 7: RBAC denial through the gateway; audit chain `Verify`; plan gate
  rejects ingest over the monthly event cap with a clear error.

## Manual

- `go run ./cmd/goflared -seed-project demo`; point a real Sentry SDK
  (`@sentry/node`, `sentry-sdk` for Python) at the logged DSN; throw an error;
  confirm it appears as an issue with a readable title and culprit, and that a
  second throw increments `times_seen` instead of making a new issue.
- `go run ./cmd/goflared -seed-project demo -edge-upstream http://localhost:3000
  -edge-project demo`; run any app on :3000 that can return a 500; hit
  `localhost:9001`; confirm the 500 both reaches the browser and shows up as an
  issue under `demo` without any SDK installed.
- **Postgres mode**: `go run ./cmd/goflared -database-url
  postgres://…?sslmode=disable -events-dir ./ev -seed-project demo
  -seed-project-id demo000000000001 -seed-key demokey123`; POST a Sentry
  envelope to `/api/demo000000000001/envelope/` — expect `202`; POST the same
  fingerprint again — the log shows `outcome=recurring times_seen=2`; the raw
  bodies appear under `./ev/events/demo000000000001/YYYY/MM/DD/*.json` and one
  row each in `goflare.issues` / `goflare.events`.
- Resolve an issue, trigger it again, confirm it reopens flagged as a
  regression.

## Priority

Keep Phase 0 green on every change — it locks the pipeline seams
(project / event / group / ingest / api / edge) and, above all, the fingerprint
rules in `internal/group`, which every later phase and every stored issue
depends on.
