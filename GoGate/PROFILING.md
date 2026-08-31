# Profiling GoGate (CoverGo U2)

## Live profiles

`gogated` serves `net/http/pprof` on a **separate, private** listener — never the
traffic port. Enable it:

```sh
gogated -addr :8090 -debug-addr 127.0.0.1:6060 \
        -mutex-profile 5 -block-profile 10000   # optional: contention sampling
# or: GOGATE_DEBUG_ADDR=127.0.0.1:6060 gogated ...
```

```sh
go tool pprof   http://127.0.0.1:6060/debug/pprof/heap
go tool pprof   http://127.0.0.1:6060/debug/pprof/profile?seconds=30   # CPU
go tool pprof   http://127.0.0.1:6060/debug/pprof/mutex
go tool pprof   http://127.0.0.1:6060/debug/pprof/block
go tool pprof   http://127.0.0.1:6060/debug/pprof/allocs
curl -o trace.out 'http://127.0.0.1:6060/debug/pprof/trace?seconds=5' && go tool trace trace.out
```

Mutex/block profiling has a small always-on cost — leave the rates at 0 in
production unless chasing a specific contention problem.

## Offline (from the benchmarks)

```sh
go test -run '^$' -bench BenchmarkGatewayProxyAuth -benchmem \
        -memprofile mem.prof -cpuprofile cpu.prof ./internal/server
go tool pprof -top -sample_index=alloc_space mem.prof
```

## Findings — 2026-08-31 (first pass, `BenchmarkGatewayProxyAuth`)

`alloc_space`, proxy + JWT path:

| where | share | note |
|---|---:|---|
| `httputil.ReverseProxy.copyBuffer` | **65%** | 32 KB response-copy buffer allocated per request |
| `bufio.NewReaderSize` | 9% | per-connection upstream reader |
| `net/http.Header.Clone` + `MIMEHeader.Set` | 5% | header copy into the outbound request |
| JWT verify (`internal/auth`) | ~5% | 36 allocs/op — see `auth/bench_test.go` |

**Actionable:** the dominant cost is `ReverseProxy.copyBuffer`. `httputil.
ReverseProxy` has a `BufferPool` field for exactly this — wiring a `sync.Pool`
of 32 KB buffers removes the #1 allocator. Tracked as **U7 (GoGate)** and
**P9 (GoGate — full ReverseProxy config)**.

## U7 result — 2026-08-31

`proxy.Pool` now sets `ReverseProxy.BufferPool` (a `sync.Pool` of 32 KiB
buffers), and the cache-miss `capturingWriter` is pooled too.

| benchmark | B/op before → after | allocs/op |
|---|---|---|
| `GatewayProxyPlain` | 45.8 KiB → **13.8 KiB** (−70%) | 111 → 110 |
| `GatewayProxyAuth` | 48.4 KiB → **16.4 KiB** (−66%) | 161 → 160 |
| `GatewayProxyRateLimited` | 45.8 KiB → **13.8 KiB** | 110 → 110 |

geomean B/op −58%. sec/op improved slightly (less GC pressure); allocs/op
barely moves — as expected, we replaced a few *large* allocations with pooled
reuse. `bench/latest.txt` regenerated at `-count=6`.

## U17 — 2026-08-31

`internal/cache` now coalesces cache-miss stampedes with
`golang.org/x/sync/singleflight` instead of a hand-rolled `call`/`WaitGroup`.
Beyond the idiom: the old version left the `WaitGroup` un-`Done`d and the key
stuck if `fill()` panicked — coalesced waiters deadlocked forever.
`singleflight` recovers the panic and re-raises it in every waiter
(`TestFillPanicDoesNotWedge`). `TestCoalesce` still proves 20 concurrent misses
→ 1 upstream fill. `GatewayCacheHit` 4.2µs → 3.2µs (hit path lost the extra
map check).
