# CoverGo — Go concept coverage across V5

**Status file · last updated 2026-08-31**

What the V5 Go estate (`GoFlare` · `GoGate` · `GoStream` · `GoDoc` · `GoRender` ·
`GoUptime` · `GoSnow` · `GoAdmin` · `GoPlatform`) exercises well, what it touches
only lightly (**PARTIAL**), and what it never touches (**UNCOVERED**) — plus,
for each gap, the project that should adopt it and why it fits.

This is a working checklist. Tick a box when a concept lands in a real service
(not a `GoPlatform` day exercise) with a test.

**Code:** `github.com/devLevelcoding/v6` (branch `main`) — a monorepo at `F:\V5`
holding the 5 services + this file. Commit + push after every batch.

**Tracked in LaraCRM:** project **22** · epic **37** ("Go Concept Coverage — 5 V5
services") · 25 sprints (one per concept) · 98 tasks · progress task **272**.
Spec: `F:\V5\GoAdmin\laracrmctl\sprints\covergo-concepts.yaml` — edit + re-run
`laracrmctl sync-db sprints/covergo-concepts.yaml` to add work (idempotent via
`covergo-concepts.state.json`). https://laracrm.levelcoding.com/project/22

### Progress log

**2026-08-31 — S0 (CI + measurement foundation) landed for all 5 services.**
Each of GoGate/GoStream/GoRender/GoFlare/GoSnow now has: a `Makefile`
(`test`/`race`/`vet`/`bench`/`fuzz`/`build`), `.github/workflows/ci.yml`
(vet → build → `go test -race` → `CGO_ENABLED=0` static binary → bench smoke;
plus a fuzz job), and a committed `bench/latest.txt` baseline. `-race` runs in
CI on linux (needs a C toolchain — not runnable on the Windows dev box).
Fuzz targets, no crashers found:

| service | fuzz target(s) | bench baseline anchor |
|---|---|---|
| GoGate | `auth.FuzzHS256Verify`, `route.FuzzMatch` | `HS256Verify` 2.9µs · 1680 B · 36 allocs |
| GoStream | `ws.FuzzReadMessage` (RFC 6455) | `PublishFanout` (1k subs) 273µs · 24.6 KB · 2 allocs |
| GoRender | — (no parser) | `BuildSlideshow` 12.4µs · 14 KB · 155 allocs |
| GoFlare | `event.FuzzParseEnvelope`, `event.FuzzDecode` | `Decode` 7.0µs · 3016 B · 52 allocs |
| GoSnow | `query.FuzzExecute` | `ExecuteShow` 248 ns · 200 B · 7 allocs |

Status: **U5** (race) — harness done, wired to CI. **U22** (static build) — done
for all 5 (GoSnow keeps a `CGO_ENABLED` toggle for the U10 DuckDB flip).
**U23** (fuzzing) — targets + CI jobs done; U23 sprint remains for the longer
nightly runs + any crashers.

**2026-08-31 — U1 (benchmarks) hot-path coverage landed for all 5.**
`bench/latest.txt` committed per repo, `bench/README.md` documents the
`benchstat old new` workflow, `make bench` regenerates. Full test suite green
everywhere. Baseline numbers (300ms/op, i7-11800H):

| service | benchmark | ns/op | B/op | allocs/op | note |
|---|---|---|---|---|---|
| GoGate | `GatewayProxyPlain` | 93k | 46.9 KB | 111 | loopback TCP dominates; ~11k rps/core |
| GoGate | `GatewayProxyAuth` | 102k | 49.6 KB | 161 | +50 allocs for JWT verify → U7 target |
| GoGate | `GatewayCacheHit` | 4.2k | 6.4 KB | 27 | 22× faster than proxy — cache path works |
| GoStream | `PublishFanout` | ~250 ns × N | 24 B × N | **2** | linear, alloc scales with N → U7 `sync.Pool` |
| GoStream | `PublishAllSlow` (1k) | 75k | 24.5 KB | 2 | drop path 3× cheaper than deliver |
| GoStream | `SubscribeChurn` | 895 | 1 KB | 9 | connect/disconnect path |
| GoRender | `BuildSlideshow/40img` | 41k | 56 KB | 497 | plan compile; allocs ∝ images → U7/U8 |
| GoRender | `PushClaimSerial` | 56 | 0 | 0 | queue hand-off |
| GoFlare | `Decode` | 7.1k | 3 KB | 52 | envelope→event decode → U7/U21 target |
| GoFlare | `IngestSameIssue` | 727 | 1 KB | 12 | steady-state dedup+fold |
| GoFlare | `Fingerprint` | 389 | 424 B | 9 | |
| GoSnow | `TableLookup` | 72 | 0 | 0 | in-mem catalog — the U10 "before" |
| GoSnow | `ExecuteShow` | 233 | 200 B | 7 | statement path — the U10 "before" |

Remaining U1: wire `benchstat` into CI as a soft gate; add a keepalive/parallel
variant of the GoGate proxy bench; RAM-per-connection measurement for GoStream.

**2026-08-31 — U2 (profiling) foundation landed for all 5.**
New `internal/debug` package in each repo: `net/http/pprof` on a **separate,
private** listener (`-debug-addr` / `GO<X>_DEBUG_ADDR`, off by default),
`-mutex-profile` / `-block-profile` flags → `runtime.SetMutexProfileFraction` /
`SetBlockProfileRate`. Each repo has a `PROFILING.md` with the `go tool pprof`
commands. First-pass findings from the U1 benchmarks:

- **GoGate**: 65% of proxy-path `alloc_space` is `httputil.ReverseProxy.
  copyBuffer` (the 32 KB per-request response buffer). `ReverseProxy.BufferPool`
  + a `sync.Pool` removes it → **U7 (GoGate)** / **P9 (GoGate)**.
- **GoStream**: 92% of `Publish` `alloc_space` is the two per-call slices
  (`ids []string`, `targets []*Client`, both ∝ subscriber count). Drop the
  `ids` pass + pool `targets` + atomic stats counters → **U7 (GoStream)**.
- GoStream mutex/block profiles need capture under real `ws` load (bench
  harness pollutes them) — rest of the U2 (GoStream) task.

Status: **U2** — debug listener + docs done for all 5; the "capture + analyze
under realistic load" half of each per-service U2 task remains (GoGate alloc
profile is done).

