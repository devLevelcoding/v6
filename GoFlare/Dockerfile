# syntax=docker/dockerfile:1.7
# goflared — GoFlare's error-tracking core + web console + edge proxy.
# Pure-Go build (lib/pq, minio-go — both CGO-free); the web console is embedded,
# so the runtime image is just the static binary.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/goflared ./cmd/goflared

FROM alpine:3.20 AS runner
RUN apk add --no-cache ca-certificates wget && adduser -S -D goflare
USER goflare
COPY --from=build /out/goflared /usr/local/bin/goflared
ENV GOFLARE_ADDR=:9000
EXPOSE 9000 9001
ENTRYPOINT ["goflared"]
