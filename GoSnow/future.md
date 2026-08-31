# GoSnow — Feature Roadmap (V5)

A from-scratch **Snowflake-style cloud data platform** in Go: separate storage
and compute, a SQL surface over columnar data in object storage, virtual
warehouses as elastic compute, and unified governance reusing the platform
layer already built in `GoAdmin`.

This file is the north star. Phase 0 (a compiling walking skeleton) is in this
repo now; everything past it is planned, not built.

---

## 1. Language decision — why Go (settled)

Question asked: **Node.js, Python, or Go for a Snowflake clone?**

**Answer: Go for the platform, Python for the data/UDF layer, Node only for the
web console. One language only → Go.**

A Snowflake clone is a distributed-systems problem before it is a database
problem. The parts that dominate the work — coordinator, metadata/transaction
service, storage↔compute separation, cluster scheduling, RBAC, result caching,
the REST/gRPC API — are exactly Go's strengths. Almost every modern system in
this space is Go: CockroachDB, TiDB coordination, InfluxDB, Vitess, Thanos, M3,
Loki. Static binary, strong concurrency, predictable memory, first-class gRPC.

- **Python** can't hold the hot query path (GIL, latency) — but it *is* where
  the data ecosystem lives, so it owns client drivers, the UDF/UDTF runtime and
  the "Snowpark" equivalent.
- **Node** is worse than Go for CPU-bound fan-out and memory pressure. It earns
  a place only for the web console (React/Next).

### You do not write the execution engine in any of the three

Snowflake's real engine is hand-tuned vectorized C++. Don't reimplement that in
Go. **Embed an existing columnar engine:**

| Option | Notes |
|---|---|
| **DuckDB** (CGo) | Columnar, vectorized, reads Parquet directly. Fastest path to "it works". Default choice for Phase 2. |
| **Apache DataFusion / Arrow** (Rust) | If a distributed, extensible engine is wanted; call over FFI or as a sidecar service. |
| Hand-rolled Go operators | Only for trivial pushdown (COUNT, projection) at the coordinator. Not a general engine. |

Storage = Parquet objects on S3/GCS/Azure Blob + our own metadata catalog.
That *is* the storage/compute split.

### Per-component language

