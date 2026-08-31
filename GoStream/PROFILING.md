# Profiling GoStream (CoverGo U2)

## Live profiles

`gostreamd` serves `net/http/pprof` on a **separate, private** listener:

```sh
gostreamd -addr :8097 -debug-addr 127.0.0.1:6061 \
          -mutex-profile 5 -block-profile 10000
# or: GOSTREAM_DEBUG_ADDR=127.0.0.1:6061 gostreamd ...
```

```sh
go tool pprof http://127.0.0.1:6061/debug/pprof/mutex     # hub lock contention
go tool pprof http://127.0.0.1:6061/debug/pprof/block      # channel-send stalls
go tool pprof http://127.0.0.1:6061/debug/pprof/heap
go tool pprof http://127.0.0.1:6061/debug/pprof/goroutine  # per-conn goroutine count
```

The mutex and block profiles are the ones that matter here: goroutine-per-
connection plus a shared `hub` map means the `hub.mu` RWMutex and the
per-connection send channels are the two places a broadcast storm can stall.

## Findings — 2026-08-31 (first pass)

**Alloc:** `BenchmarkPublishFanout` — 92% of `alloc_space` is inside
`hub.Publish` itself. It allocates **two slices per call** (`ids []string` and
`targets []*Client`), each sized to the subscriber count — this is the
"2 allocs/op, bytes ∝ N" seen in `bench/latest.txt`.

**Actionable (U7 — GoStream):**
1. drop the `ids` intermediate — one pass over `h.topics[topic]` looking up
   `h.clients[id]` directly;
2. pool the `targets []*Client` slice (`sync.Pool`), or store `map[string]*Client`
   per topic so no client lookup is needed;
3. the second `h.mu.Lock()` (stats bump) can be `sync/atomic` counters.

**Contention:** at 1k subscribers the mutex profile shows negligible real
contention — but the current benchmark's own drain goroutines pollute the
block/mutex profiles. A realistic contention measurement needs the profile
captured against real `ws` connections under a broadcast load generator; that
is the rest of the U2 (GoStream) task.

## U7 result — 2026-08-31

- `hub.Publish` now: one pass (no `ids []string`), a pooled `[]*Client` scratch
  slice, and `sync/atomic` stat counters (no second `h.mu.Lock()`).
- `ws.Conn` reuses one `readBuf` across frames instead of `make` per frame.

| benchmark | B/op before → after | allocs/op |
|---|---|---|
| `PublishFanout/1000` | 24.6 KiB → **4 B** | 2 → **0** |
| `PublishFanout/10000` | 240 KiB → **~0.8 KiB** | 2 → **0** |
| `PublishFanout/50000` | 1176 KiB → 181 KiB | 2 → 2 (pool slice regrows) |
| `PublishAllSlow` (1k) | 24 KiB → **0** | 2 → **0** |

The broadcast path is now allocation-free up to 10k subscribers per topic.
Remaining: `ws.ReadMessage` still allocates the final per-message copy
(`append(nil, payload…)`) — removing it needs an API decision (reuse-until-next-
read semantics), tracked under U7 follow-up.
