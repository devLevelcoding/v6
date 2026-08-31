# GoFlare

A from-scratch **error-tracking platform with an edge in front of it** — Sentry's
job (SDK event ingest, fingerprint grouping, issue triage) fused with
Cloudflare's position (a reverse proxy your traffic flows through), so the edge
captures your app's failures with zero SDK work and files them next to the ones
your SDK reports.

Full plan and rationale: [`future.md`](future.md).

## Status: Phase 1 — durable storage

`goflared` compiles, `go test ./...` is green (Postgres store tests run against
`GOFLARE_TEST_DATABASE_URL`, skip without it), and the whole chain runs end to
end: **Sentry SDK → envelope ingest → fingerprint → issue**, and **edge proxy →
upstream 5xx → synthetic event → same issue store**. A **web console** (embedded,
no build step) serves at `/`.

**Two storage modes:**

- **In-memory + snapshot** (default) — state survives a restart via one JSON
  file (`-snapshot-path`). Fine for single-instance / local dev.
- **Postgres + blob store** (`-database-url`) — projects, issues, an event
  index and issue-status history in Postgres (`goflare` schema); raw event
  bodies in a blob store (`-events-dir` for a local directory, or `-blob-*` for
  any S3-compatible endpoint). Ingest becomes an **async pipeline**: the handler
  authenticates, enqueues and returns `202`; a worker pool groups + persists;
  a full queue returns `429` + `Retry-After` so a burst never blocks the SDK
  and nothing is dropped silently. Multi-instance ready.

An `org → team → project` tenancy hierarchy backs both modes (REST endpoints
land with the rest of the dashboard API next). The edge is still a thin
host-routed proxy with failure capture only; WAF, caching, DNS are later
phases. See `future.md` §3.

## Layout

| Path | Role |
|---|---|
| `cmd/goflared` | server entrypoint: core listener + optional edge listener |
| `internal/project` | projects + DSN keys — `MemStore` and `PGStore` behind one interface |
| `internal/org` | `org → team → project` tenancy — `MemStore` and `PGStore` |
| `internal/event` | event model + tolerant decoder + Sentry envelope parser + ingest auth |
| `internal/group` | **fingerprinting + issue upsert + event sampling** — the core; in-memory or Postgres + blob (`UsePostgres`) |
| `internal/blob` | object store for raw event bodies: `MemStore`, `LocalStore`, `S3Store` |
| `internal/pg` | opens the shared Postgres connection (`goflare` schema) |
| `internal/ingest` | SDK endpoints `/api/{id}/envelope/`, `/api/{id}/store/` + the async `Pipeline` |
| `internal/api` | dashboard REST API: projects, issues, triage, events |
| `internal/ui` | embedded web console (vanilla JS SPA on the `/api/0/*` API) |
| `internal/snapshot` | JSON-file persistence for in-memory mode |
| `internal/edge` | host-routed reverse proxy that captures upstream 5xx / outages as events |
| `internal/server` | wires ingest + api + console + health onto one mux |
| `internal/pgtest` | throwaway-database helper for the Postgres store tests |
| `internal/uid` | random ids over `crypto/rand` |

Dependencies: `lib/pq` (Postgres) and `minio-go` (S3 blob store) — both shared
with other V5 projects; everything else is standard library.

## Run

```bash
cd GoFlare
go test ./...                                       # Postgres tests skip unless GOFLARE_TEST_DATABASE_URL is set
GOFLARE_TEST_DATABASE_URL=postgres://u:p@localhost/scratch?sslmode=disable go test ./...

go run ./cmd/goflared -seed-project "My App"        # in-memory, core + console on :9000, logs a DSN
open http://localhost:9000                          # the web console

# persist across restarts (in-memory mode):
go run ./cmd/goflared -seed-project "My App" -snapshot-path ./goflare.json

# Postgres + local blob store (Phase 1):
go run ./cmd/goflared -seed-project "My App" \
  -database-url postgres://u:p@localhost/goflare?sslmode=disable \
  -events-dir ./goflare-events

# ...with S3 for event bodies instead of a local dir:
go run ./cmd/goflared -database-url ... \
  -blob-endpoint s3.amazonaws.com -blob-bucket goflare-events \
  -blob-access-key AKIA... -blob-secret-key ...

# with the edge in front of a local app:
go run ./cmd/goflared -seed-project "My App" \
  -edge-upstream http://localhost:3000 -edge-project my-app
# edge then listens on :9001 and files that app's 5xx under "my-app"
```

