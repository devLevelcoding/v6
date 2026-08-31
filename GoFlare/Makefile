# GoFlare — dev + measurement targets (CoverGo S0).
BIN        := goflared
CMD        := ./cmd/goflared
FUZZTIME   ?= 30s
BENCHDIR   := bench

.PHONY: test race vet bench fuzz build ci tidy

test:
	go test ./...

race:            ## race detector (CoverGo U5) — needs CGO; runs in CI on linux
	CGO_ENABLED=1 go test -race -count=1 ./...

vet:
	go vet ./...

bench:           ## benchstat baseline (CoverGo U1) — ingest pipeline + fingerprint
	@mkdir -p $(BENCHDIR)
	go test -run '^$$' -bench . -benchmem ./... | tee $(BENCHDIR)/latest.txt

fuzz:            ## fuzz the event envelope + payload decoders (CoverGo U23)
	go test -run '^$$' -fuzz 'FuzzParseEnvelope$$' -fuzztime $(FUZZTIME) ./internal/event
	go test -run '^$$' -fuzz 'FuzzDecode$$'        -fuzztime $(FUZZTIME) ./internal/event

build:           ## static, stripped binary (CoverGo U22)
	CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o dist/$(BIN) $(CMD)

ci: vet race bench

tidy:
	go mod tidy
