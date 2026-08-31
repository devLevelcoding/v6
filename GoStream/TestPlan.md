# GoStream — Test Plan

## Automated (present)

`cd GoStream && go build ./... && go vet ./... && go test ./...`

- `internal/uid` — 1000 draws are 32-char lowercase hex and unique.
- `internal/ws` — against a real echo server driven by the `wstest` client:
  handshake + text echo at 0 / 5 / 5000 bytes (exercises the 7-bit and 16-bit
  length encodings); a plain `GET` is not upgraded; a client ping is answered
  transparently (a message sent after it still echoes); an **unmasked client
  frame closes the connection**; a message over the server's `SetReadLimit`
  closes it (1009 path).
- `internal/hub` — subscribe/publish is topic-isolated and a publish to an
  empty topic delivers 0; unsubscribe and `Remove` drop the client from every
  topic and kill it; a **persistently slow consumer** (buffer 2, `MaxDropped`
  3) accumulates drops and is then evicted with a recorded reason;
  `Presence` lists every client with its topics, `Stats` counts
  clients/topics/published/delivered.
- `internal/proto` — `Message` passes valid JSON through untouched, wraps
  non-JSON as a JSON string, and emits `"null"` for an empty payload;
  `Welcome` / `Ack` / `Pong` / `Errorf` have the right `type` and fields.
- `internal/ingest` — `POST /pub/{topic}` delivers the wrapped envelope
  (`"topic"` + the body) to a subscriber and returns the delivered count;
  token auth accepts `?token=` and `Authorization: Bearer`, rejects a missing
  token with `401`; an oversized body → `413`.
- `internal/server` — end to end over an `httptest` server and the `wstest`
  client: connect → `welcome` → `subscribe` → `subscribed` → an HTTP
  `POST /pub/room` arrives as a `message` with the exact payload;
  `?topics=a,b` auto-subscribes; a client `publish` is refused with an `error`
  when `-client-publish` is off and **relayed to another socket** when it is on;
  `-ws-token` rejects a token-less handshake and accepts `?token=`;
  `/_gostream/{healthz,stats,presence?topic=}` return the right shape; a
  disconnect removes the client from the hub and empties its topics.

Race detector (`go test -race ./...`) needs a C toolchain — run it in CI. The
hub fan-out and the server read/write loops are the parts that need it.

## Manual smoke

```bash
go run ./cmd/gostreamd -client-publish &
curl -s localhost:8097/_gostream/healthz
# browser console:
#   ws = new WebSocket("ws://localhost:8097/ws?topics=room")
#   ws.onmessage = e => console.log(JSON.parse(e.data))
curl -X POST localhost:8097/pub/room -d '{"hi":true}'   # the browser logs it
curl -s localhost:8097/_gostream/presence
curl -s localhost:8097/_gostream/stats
```

## Not yet covered (needs a real load rig / CI)

- Connection density: 100k idle connections in ~1 GiB, p99 write latency flat
  during a broadcast to all of them.
- Slow-consumer eviction under a genuine broadcast storm (not a synthetic
  buffer-fill).
- Autobahn WebSocket protocol test suite against `internal/ws` (fragmentation
  edge cases, UTF-8 validation, close-code handling).

## Automated (to add as phases land)

- **Phase 1**: a broker message becomes a `hub.Publish`; a new subscriber to a
  topic with a retained message gets it immediately; `/metrics` counters move.
- **Phase 2**: a connection with no `subscribe` scope is refused the topic; a
  publish without `publish` scope is refused.
- **Phase 3**: two instances over a bus — a subscriber on instance A gets a
  message published to instance B; presence is the union.
- **Phase 4**: a client that reconnects within the replay window receives the
  messages it missed, in order; an at-least-once topic redelivers on a missing ack.
- **Phase 7**: RBAC on `POST /_gostream/...`; one audit entry per admin action.
