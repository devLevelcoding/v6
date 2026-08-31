# GoSnow — dev + measurement targets (CoverGo S0).
BIN        := gosnowd
CMD        := ./cmd/gosnowd
FUZZTIME   ?= 30s
BENCHDIR   := bench

# GoSnow is CGO-free TODAY. CoverGo U10 embeds DuckDB (cgo) and will flip this
# to CGO_ENABLED=1 + a glibc/distroless base — see internal/query when it lands.
CGO_ENABLED ?= 0

.PHONY: test race vet bench fuzz build ci tidy

test:
	go test ./...

race:            ## race detector (CoverGo U5) — needs CGO; runs in CI on linux
	CGO_ENABLED=1 go test -race -count=1 ./...

vet:
	go vet ./...

bench:           ## benchstat baseline (CoverGo U1) — catalog + query engine
	@mkdir -p $(BENCHDIR)
	go test -run '^$$' -bench . -benchmem ./... | tee $(BENCHDIR)/latest.txt

fuzz:            ## fuzz the SQL-ish statement parser (CoverGo U23)
	go test -run '^$$' -fuzz 'FuzzExecute$$' -fuzztime $(FUZZTIME) ./internal/query

build:           ## static binary while CGO-free; U10 changes this
	CGO_ENABLED=$(CGO_ENABLED) go build -trimpath -ldflags '-s -w' -o dist/$(BIN) $(CMD)

ci: vet race bench

tidy:
	go mod tidy