**2026-08-31 — U7 (sync.Pool) landed for GoGate, GoStream, GoFlare.**

| service | change | measured |
|---|---|---|
| **GoGate** | `ReverseProxy.BufferPool` (32 KiB `sync.Pool`) + pooled cache-miss `capturingWriter` | proxy-path **B/op −58% geomean** (46 KiB → 14 KiB); allocs/op ~flat (few large → pooled) |
| **GoStream** | `Publish`: single pass, pooled `[]*Client`, atomic stat counters; `ws.Conn` reuses one `readBuf` | broadcast path **0 allocs/op up to 10k subs** (was 24 KiB/2 allocs at 1k) |
| **GoFlare** | `Item.Payload` aliases the request body (no defensive copy); pooled `rawEvent` decode struct | `Decode` 52 → 51 allocs (the rest is `json.Unmarshal` of nested fields — needs U21 codegen, not U7) |

`bench/latest.txt` regenerated at `-count=6` for the three. PROFILING.md updated
with before/after. Remaining U7: GoStream `ReadMessage` per-message copy (needs
an API decision); GoRender plan-compile allocs (∝ image count).

**2026-08-31 — U16 (errgroup) landed for GoFlare + GoRender.**
Added `golang.org/x/sync` to GoFlare and GoRender (both already had `x/*`
indirect deps; documented tradeoff vs the stdlib-only goal).