Flags (also env vars — `GOFLARE_ADDR`, `GOFLARE_EDGE_ADDR`, `GOFLARE_PUBLIC_URL`,
`GOFLARE_EVENTS_PER_ISSUE`, `GOFLARE_EDGE_UPSTREAM`, `GOFLARE_EDGE_HOST`,
`GOFLARE_EDGE_PROJECT`, `GOFLARE_SEED_PROJECT`, `GOFLARE_SNAPSHOT`):

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:9000` | ingest + dashboard API listener |
| `-edge-addr` | `:9001` | edge proxy listener (only starts if `-edge-upstream` set) |
| `-public-url` | `http://localhost:9000` | base URL used to render project DSNs |
| `-events-per-issue` | `50` | events sampled and kept per issue |
| `-edge-upstream` | _(none)_ | upstream URL to proxy; enables the edge |
| `-edge-host` | _(any)_ | Host header the edge route matches |
| `-edge-project` | _(none)_ | project slug/id that edge failures file under |
| `-seed-project` | _(none)_ | create a project on boot and log its DSN (skipped if it already exists in the snapshot) |
| `-seed-project-id` | _(generated)_ | fixed id for the seeded project, so its DSN is known before boot |
| `-seed-key` | _(generated)_ | fixed DSN public key for the seeded project |
| `-snapshot-path` | _(none)_ | JSON file to persist projects/issues/events to; empty = in-memory only |
| `-snapshot-interval` | `15s` | how often to flush the snapshot when state changed |

## Try it — SDK ingest

```bash
# point any Sentry SDK at the DSN goflared logged, or by hand:
KEY=<public_key>  PID=<project_id>
ITEM='{"exception":{"values":[{"type":"TypeError","value":"x is undefined",
  "stacktrace":{"frames":[{"filename":"app/pay.js","function":"charge","in_app":true}]}}]},
  "level":"error","platform":"javascript"}'
printf '{"event_id":"%032x"}\n{"type":"event","length":%d}\n%s\n' 0 ${#ITEM} "$ITEM" \
 | curl -s -XPOST "localhost:9000/api/$PID/envelope/" \
     -H "X-Sentry-Auth: Sentry sentry_key=$KEY, sentry_version=7" --data-binary @-

curl -s "localhost:9000/api/0/projects/$PID/issues/"
curl -s -XPUT "localhost:9000/api/0/issues/<issue_id>/" -d '{"status":"resolved"}'
```

Send the same error again after resolving it and the issue reopens as a
**regression**.

### Dashboard API

| Method + path | Purpose |
|---|---|
| `GET /api/0/projects/` · `POST /api/0/projects/` | list · create (returns DSN) |
| `GET /api/0/projects/{id}/` | one project |
| `GET /api/0/projects/{id}/issues/?status=&query=` | issue stream |
| `GET /api/0/issues/{id}/` | issue detail |
| `PUT /api/0/issues/{id}/` | `{"status":"resolved"\|"ignored"\|"unresolved"}` |
| `GET /api/0/issues/{id}/events/?limit=N` | sampled events, newest first |
| `GET /api/0/issues/{id}/events/latest/` | most recent event |

### Ingest API (Sentry-compatible)

| Method + path | Purpose |
|---|---|
| `POST /api/{project_id}/envelope/` | envelope format (gzip/deflate aware) |
| `POST /api/{project_id}/store/` | legacy bare-event format |

Auth is the DSN public key via `X-Sentry-Auth`, `Authorization: Sentry …`, or
`?sentry_key=`.

## How grouping works

`internal/group` computes an ordered fingerprint and SHA-1s it into an issue key:

1. an explicit `fingerprint` on the event wins (with `{{ default }}` expansion);
2. else, for an exception: `type` + a per-frame signature of the **in-app**
   frames (all frames if none are in-app); frameless falls back to a
   number/hex-masked exception value;
3. else the message, with embedded numbers and hex ids masked so
   `user 4171 not found` and `user 5522 not found` land on one issue;
4. else level, else platform.

New fingerprint → new issue. Same fingerprint on a resolved issue → regression.
