# GoSnow

A from-scratch **Snowflake-style cloud data platform** in Go — separate storage
and compute, a SQL surface over columnar data in object storage, and virtual
warehouses as elastic compute.

Full plan and rationale (including the Node vs Python vs Go decision):
[`future.md`](future.md).

## Status: Phase 0 — walking skeleton

`gosnowd` compiles and its component seams are real interfaces, but every
implementation is in-memory and the query engine only understands a toy slice
of SQL. Nothing here stores or queries real data yet — see `future.md` §3.

## Layout

| Path | Role |
|---|---|
| `cmd/gosnowd` | server entrypoint |
| `internal/server` | HTTP / SQL REST API |
| `internal/catalog` | metadata: databases → schemas → tables |
| `internal/storage` | object-store abstraction (`MemStore`, `LocalStore`, `S3Store`) |
| `internal/warehouse` | virtual-warehouse (compute pool) registry |
| `internal/query` | query coordinator + skeleton engine |

## Run

```bash
cd GoSnow
go test ./...
go run ./cmd/gosnowd                          # in-memory, listens on :8090
go run ./cmd/gosnowd -data ./_data -addr :9000  # local disk

# S3-compatible object storage (MinIO shown; also AWS / R2 / Spaces / B2)
export GOSNOW_S3_ACCESS_KEY=minioadmin GOSNOW_S3_SECRET_KEY=minioadmin
go run ./cmd/gosnowd -s3-endpoint localhost:9000 -s3-bucket gosnow -s3-insecure
```

## Try it

```bash
curl localhost:8090/healthz

curl -s localhost:8090/v1/statements \
  -d '{"sql":"CREATE DATABASE analytics"}'

curl -s localhost:8090/v1/statements \
  -d '{"sql":"SHOW DATABASES"}'

curl -s localhost:8090/v1/statements -d '{"sql":"SELECT 1"}'

curl -s localhost:8090/v1/warehouses \
  -d '{"name":"wh1","size":"small"}'
```

Anything outside the skeleton's grammar returns `422` with a pointer to the
relevant `future.md` phase — that is expected.