- **GoFlare**: `ingest.Pipeline` workers now run under `errgroup.WithContext(ctx)`
  instead of a manual `sync.WaitGroup`. `Wait()` returns the first worker error;
  a dead worker cancels `gctx` so the others wind down. `cmd/goflared` logs it.
- **GoRender**: `plan.buildConcat` probes all clips **concurrently** under
  `errgroup` with `SetLimit(max(2, NumCPU))` — was a serial loop of `ffprobe`
  shell-outs. First bad clip cancels the sibling probes; `TestConcatProbeFailFast`
  covers it.
- **GoSnow**: parallel partition scan is blocked on U10 (no partitions yet).

Status: **U16** — GoFlare + GoRender done with fail-fast tests; GoSnow leg
waits on U10.

**2026-08-31 — U17 (singleflight) done — GoGate.**
`internal/cache` coalesces cache-miss stampedes with `golang.org/x/sync/
singleflight` instead of the hand-rolled `call`/`WaitGroup`. This also fixes a
latent bug: the old version left the `WaitGroup` un-`Done`d and the key stuck
if `fill()` panicked, deadlocking every coalesced waiter. `singleflight`
recovers and re-raises the panic in all waiters (`TestFillPanicDoesNotWedge`).
`TestCoalesce` still proves 20 misses → 1 fill; `GatewayCacheHit` 4.2µs → 3.2µs.
GoGate now depends on `golang.org/x/sync` (was zero-dep) — accepted for the
correctness fix + the roadmap (`future.md` names singleflight explicitly).

---

**2026-08-31 — U24 (testing/synctest) — GoFlare, GoRender, GoGate.**
Replaced sleep-and-poll / timing-guard tests with deterministic `synctest`
bubbles (go 1.26 stable `testing/synctest`):

- **GoFlare** `ingest`: `TestPipelineDrainsToGroupStore` + `TestPipelineFlushesOnShutdown`
  — `synctest.Wait()` instead of a 2s polling loop (now ~0ms, deterministic).
- **GoRender** `queue`: `TestClaimBlocksThenReceives` (no `time.After` guards) +
  `TestPushCtxCancelWhenFull` (now asserts the deadline fires at *exactly* 20ms).
- **GoGate** `cache`: `TestCoalesce` — `synctest.Wait()` replaces "sleep 20ms and
  hope all 20 goroutines piled up".

GoStream leg (ping-loop cadence) needs a `ws.Conn` test seam — left todo.

---

**2026-08-31 — U18 (semaphore), U8 (iterators, partial), U6 (runtime tuning).**

- **U18** — `golang.org/x/sync/semaphore`:
  - **GoRender**: `spec.Weight()` (1–4 by pixel count); the worker pool admits
    jobs against a `semaphore.NewWeighted(CostBudget)` (default `2*N`) so several
    4K renders serialise instead of thrashing the box. `TestPoolCostWeightedAdmission`.
  - **GoGate**: new `internal/inflight` — per-route (`Policy.MaxInFlight`)
    concurrency cap; a request that can't get a slot within a 100ms grace gets
    503 + Retry-After. `TestMaxInFlightReturns503`, `inflight` unit tests.
- **U8** — **GoGate** `route.MemStore.All() iter.Seq[Route]` (+ `List` now
  `slices.Collect(All())`), `TestAllSeqEarlyBreak`. GoFlare/GoSnow legs deferred
  to their streaming boundaries (event history, DuckDB rows — land with U10).
- **U6** — **GoRender** worker default is now `runtime.GOMAXPROCS(0)` (cgroup-
  aware on go 1.25+) not `runtime.NumCPU()`. `debug.LogRuntime` in all 5 logs the
  effective `GOMAXPROCS`/`GOMEMLIMIT`/`GOGC` at boot (the limits themselves are
  read from the env by the Go runtime).

**2026-08-31 — P3 (panic/recover) + U4 (escape analysis).**

