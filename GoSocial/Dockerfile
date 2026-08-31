# Main API: auth, posts, follows, feed, messages, GraphQL, feature flags,
# debug/replay, debug/breaker, and the WebSocket notification endpoint.
#
#   docker build -t go-social-v2 .
#   docker run --rm -p 8400:8400 go-social-v2
#
# Uses golang:1.25-alpine because golang.org/x/crypto's go.mod requires
# go >= 1.25.0.

FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/go-social-v2 .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/go-social-v2 .
ENTRYPOINT ["./go-social-v2"]
