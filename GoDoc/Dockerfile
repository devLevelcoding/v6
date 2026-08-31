# syntax=docker/dockerfile:1.7
# godocd — GoDoc's document / export service. Pure-Go build (go-pdf/fpdf); the
# runtime image is just the static binary on a minimal base.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/godocd ./cmd/godocd

FROM alpine:3.20 AS runner
RUN apk add --no-cache ca-certificates && adduser -S -D godoc
USER godoc
COPY --from=build /out/godocd /usr/local/bin/godocd
ENV GODOC_ADDR=:8098
EXPOSE 8098
ENTRYPOINT ["godocd"]