- **P3** — **GoRender** `Pool.runGuarded` recovers a panicking render (job fails,
  worker lives — `TestPoolSurvivesJobPanic`); **GoStream** `server.guard` wraps
  both per-connection loops, recovers + kills that client only
  (`TestGuardContainsPanic`). An unrecovered write-goroutine panic used to crash
  the process.
- **U4** — **GoStream** `readFrame`/`writeFrame`: the frame-header arrays
  (`h`/`ext`/`mask`/`head`) were heap-allocating per frame (`go build -gcflags=-m`)
  because their slices were passed to `io.ReadFull`/`conn.Write`. Moved to
  per-`Conn` scratch fields + package-level sentinel errors. `BenchmarkReadFrame`:
  **~0 allocs/frame** steady-state (5 allocs for 4096 frames). Fuzz still clean.

### Session 2 summary (2026-08-31)

Sprints landed: **S0** · **U1** · **U2** · **U7** (GoGate/GoStream/GoFlare) ·
**U16** (GoFlare/GoRender) · **U17** (GoGate) · **U24** (GoFlare/GoRender/GoGate) ·
**U18** (GoRender/GoGate) · **U8** (GoGate) · **U6** (GoRender + all 5) ·
**P3** (GoRender/GoStream) · **U4** (GoStream). All 5 repos `go vet` + `go test`
green throughout. `golang.org/x/sync` added to GoFlare, GoRender, GoGate.

**2026-08-31 — U9 (unsafe), P8 (io composition), P9 (reverse-proxy depth).**

- **U9** — **GoStream** `internal/ws/mask.go`: word-at-a-time WebSocket
  unmasking via `unsafe.Add`, `//go:build !purego` with a plain-Go fallback in
  `mask_purego.go`. `BenchmarkMaskBytes` **205 ns/op (19.9 GB/s)** vs purego
  1595 ns/op (2.6 GB/s) — **7.8×**; `BenchmarkReadFrame` 222µs → 127µs. Fast
  path verified byte-for-byte against a reference impl (all sizes + unaligned
  starts + round-trip); `go vet` clean; fuzz clean; both build tags tested.
- **P8** — **GoRender** `media`: ffmpeg stderr capture is now a **bounded**
  `tailWriter` (8 KiB, was an unbounded `strings.Builder`); with a logger set,
  `io.MultiWriter`/`io.TeeReader` mirror stderr + the `-progress` stream to
  debug logs without the parser losing a line. `slogWriter` splits lines across
  writes. Tested.
- **P9** — **GoGate** `proxy.ForRoute(base, respHeaderTimeout)` — a per-route
  timeout gets its own cloned `*http.Transport`, cached separately;
  `Policy.UpstreamTimeout` wires it. `TestForRoutePerRouteTransport`.
  **GoFlare** edge already had `ModifyResponse`/`ErrorHandler` 5xx capture
  (`TestEdgeCapturesUpstream5xx` / `…Down`) — marked done.

**2026-08-31 — P4 (crypto/tls) — GoGate.**
New `internal/tlsconf`: `Base()` (TLS 1.2 min + pinned 1.2 AEAD suites),
`Server(cert, key, clientCA)` (HTTPS + optional `RequireAndVerifyClientCert`
inbound mTLS), `Upstream(caFile, cert, key)` (CA pinning + optional outbound
mTLS). Wired into `cmd/gogated` — `-tls-cert/-tls-key/-tls-client-ca` for the
listener, `-upstream-ca/-upstream-cert/-upstream-key` for the proxy transport.
`TestMutualTLSEndToEnd` drives a real mTLS handshake against an in-memory CA
(shared-CA setup, verifies both directions + rejects a certless client).
GoFlare edge-TLS leg deferred (needs `tlsconf` duplicated into that module).

**U21** — the GoFlare/GoSnow enums are already `type X string` (self-describing),
so `stringer` doesn't apply; `buf generate` waits on U20. Marked not-applicable
in current form.

**2026-08-31 — P1 (generics) + U19 (structured pipelines).**

