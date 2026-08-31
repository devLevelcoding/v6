# syntax=docker/dockerfile:1.7
# gorenderd — GoRender's API + worker pool. Unlike the other V5 services this
# image is not scratch: ffmpeg + ffprobe are a hard runtime dependency.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gorenderd ./cmd/gorenderd

FROM alpine:3.20 AS runner
RUN apk add --no-cache ca-certificates ffmpeg && adduser -S -D render
USER render
COPY --from=build /out/gorenderd /usr/local/bin/gorenderd
ENV GORENDER_ADDR=:8096 \
    GORENDER_OUT_DIR=/data
VOLUME ["/data"]
EXPOSE 8096
ENTRYPOINT ["gorenderd"]