| Component | Language |
|---|---|
| Coordinator, catalog, txn/metadata, cluster manager, API | **Go** |
| SQL parser / planner / optimizer | **Go** (or reuse DataFusion's) |
| Execution workers | thin **Go** around embedded **DuckDB / DataFusion** |
| Client drivers, UDF/UDTF runtime, Snowpark-equivalent | **Python** |
| Web console | **Node** (React/Next) |

The language is the easy call. The optimizer and the metadata transaction layer
are where clones die.

---

## 2. Architecture

```
                    ┌────────────────────────────────────────┐
   clients ───────► │  API / SQL REST  (Go, internal/server)  │
   (driver, console)└───────────────┬────────────────────────┘
                                    │
                    ┌───────────────▼────────────────┐
                    │  Coordinator  (Go)             │
                    │  parse → plan → optimize →     │
                    │  schedule → gather results     │
                    └───┬───────────────┬────────────┘
             metadata    │               │  fan-out work
        ┌────────────────▼──┐        ┌───▼───────────────────────┐
        │  Catalog (Go)     │        │  Virtual Warehouses (Go)  │
        │  db/schema/table, │        │  elastic worker pools;    │
        │  micro-partition  │        │  each worker = Go +       │
        │  stats, versions  │        │  embedded DuckDB engine   │
        │  → Postgres       │        └───┬───────────────────────┘
        └──────────────────┘             │ read/write objects
                                 ┌───────▼───────────────────────┐
                                 │  Storage (Go, internal/storage)│
                                 │  Parquet micro-partitions on   │
                                 │  local FS / S3 / GCS / Azure    │
                                 └────────────────────────────────┘

  cross-cutting (reuse GoAdmin gateway): identity, RBAC, hash-chained audit,
  GoSecrets for credentials, config hot-reload.
```

Core principle (Snowflake's): **storage is cheap and shared; compute is
ephemeral, per-warehouse, and independently scaled.** A warehouse can be
resized or suspended without touching a byte of data. Many warehouses read the
same tables with zero contention.

---

## 3. Phased roadmap

### Phase 0 — walking skeleton ✅ (in this repo)

A compiling, tested `gosnowd` that wires the real component boundaries with
in-memory implementations.

- `cmd/gosnowd` — HTTP server, graceful shutdown, `-addr` / `-data` flags.
- `internal/server` — REST API: `GET /healthz`, `GET /v1/version`,
  `POST /v1/statements`, `GET /v1/databases`, `GET/POST /v1/warehouses`.
- `internal/catalog` — `Catalog` interface + in-memory tree (db → schema →
  table). Stand-in for the Postgres catalog.
- `internal/storage` — `Store` interface + `MemStore`, `LocalStore`, and
  **`S3Store`** (real, any S3-compatible endpoint: MinIO, AWS, R2, Spaces, B2 —
  via the same `minio-go` client `GoAdmin/gofile` uses). GCS/Azure still to come.
- `internal/warehouse` — `Manager` tracking named compute pools and their
  state (`suspended` / `running`).
- `internal/query` — `Engine` interface + `Coordinator`, a toy engine that
  runs `SELECT <literal>`, `CREATE DATABASE`, `CREATE SCHEMA`, `SHOW
  DATABASES/SCHEMAS`, and rejects everything else with a pointer to this file.

Run: `cd GoSnow && go test ./... && go run ./cmd/gosnowd`

### Phase 1 — durable metadata + real object storage

- Postgres-backed `catalog.Catalog` (same instance/pattern as GoAdmin 2.0's
  `goadmin` schema — one connection, `gosnow` schema). Migrations.
- Table = ordered set of **micro-partitions** (immutable Parquet objects) +
  per-partition column min/max/null stats for pruning.
- `storage.Store`: ~~real `S3Store`~~ **done** (`minio-go`, works with
  MinIO/AWS/R2/Spaces/B2); still to add `GCSStore`, `AzureBlobStore`.
  `LocalStore` stays for dev/tests.
- Snapshot isolation on the catalog: every write creates a new table version
  (list of partition ids); readers pin a version. This is Time Travel's
  foundation.

### Phase 2 — real execution engine

- Embed **DuckDB** via CGo in the worker binary (`internal/engine/duckdb`).
- Coordinator plan: prune micro-partitions by stats → hand each worker a set
  of Parquet object keys + a pushed-down filter/projection → workers stream
  Arrow batches back → coordinator merges/aggregates.
- `SELECT` (scans, filters, joins, group-by, order-by, window), `INSERT`,
  `COPY INTO` (bulk load from staged files), `CREATE TABLE AS`.
- Result set caching keyed by (sql, table-versions).

### Phase 3 — virtual warehouses (elastic compute)

- Warehouse = a pool of worker processes. Local mode: subprocesses. Cloud
  mode: a Kubernetes Deployment per warehouse (reuse `GoPlatform` k8s know-how).
- `CREATE/ALTER/DROP WAREHOUSE`, `RESUME`/`SUSPEND`, auto-suspend after idle,
  auto-resume on query.
- Sizes (`x-small`…`4x-large`) = worker count / CPU. Multi-cluster warehouse =
  add clusters under queue pressure.
- Per-warehouse credit metering → usage table → billing view.

### Phase 4 — SQL language surface + drivers

- Full parser/planner in Go (or vendor DataFusion's SQL frontend). Information
  schema. `MERGE`, CTEs, `QUALIFY`, semi-structured (`VARIANT`, `:` path,
  `FLATTEN`).
- **Python driver** (`gosnow-connector`, DBAPI 2.0 + SQLAlchemy dialect) and a
  Go driver (`database/sql`).
- Snowflake-compatible SQL REST API shape so existing tools mostly "just work".

### Phase 5 — governance (mostly reuse, not rebuild)

- Mount GoSnow behind the **GoAdmin gateway** and inherit:
  - identity via `gobase_session` → `/api/auth/me`,
  - `(role, action, resource)` RBAC at the gateway,
  - hash-chained, tamper-evident audit log,
  - `GoSecrets` for object-store credentials (no keys in config).
- Add data-plane grants: database/schema/table/column, row-access policies,
  masking policies, secure views.

### Phase 6 — data sharing & marketplace

- Share = a named, read-only grant of specific secure views/tables to another
  account. Zero-copy: consumer queries the provider's storage versions
  directly, billed on the consumer's warehouse.
- Reader accounts for consumers without their own GoSnow.
- Listing catalog (the "marketplace").

### Phase 7 — ingestion & pipelines

- `STAGE` objects (external S3/GCS location or internal).
- Auto-ingest ("Snowpipe"): object-created event → append micro-partitions.
- `STREAM` (CDC offset over a table) + `TASK` (scheduled/triggered SQL DAG).
- Kafka connector — the streaming path already sketched for `crm3-micro`
  (`F:\leadMarketing\crm_pipeline`), landed here instead of BigQuery.

### Phase 8 — ML & functions

- Python **UDF/UDTF** runtime: sandboxed worker (gVisor / firecracker /
  subprocess with seccomp), Arrow in/out.
- Snowpark-style dataframe API (Python) that compiles to GoSnow SQL plans.
- `CREATE MODEL` / `PREDICT` thin wrapper over the existing PyTorch work in
  `F:\dataScience`.

### Phase 9 — web console (Node)

- SQL worksheet, result grid, query history/profile, warehouse monitor,
  catalog browser, role/grant editor, usage dashboards.
- Served as an SPA behind the gateway, same as gofile/gobase UIs.

---

## 4. What to reuse from V5 (don't rebuild)

| Need | Reuse |
|---|---|
| Reverse proxy, one origin, identity | `GoAdmin/gateway` |
| RBAC engine `(role, action, resource)` | `GoAdmin/gateway/internal/rbac` |
| Tamper-evident audit log (hash chain) | `GoAdmin/gateway/internal/auditlog` |
| Secrets at rest (AES-256-GCM, PG) | `GoAdmin/gateway/internal/secrets` (GoSecrets) |
| Config hot-reload pattern | `GoAdmin` Phase 1.5 |
| k8s / Terraform / observability | `GoPlatform` modules 3–7 |
| JWT / JSON helpers | `GoAdmin/pkg/apikit` |
| Real analytics workloads to test against | `F:\leadMarketing\crm_pipeline`, `F:\dataScience` |

## 5. Explicitly out of scope

- Writing a vectorized columnar engine by hand — embed DuckDB/DataFusion.
- Full ANSI SQL conformance before there is a single real user.
- Multi-region replication / cross-cloud failover — Phase 10+, if ever.
- A billing/payments product — usage metering only, stop there.
- Being bug-for-bug Snowflake-compatible — compatible *enough* for common tools.

## 6. Open questions

1. Embedded DuckDB (CGo, simple) vs DataFusion sidecar (no CGo, more control)?
2. Catalog transactions: plain Postgres row-versioning vs a real MVCC layer?
3. Worker isolation for warehouses locally — subprocess vs container always?
4. Do we need our own Parquet writer or is `parquet-go` / Arrow-Go enough?
5. Python UDF sandbox: subprocess+seccomp (portable) vs firecracker (heavier)?

## 7. Status

- [x] **Phase 0 — walking skeleton** — `gosnowd` builds, `go test ./...` green,
  component seams (catalog / storage / warehouse / query) defined as interfaces
  with in-memory implementations.
- [ ] Phase 1 — Postgres catalog + real object storage + micro-partitions
  - [x] `storage.S3Store` — any S3-compatible endpoint via `minio-go`; selected
    by `gosnowd -s3-endpoint` / `GOSNOW_S3_*`. Integration test gated on
    `GOSNOW_S3_TEST_ENDPOINT` (run against local MinIO).
  - [ ] `GCSStore`, `AzureBlobStore`
  - [ ] Postgres-backed catalog + micro-partition model
- [ ] Phase 2 — embedded execution engine
- [ ] Phase 3 — virtual warehouses
- [ ] Phase 4 — SQL surface + drivers
- [ ] Phase 5 — governance via gateway
- [ ] Phase 6 — data sharing
- [ ] Phase 7 — ingestion & pipelines
- [ ] Phase 8 — ML & UDFs
- [ ] Phase 9 — web console