- **P1** — **GoGate** `internal/ttlcache` — generic `Cache[K comparable, V any]`
  (map + mutex + per-entry TTL + bounded-size reset), the pattern `cache.Cache`
  was hand-rolling. `cache.Cache` now embeds `ttlcache.Cache[string, *Response]`;
  ~30 lines of duplicated map/lock/expiry logic gone. Both tested (incl. typed
  non-string keys/values).
- **U19** — **GoFlare** `ingest.Pipeline`: documented structured-concurrency
  contract (one bounded stage, non-blocking Submit, ctx-aware workers, defined
  shutdown order) + `TestPipelineNoGoroutineLeak` (synctest — exact goroutine
  count before/after `Wait()`). **GoRender** `worker.run`: a job that fails
  mid-encode now removes its partial `.mp4` (`TestPoolCleansPartialArtifactOnFailure`).
  GoSnow leg blocked on U10.

Still queued: U3 (analysis), U10 (cgo — CI-only), U20 (gRPC — CI-only) · P5, P11 ·
P4 GoFlare · GoStream legs of U8/U24 · GoSnow legs of U8/U9/U16/U19 · P8 GoFlare/GoDoc.

**2026-08-31 — blocker annotations.** LaraCRM's `tasks.status` is a MySQL enum
(`todo|in_progress|review|done`) — no "blocked" value without a schema migration
(and the Kanban board has no column for it). So the 23 blocked/deferred/CI-only
tasks stay `todo` and each carries a comment naming its blocker
(`sprints/covergo-blockers.txt` → `laracrmctl comments-db`). Breakdown: **11
blocked on U10** (307, 312, 313–316, 319, 329, 331, 347) — all need the DuckDB
cgo engine; **4 on U20 codegen** (330, 332, 333, 335); **2 analysis-only**
(292, 293 — trace endpoint ships, need a load generator); **2 need a small
refactor** (344 ws.Conn test seam, 355 tlsconf into the goflare module); the
rest deferred/low-ROI/n-a (308, 336, 349, 361, 362).

---

## Legend

| Mark | Meaning |
|---|---|
| ✅ | Covered deeply — multiple services, idiomatic, tested |
| 🟡 | **PARTIAL** — present but shallow, one file, or demo-only |
| ⬜ | **UNCOVERED** — zero first-party use |
| ⭐ | Roadmap-critical — a `future.md` already depends on this |

---

## Covered already (for reference — not the work)

`net/http` + middleware chains · goroutines / channels / `select` · `sync.Mutex`
/`RWMutex` /`WaitGroup` /`Once` /`atomic` · `context` propagation + cancellation ·
interface seams (mem + Postgres store impls) · `errors` wrapping (`%w`, `Is/As`) ·
`encoding/json` · streaming `encoding/xml` + `encoding/csv` (GoDoc) · `database/sql`
(+ `sqlmock`, dockerized PG) · hand-rolled RFC 6455 (GoStream) · token-bucket
limiter · `httputil.ReverseProxy` · AES-GCM (GoAdmin) · JWT verify (GoGate) ·
`go:embed` · `log/slog` · `os/exec` orchestration · `signal.NotifyContext`
graceful shutdown · `httptest` · table-driven tests · `cmd/` + `internal/` layout.

---

## PARTIAL — present but shallow

