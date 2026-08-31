# GoRender — dev + measurement targets (CoverGo S0).
BIN        := gorenderd
CMD        := ./cmd/gorenderd
BENCHDIR   := bench

.PHONY: test race vet bench build ci tidy

test:
	go test ./...

race:            ## race detector (CoverGo U5) — needs CGO; runs in CI on linux
	CGO_ENABLED=1 go test -race -count=1 ./...

vet:
	go vet ./...

bench:           ## benchstat baseline (CoverGo U1) — plan compile + queue throughput
	@mkdir -p $(BENCHDIR)
	go test -run '^$$' -bench . -benchmem ./... | tee $(BENCHDIR)/latest.txt

build:           ## static, stripped binary — ffmpeg stays a runtime sidecar (CoverGo U22)
	CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o dist/$(BIN) $(CMD)

ci: vet race bench

tidy:
	go mod tidy
