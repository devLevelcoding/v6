# GoSnow — Test Plan

## Automated (present)

`cd GoSnow && go build ./... && go vet ./... && go test ./...`

- `internal/catalog` — database/schema/table create, duplicate → `ErrExists`,
  missing parent → `ErrNotFound`, identifier folding, list ordering.
- `internal/storage` — one suite run against `MemStore` and `LocalStore`:
  put/get round trip, prefix list, delete, `ErrNotExist` after delete. The same
  suite runs against `S3Store` when `GOSNOW_S3_TEST_ENDPOINT` is set (local
  MinIO); it skips otherwise, so the default `go test ./...` stays offline.
- `internal/query` — `SELECT <literal>`, `CREATE DATABASE`, `CREATE SCHEMA
  db.schema`, `SHOW DATABASES`, `SHOW SCHEMAS IN db`; unsupported statements →
  `ErrUnsupported`.
- `internal/server` — `/healthz`, `POST /v1/statements` happy path + 422 for
  unsupported SQL, `POST /v1/warehouses` → 201.

## Automated (to add as phases land)

- Phase 1: Postgres catalog against a throwaway schema; `GCSStore` / `AzureBlobStore`
  parity with the `S3Store` suite; micro-partition stats pruning picks the right
  object set.
- Phase 2: engine correctness vs a reference (DuckDB direct) on TPC-H SF1;
  result-cache hit/miss.
- Phase 3: warehouse resume/suspend/auto-suspend; multi-cluster scale-out under
  a synthetic queue.
- Phase 5: RBAC denial through the gateway; audit chain `Verify` after a run.

## Manual

- `go run ./cmd/gosnowd -data ./_data`; exercise the `curl` calls in
  `README.md`; confirm `_data/` is created and `SIGINT` shuts down cleanly.
- Point the (future) Python driver at `http://localhost:8090` and run a
  `CREATE DATABASE` / `SHOW DATABASES` round trip.

## Priority

Keep Phase 0 green on every change — it locks the component seams
(catalog / storage / warehouse / query) that every later phase builds on.