| # | Concept | Where it is today | Adopt in | Priority | Notes |
|---|---|---|---|---|---|
| P1 | 🟡 **Generics** (constraints, type sets, generic containers) | 3 trivial helpers (`sortedKeys[V any]`, `jobtrack.Registry[T]`) | GoSnow (typed row/column), GoGate (typed route store), GoFlare (typed store cache) | Med | Also pull in `slices`/`maps`/`cmp` (1 file today) |
| P2 | 🟡 `text/template` / `html/template` | 1 file | GoUptime (status pages), GoFlare (alert bodies), GoDoc | Low | Status pages in `future.md` currently string-built |
| P3 | 🟡 `panic`/`recover` | 5 files, middleware only | Keep contained — add recover to GoRender worker + GoStream conn goroutines so one panic ≠ dead pool | Med | |
| P4 | 🟡 `crypto/tls` / `x509` | 1 file | GoGate + GoFlare edge (mTLS upstream, custom `tls.Config`), GoUptime SSL probe (cert-expiry already planned) | Med | `future.md` for GoGate/GoFlare/GoUptime all name TLS |
| P5 | 🟡 `database/sql` transactions | `Begin`/`BeginTx` in 4 files | GoFlare ingest (atomic issue upsert), GoAdmin RBAC writes, GoUptime incident state | Med | No prepared-stmt reuse, no `LISTEN/NOTIFY` |
| P6 | 🟡 `encoding/binary` / `gob` | 4 files (WS frames) | GoStream (frame fast-path), GoSnow (columnar page format), GoRender (dedup keys) | Low | No custom `MarshalJSON`/`UnmarshalJSON` anywhere (P12) |
| P7 | 🟡 Channel-direction types (`chan<-`, `<-chan`) | 4 files | Make it the default in GoRender queue + GoStream hub public API | Low | Cheap correctness win |
| P8 | 🟡 `io` composition (`io.Pipe`, `TeeReader`, `MultiWriter`) | 1 file | GoDoc (stream doc → response + hash), GoRender (ffmpeg stdout tee → progress + log), GoFlare (capture body without buffering) | Med | |
| P9 | 🟡 `httputil.ReverseProxy` depth | 2 files, basic | GoGate + GoFlare edge: custom `Transport`, `Rewrite`, `ModifyResponse`, error hooks, HTTP/2 | Med ⭐ | Both `future.md` build on the proxy |
| P10 | 🟡 Subtests / `t.Parallel` / golden files | `t.Run` in 10 files | GoDoc (golden XML/PDF), GoSnow (golden query plans) | Low | |
| P11 | 🟡 OTel / Prometheus / raw TCP-UDP | `GoPlatform` days only | Wire real `/metrics` + traces into GoGate, GoStream, GoRender | Med ⭐ | `future.md` observability phases assume this |
| P12 | 🟡 `reflect` | 0 (struct tags + json only) | GoDoc (spec→template field map), GoAdmin connections (row→JSON) — **only where tags can't** | Low | Document where it's deliberately avoided |
| P13 | 🟡 `flag` / CLI structure | 10 files, flat | GoAdmin `gofile`/`laracrmctl` subcommands | Low | No cobra needed; stdlib `flag.NewFlagSet` subcommands |
| P14 | 🟡 Go modules hygiene (`replace`, `go.work`, `retract`) | `replace` once (GoEmail) | Add `go.work` spanning V5 for cross-service local dev | Low | 20+ separate `go.mod`, no workspace |

---

## UNCOVERED — zero first-party use

### Performance engineering (the biggest hole)

