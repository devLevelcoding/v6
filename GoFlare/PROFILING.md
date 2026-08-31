# Profiling GoFlare (CoverGo U2)

`goflared` serves `net/http/pprof` on a **separate, private** listener — never
the traffic port:

```sh
goflared -debug-addr 127.0.0.1:6063 -mutex-profile 5 -block-profile 10000
# or: GOFLARE_DEBUG_ADDR=127.0.0.1:6063 goflared ...
```

```sh
go tool pprof http://127.0.0.1:6063/debug/pprof/profile?seconds=30   # CPU
go tool pprof http://127.0.0.1:6063/debug/pprof/heap
go tool pprof http://127.0.0.1:6063/debug/pprof/goroutine
go tool pprof http://127.0.0.1:6063/debug/pprof/mutex
curl -o trace.out 'http://127.0.0.1:6063/debug/pprof/trace?seconds=5' && go tool trace trace.out
```

Offline, from the benchmarks:

```sh
go test -run '^$' -bench . -benchmem -memprofile mem.prof -cpuprofile cpu.prof ./...
go tool pprof -top -sample_index=alloc_space mem.prof
```

## Findings

Pending capture under realistic load — see the per-service U2 task
(GoFlare) in `F:\V5\CoverGo.md`. Baseline allocation counts are in
`bench/latest.txt`.
