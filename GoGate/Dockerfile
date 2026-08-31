# syntax=docker/dockerfile:1.7
# gogated — GoGate's edge gateway / BFF. Stdlib-only build; the runtime image is
# just the static binary on a minimal base (ca-certificates for upstream TLS).

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gogated ./cmd/gogated

FROM alpine:3.20 AS runner
RUN apk add --no-cache ca-certificates && adduser -S -D gogate
USER gogate
COPY --from=build /out/gogated /usr/local/bin/gogated
ENV GOGATE_ADDR=:8090
EXPOSE 8090
ENTRYPOINT ["gogated"]