| # | Concept | Adopt in | Priority | Why it fits |
|---|---|---|---|---|
| U1 | ⬜⭐ **Benchmarks** (`testing.B`, sub-benchmarks, `benchstat`) | GoGate (6k→90k rps), GoStream (15k→300k conns), GoRender (65s→9s), GoSnow (scan), GoFlare (ingest) | **High** | `featureGo.md`'s entire thesis is unmeasured multipliers. Every service with a perf claim needs `*_bench_test.go`. |
| U2 | ⬜⭐ **Profiling** (`net/http/pprof`, `runtime/pprof`; CPU/heap/mutex/block) | GoGate + GoAdmin (already have `internal/server/admin.go` — mount pprof there), GoStream (mutex/block on hub), GoRender (pool contention) | **High** | One import behind the existing admin listener. |
| U3 | ⬜ Execution tracer (`runtime/trace`) | GoStream fan-out scheduling, GoRender worker contention | Low | Diagnostic; use when U2 points at scheduling. |
| U4 | ⬜ Escape analysis / inlining (`-gcflags=-m`) | GoStream frame encode/decode, GoGate proxy path | Med | Build-time; record findings in each `TestPlan.md`. |
| U5 | ⬜⭐ **Race detector** (`go test -race`) | **ALL** — hubs, caches, registries, schedulers all share state | **High** | There is no CI. Highest value / lowest effort item in this file. Add `-race` to a Makefile + GH Actions. |
| U6 | ⬜ Runtime tuning (`GOMAXPROCS` via automaxprocs, `GOGC`, `GOMEMLIMIT`, `debug.SetGCPercent`) | GoRender (container CPU), GoGate/GoStream (`GOMEMLIMIT` in scale-to-zero), GoSnow (big scans) | Med | `GoPlatform` module 3 (requests/limits) is the teaching ground. |
| U7 | ⬜ `sync.Pool` (alloc reuse) | GoStream (frame + send buffers), GoGate (proxy copy + cache buffers), GoDoc (write buffers), GoFlare (envelope decode) | Med | Direct alloc-reduction on hot paths once U1/U2 exist. |

### Language / runtime surface

| # | Concept | Adopt in | Priority | Why it fits |
|---|---|---|---|---|
| U8 | ⬜ Iterators (`iter.Seq`, range-over-func) | GoSnow (row iteration), GoFlare (event stream), GoUptime (history timeline), GoGate (route store) | Med | `go 1.26` already; API modernization. |
| U9 | ⬜ `unsafe` / `unsafe.Pointer` | GoStream frame unmasking (word-at-a-time XOR), GoSnow columnar buffers | Low | Only if U1 benchmarks justify it. |
| U10 | ⬜⭐ **cgo** | **GoSnow** — `future.md` Phase 2 default is "embed DuckDB (CGo)"; `go.mod` has **no duckdb dep**. Also GoLeads (planned). | **High** | Single most roadmap-blocking gap. Forces `CGO_ENABLED=1` + distroless (U15). |
| U11 | ⬜ Assembly / `//go:noescape` / intrinsics | GoStream masking, GoFlare fingerprint hash | Very low | Consume via deps (`xxhash` already ships asm) — don't hand-author. |
| U12 | ⬜ `plugin` package | (considered for GoUptime probe types, GoGate middleware, GoRender templates) | Very low | Usually the wrong tool — prefer compile-time registration or subprocess. Note the decision, don't build it. |
| U13 | ⬜ `runtime.SetFinalizer` / `weak` pointers (Go 1.24) | GoGate cache (weak-ref eviction), GoStream conn cleanup | Low | Niche; revisit if cache memory shows up in U2. |
| U14 | ⬜ `sync.Cond` | GoRender queue wake, GoStream backpressure | Low | Channels usually preferred — use `sync.Cond` only for broadcast-wake on shared state. |
| U15 | ⬜ Memory model / happens-before (explicit) | Cross-cutting: document per-field ownership + guard in each `TestPlan.md` | Med | Pairs with U5. |

### Concurrency toolkit

| # | Concept | Adopt in | Priority | Why it fits |
|---|---|---|---|---|
| U16 | ⬜⭐ `golang.org/x/sync/errgroup` | GoRender (fan-out, first-error cancel), GoUptime (parallel probes), GoSnow (parallel partition scan), GoFlare pipeline | **High** | Replaces ad-hoc `WaitGroup` + error channel everywhere. |
| U17 | ⬜⭐ `golang.org/x/sync/singleflight` | **GoGate** — `future.md` literally says "`singleflight` coalescing"; current cache is hand-rolled | Med | Swap hand-rolled dedup for the real thing. |
| U18 | ⬜ `golang.org/x/sync/semaphore` | GoRender (bound ffmpeg concurrency), GoGate (per-upstream conn caps) | Med | Weighted bound vs plain buffered channel. |
| U19 | ⬜ Bounded structured concurrency / cancellation-aware pipelines | GoFlare ingest, GoRender, GoSnow query | Med | Beyond the basic worker pool already present. |

