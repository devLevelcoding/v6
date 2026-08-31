# GoStream

A from-scratch **WebSocket fan-out service** in Go — clients subscribe to topics
over `/ws`, anything `POST`ed to `/pub/{topic}` (or published by a permitted
client) is delivered to that topic's subscribers, and a **slow subscriber is
dropped, not waited on**.

It exists to hold real connection counts. Socket.io / Node caps a single
instance around 10–20k concurrent connections before the event loop lags, at
~30–50 KiB each. A goroutine-per-connection Go server on `epoll` holds
100k–500k at ~2–10 KiB each, with flat p99 under a broadcast storm — which is
what ShopFloor3D's live overlays, crm3-micro's `chat-svc`, and MobileTasks'
push updates all want.

Full plan and rationale: [`future.md`](future.md).

## Status: Phase 0 — walking skeleton

`gostreamd` compiles, `go test ./...` is green (including a real
client↔server WebSocket round-trip), and the chain runs end to end:
**connect → subscribe → publish (HTTP or client) → fan-out → slow-consumer
eviction**, plus a `/_gostream` control-plane API with live **presence**.

The WebSocket layer (`internal/ws`) is a hand-written RFC 6455 server — no
`gorilla/websocket`, no `coder/websocket`, **zero external dependencies**. It
does the handshake, text/binary messages, fragmented-message reassembly,
transparent ping/pong and the close handshake. No `permessage-deflate`, no
fragmented sends.

Everything is in-memory and single-instance: the hub, presence and topic index
don't survive a restart, and there's no cross-instance fan-out. Redis/NATS
fan-out, JWT identity, a gRPC publish path, backpressure signalling and
horizontal scale-out are later phases — see `future.md` §3.

## Layout

| Path | Role |
|---|---|
| `cmd/gostreamd` | server entrypoint, flags, graceful shutdown |
| `internal/ws` | hand-rolled RFC 6455: `Upgrade` + a `Conn` (read/write messages, auto-pong, close) |
| `internal/hub` | the fan-out core: topic→subscribers index, non-blocking `Publish`, slow-consumer eviction, presence |
| `internal/proto` | the small JSON wire protocol: client `Command`s, server `Event`s, the `message` envelope |
| `internal/ingest` | `POST /pub/{topic}` → `hub.Publish` (optional token auth) |
| `internal/server` | `/ws` + the client protocol loop + `/_gostream` control-plane API |
| `internal/wstest` | a minimal WebSocket *client* for the tests |
| `internal/uid` | random ids over `crypto/rand` |

## Run

```bash
cd GoStream
go test ./...

go run ./cmd/gostreamd                       # :8097, open publish + subscribe
go run ./cmd/gostreamd -client-publish \
  -publish-token "$PUB" -ws-token "$WS" \
  -send-buffer 128 -max-dropped 16
```

Flags (each also an env var — `GOSTREAM_ADDR`, `GOSTREAM_SEND_BUFFER`,
`GOSTREAM_MAX_DROPPED`, `GOSTREAM_PUBLISH_TOKEN`, `GOSTREAM_WS_TOKEN`,
`GOSTREAM_CLIENT_PUBLISH`, `GOSTREAM_IDLE_TIMEOUT`, `GOSTREAM_PING_INTERVAL`,
`GOSTREAM_READ_LIMIT`):

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8097` | listen address |
| `-send-buffer` | `64` | queued messages per connection before drops start |
| `-max-dropped` | `32` | drops tolerated before a slow client is evicted |
| `-publish-token` | _(none)_ | token for `POST /pub` (bearer or `?token=`); empty = open |
| `-ws-token` | _(none)_ | token to open `/ws`; empty = open |
| `-client-publish` | `false` | let socket clients publish, not only subscribe |
| `-idle-timeout` | `75s` | drop a socket silent this long |
| `-ping-interval` | `30s` | server→client ping cadence |

## Try it

```bash
curl localhost:8097/_gostream/healthz
curl localhost:8097/_gostream/stats        # clients, topics, published/delivered/dropped
curl localhost:8097/_gostream/presence     # every connection; ?topic=room to filter

# publish (any JSON value is the payload)
curl -X POST localhost:8097/pub/room -d '{"user":"ana","text":"hi"}'
```

Browser client:

```js
const ws = new WebSocket("ws://localhost:8097/ws?topics=room");
ws.onmessage = e => {
  const ev = JSON.parse(e.data);          // {type:"message", topic:"room", data:{...}, ts}
  if (ev.type === "message") render(ev.data);
};
ws.onopen = () => ws.send(JSON.stringify({ type: "subscribe", topic: "presence" }));
```

### Wire protocol

Client → server (`Command`, one JSON object per text frame):

| `type` | fields | effect |
|---|---|---|
| `subscribe` | `topic` | join a topic; server replies `{"type":"subscribed","topic"}` |
| `unsubscribe` | `topic` | leave a topic |
| `publish` | `topic`, `data` | fan out (only if `-client-publish`) |
| `ping` | — | server replies `{"type":"pong"}` |

Server → client (`Event`): `welcome` (with your `id`), `message`
(`topic` + `data` + `ts`), `subscribed` / `unsubscribed`, `pong`, `error`.

### Control-plane API (under `/_gostream`)

| Method + path | Purpose |
|---|---|
| `GET /_gostream/healthz`, `/version` | liveness, build version |
| `GET /_gostream/stats` | client / topic counts, published / delivered / dropped totals |
| `GET /_gostream/presence?topic=` | connected clients (id, subject, remote addr, topics, since) |
| `POST /pub/{topic}` | publish; returns `{"delivered":N,"dropped":M}` |
