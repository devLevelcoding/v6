# GoUptime

A from-scratch **uptime & dead-route monitoring service** in Go — probe HTTP and
TCP targets on their own intervals, detect incidents with a hysteresis policy so
a single blip pages nobody, and fan incident events out to a webhook and the
log.

Full plan and rationale: [`future.md`](future.md).

## Status: Phase 0 — walking skeleton

`gouptimed` compiles, `go test ./...` is green, and the probe → detect → notify
pipeline runs end to end. Everything is in-memory: monitors, results and
incidents do not survive a restart, and the only monitor types are `http` and
`tcp`. SSL/DNS checks, the crawl-based dead-route checker, status pages and
Postgres persistence are later phases — see `future.md` §3.

## Layout

| Path | Role |
|---|---|
| `cmd/gouptimed` | server entrypoint, flags, graceful shutdown |
| `internal/monitor` | monitor model + validation + in-memory `Store` (Postgres stand-in) |
| `internal/check` | one probe against one monitor (`http`, `tcp`); returns a `Result` |
| `internal/incident` | `Result` stream → incidents, with open/resolve hysteresis |
| `internal/history` | bounded per-monitor result ring + uptime summary |
| `internal/notify` | `Notifier` interface + `LogNotifier`, `WebhookNotifier`, `Multi` |
| `internal/scheduler` | one goroutine per enabled monitor, on its interval |
| `internal/server` | REST API |
| `internal/uid` | random ids over `crypto/rand` (no UUID dependency) |

Zero external dependencies — standard library only.

## Run

```bash
cd GoUptime
go test ./...
go run ./cmd/gouptimed                                   # listens on :8095, log-only alerts
go run ./cmd/gouptimed -webhook https://example.com/hook  # POST incident events as JSON
go run ./cmd/gouptimed -fail-threshold 2 -recover-threshold 2 -retain 1000
```

Flags (all also environment vars — `GOUPTIME_ADDR`, `GOUPTIME_WEBHOOK_URL`,
`GOUPTIME_FAIL_THRESHOLD`, `GOUPTIME_RECOVER_THRESHOLD`, `GOUPTIME_RETAIN`):

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8095` | listen address |
| `-webhook` | _(none)_ | incident webhook URL; empty = log only |
| `-fail-threshold` | `3` | consecutive failures before an incident opens |
| `-recover-threshold` | `2` | consecutive successes before it resolves |
| `-retain` | `500` | results kept in memory per monitor |

## Try it

```bash
curl localhost:8095/healthz

# add a monitor (probes start immediately)
curl -s localhost:8095/v1/monitors -d '{
  "name":"example","type":"http","target":"https://example.com",
  "interval_seconds":30,"enabled":true
}'

# a monitor that will fail
curl -s localhost:8095/v1/monitors -d '{
  "name":"dead-port","type":"tcp","target":"127.0.0.1:2",
  "interval_seconds":10,"enabled":true
}'

curl -s localhost:8095/v1/monitors
curl -s localhost:8095/v1/monitors/<id>/summary      # uptime ratio, avg latency, last result
curl -s localhost:8095/v1/monitors/<id>/results?limit=20
curl -s -XPOST localhost:8095/v1/monitors/<id>/check # probe now, synchronously
curl -s localhost:8095/v1/incidents                  # ?monitor=<id> to filter
```

### API

| Method + path | Purpose |
|---|---|
| `GET /healthz`, `GET /v1/version` | liveness, build version |
| `GET /v1/monitors` | list monitors |
| `POST /v1/monitors` | create (`interval_seconds`, `timeout_seconds`, `expect_status:[lo,hi]`) |
| `GET /v1/monitors/{id}` | one monitor |
| `PUT /v1/monitors/{id}` | replace |
| `DELETE /v1/monitors/{id}` | remove |
| `POST /v1/monitors/{id}/check` | probe once now, return the `Result` |
| `GET /v1/monitors/{id}/results?limit=N` | recent results, newest first |
| `GET /v1/monitors/{id}/summary` | rolled-up uptime over the retained window |
| `GET /v1/incidents?monitor={id}` | incident log, newest first |

`expect_status` defaults to "any 2xx or 3xx". Set `[200,204]` to be strict.