### Ecosystem / tooling

| # | Concept | Adopt in | Priority | Why it fits |
|---|---|---|---|---|
| U20 | ⬜⭐ gRPC / protobuf / streaming RPC | **GoSnow** (`future.md`: "first-class gRPC", coordinator↔worker), GoStream (`future.md`: "publish over HTTP/gRPC"), GoAdmin `secretclient` (service-to-service) | **High** | Named in three roadmaps. |
| U21 | ⬜ `//go:generate` + codegen (`protoc`, `stringer`, `sqlc`) | GoSnow (proto stubs, SQL), GoDoc (templates), GoAdmin (RBAC policy), GoFlare (event schema) | Med | Comes with U20. |
| U22 | ⬜⭐ Build tags / `GOOS`×`GOARCH` / static-link / distroless | **ALL** — every `future.md` claims "static binary, drops on any box / scale-to-zero" | **High** | GoSnow+DuckDB (U10) can't be fully static → needs distroless base + a documented build matrix. |
| U23 | ⬜⭐ Fuzzing (`testing.F`) | GoStream WS frame parser, GoGate JWT + route matcher, GoFlare envelope decoder, GoDoc spec decoder, GoSnow query parser | **High** | Every hand-rolled parser is a fuzz target. Cheap to add. |
| U24 | ⬜ `testing/synctest` (Go 1.24+, deterministic time) | GoUptime scheduler, GoGate rate-limiter refill, GoRender queue timeouts, GoStream heartbeat/ping | Med | Kills flaky time-based tests across four services. |

---

## Recommended upgrade order

Ranked by payoff-to-effort, tied to the roadmaps:

1. **U5 race detector + U23 fuzzing + a CI file** — one afternoon, protects every
   service, and there is currently no CI at all. Do this first.
2. **U1 benchmarks + U2 pprof** in **GoGate** and **GoStream** — these have the
   loudest `featureGo.md` claims (15×, 20×) and an existing admin listener to
   mount pprof on. Turns "indicative" into "measured".
3. **U10 cgo + DuckDB in GoSnow** — unblocks the entire GoSnow Phase 2, forces
   **U22** (distroless build matrix) as a side effect.
4. **U16 errgroup + U17 singleflight + U18 semaphore** in **GoRender** and
   **GoGate** — replaces hand-rolled concurrency the roadmaps already flag.
5. **U20 gRPC** in **GoSnow** (coordinator↔worker) then **GoStream** (publish
   ingress) — named in both `future.md` files.
6. **U6 runtime tuning + U7 sync.Pool** — only after U1/U2 give numbers to beat.

---

## Per-project quick view

| Project | Biggest gap it should close | Secondary |
|---|---|---|
| **GoGate** | U1/U2 (prove the 15×), U17 singleflight | U9 proxy depth, U18 |
| **GoStream** | U1/U2 (prove the 20×), U23 fuzz the frame parser | U7 pool, U20 gRPC ingress |
| **GoRender** | U16 errgroup, U18 semaphore, U3 tracer | U6 automaxprocs, U8 |
| **GoSnow** | U10 cgo+DuckDB, U20 gRPC, U21 codegen | U8 iterators, U1 |
| **GoFlare** | U23 fuzz envelope decoder, P9 proxy depth, U11 edge TLS | U16 pipeline |
| **GoUptime** | U24 synctest (scheduler), P4 cert probes, U16 | P2 status page templates |
| **GoDoc** | U23 fuzz spec decoder, P8 io.Pipe, P10 golden files | U7 buffers |
| **GoAdmin** | U2 pprof on admin plane, U5 race in CI | P5 tx, U20 secretclient gRPC |
| **GoPlatform** | (teaching ground) add days for U1, U2, U5, U6 | U22 |
