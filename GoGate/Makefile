# GoGate — dev + measurement targets (CoverGo S0).
BIN        := gogated
CMD        := ./cmd/gogated
FUZZTIME   ?= 30s
BENCHDIR   := bench

.PHONY: test race vet bench fuzz build ci tidy

test:            ## unit tests
	go test ./...

race:            ## race detector (CoverGo U5) — needs CGO (a C toolchain); runs in CI on linux
	CGO_ENABLED=1 go test -race -count=1 ./...

vet:
	go vet ./...

bench:           ## record a benchstat baseline (CoverGo U1)
	@mkdir -p $(BENCHDIR)
	go test -run '^$$' -bench . -benchmem ./... | tee $(BENCHDIR)/latest.txt

fuzz:            ## short fuzz sweep of the hand-rolled parsers (CoverGo U23)
	go test -run '^$$' -fuzz 'FuzzHS256Verify$$'  -fuzztime $(FUZZTIME) ./internal/auth
	go test -run '^$$' -fuzz 'FuzzMatch$$'        -fuzztime $(FUZZTIME) ./internal/route

build:           ## static, stripped binary (CoverGo U22)
	CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o dist/$(BIN) $(CMD)

ci: vet race bench

tidy:
	go mod tidy
