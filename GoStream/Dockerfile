# syntax=docker/dockerfile:1.7
# gostreamd — GoStream's WebSocket fan-out. Stdlib-only build (hand-rolled
# RFC 6455); the runtime image is just the static binary on scratch.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gostreamd ./cmd/gostreamd

FROM alpine:3.20 AS runner
RUN apk add --no-cache ca-certificates && adduser -S -D gostream
USER gostream
COPY --from=build /out/gostreamd /usr/local/bin/gostreamd
ENV GOSTREAM_ADDR=:8097
EXPOSE 8097
ENTRYPOINT ["gostreamd"]
