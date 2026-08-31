# GoSnow benchmarks (CoverGo U1)

`latest.txt` is the committed baseline. Regression workflow:

```sh
cp bench/latest.txt /tmp/old.txt
# ... make a change to the hot path ...
make bench
go run golang.org/x/perf/cmd/benchstat@latest /tmp/old.txt bench/latest.txt
```

Commit a new `latest.txt` only with a note on why the numbers moved.
Anchors that matter: see the per-service rows in `F:\V5\CoverGo.md`.
