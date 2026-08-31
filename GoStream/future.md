# GoStream — Feature Roadmap (V5)

A from-scratch **WebSocket fan-out service** in Go: subscribe by topic, publish
over HTTP/gRPC, deliver to subscribers without blocking on the slow ones, and
scale connection count past what a Node event loop can hold — reusing the
platform layer already built in `GoAdmin` for identity, RBAC, audit and secrets.

This file is the north star. Phase 0 (a compiling, tested walking skeleton) is
in this repo now; everything past it is planned, not built.

---

## 1. Why this, why Go

Three projects on the drive push data to browsers over WebSockets:

- **ShopFloor3D** runs **Socket.io** for operator-position and task overlays on
  a live 3D floor.
- **crm3-micro/chat-svc** carries chat over a WebSocket transport.
- **MobileTasks** wants live task updates pushed to the app.

Socket.io on Node holds ~10–20k concurrent connections per instance before the
single event loop starts adding latency to every message, and each connection
is ~30–50 KiB of heap. A broadcast to all of them is an O(n) loop on that one
thread. Go gives a goroutine per connection (a few KiB of stack, scheduled
across every core), `epoll`-backed I/O for free, and a fan-out that a slow
consumer can't stall — the message is dropped for that consumer, not queued
forever. 100k–500k connections per instance is normal.

The WebSocket protocol itself is small enough to own: `internal/ws` is a
hand-written RFC 6455 server (~250 lines) with no dependency, so the whole
service is standard-library Go.

### What GoStream replaces, concretely

| Today | GoStream |
|---|---|
| `socket.io` server + its rooms | `hub` topics |
| `io.to(room).emit(...)` (event-loop O(n)) | `hub.Publish(topic, msg)` — non-blocking per subscriber |
| a Node instance per ~15k connections | one Go instance per ~300k |
| ad-hoc "is this user online" checks | `/_gostream/presence` |
| the chat-svc bespoke WS transport | `/ws` + the JSON `Command`/`Event` protocol |

---

## 2. Architecture

```
  browsers / apps ──ws──► gostreamd (Go, internal/server)
                           │  Upgrade (internal/ws: hand-rolled RFC 6455)
                           │  read loop  → subscribe / unsubscribe / publish
                           │  write loop ← client.Out() (bounded buffer) + pings
                           ▼
                     internal/hub
                       topic ──► {client, client, …}
                       Publish: for each subscriber, non-blocking enqueue;
                                buffer full → drop + count; too many → evict
                       Presence: id, subject, topics, connected_at
                           ▲
             ┌─────────────┴───────────────┐
   POST /pub/{topic}                gRPC Publish (Phase 1)
   (internal/ingest)                internal Go API

  cross-cutting (reuse GoAdmin): JWT identity on connect, RBAC on publish
  subjects, audit of admin actions, GoSecrets for the tokens.
```

The seams: `hub` deals only in client ids / topic strings / `[]byte`, so the
transport (`ws`) and the publish path (`ingest`) are independent of it; the
per-client buffer + drop policy lives on `hub.Client`; presence is a `hub`
method. Each later phase slots behind one of those.

---

## 3. Phases

### Phase 0 — walking skeleton — **in repo**
`ws` (Upgrade + Conn, RFC 6455, no deps), `hub` (topic index, non-blocking
Publish, slow-consumer eviction, presence, stats), `proto` (JSON
Command/Event), `ingest` (`POST /pub/{topic}` + token auth), `server` (`/ws` +
client protocol + `/_gostream` API), `wstest` (a client for the tests),
`gostreamd`. `go test ./...` green including a real WS round-trip.

### Phase 1 — real publish paths & durability of intent
- **gRPC / internal Go publish API** behind the same `hub` — so crm3 services
  publish without an HTTP hop.
- A **Pub/Sub / NATS consumer** that turns broker messages into `hub.Publish`,
  so existing event streams fan out to sockets with no producer change.
- Optional **last-value cache** per topic (retained message): a new subscriber
  gets the most recent payload immediately.
- Structured access logs + `/metrics` (Prometheus text, stdlib): connections,
  messages/s, drops/s, evictions, p99 write latency.

### Phase 2 — identity & authorization
- JWT verification on connect (reuse the HS256/JWKS work from `GoGate`), claims
  become the connection's `subject`.
- **Per-topic authorization**: a policy (glob → required scope/claim) decides
  who may subscribe and who may publish; `crm3-<tenant>-<room>` style scoping.
- Signed subscribe tokens for capability-style access without a full JWT.

### Phase 3 — horizontal scale-out
- N `gostreamd` instances share fan-out over a bus (Redis pub/sub, NATS, or a
  gossip mesh): a publish on one instance reaches subscribers on all.
- Consistent-hash or sticky routing so a topic's subscribers cluster on fewer
  instances (cheaper cross-instance fan-out).
- Presence becomes cluster-wide (a shared registry with TTL heartbeats).
- Graceful drain: an instance leaving tells clients to reconnect elsewhere.

### Phase 4 — delivery guarantees (opt-in)
- Per-subscription **replay buffer** with an offset/cursor: a client that
  reconnects within a window catches up instead of losing messages.
- At-least-once mode for chosen topics (ack + redelivery), backed by the
  Phase-1 cache / a small log.
- Backpressure signalling: instead of silently dropping, send the client a
  `lag` event so it can decide (resync, slow its render, disconnect).

### Phase 5 — richer messaging
- **Presence as a first-class topic**: `presence:<channel>` emits join/leave
  events and a roster; typing indicators, cursors.
- Request/response over the socket (correlated `id`), so a client RPC and a
  server push share one connection.
- Message filters / server-side transforms per subscription (only send fields
  the client is entitled to).
- Binary framing option (MessagePack / protobuf) for high-rate numeric streams
  (ShopFloor3D operator positions).

### Phase 6 — edge & transport
- SSE fallback endpoint for environments that block WebSockets.
- WebTransport / HTTP/3 datagrams for the lossy-tolerant high-rate case.
- TLS termination with ACME; per-IP connection limits and a handshake rate
  limiter (borrow from `GoGate`).
- Compression (`permessage-deflate`) negotiated per connection.

### Phase 7 — governance (reuse, not rebuild)
- Mount `/_gostream` behind **GoAdmin's gateway**: identity via
  `gobase_session`, RBAC (`stream:topic:publish`, `stream:presence:read`),
  hash-chained audit of admin actions, **GoSecrets** for the publish/WS tokens.
- Per-tenant connection and message quotas; a read-only status page.

### Phase 8 — web console (Node)
- Live topic list with rps / subscriber count, a connection explorer
  (filter by subject / topic / age), a message tap, drop/eviction charts. SPA
  behind the gateway like gofile / GoObserv.

---

## 4. What to reuse from V5 (don't rebuild)

| Need | Reuse |
|---|---|
| JWT / JWKS verification | `GoGate/internal/auth` (lift or share) |
| Connection & handshake rate limiting | `GoGate/internal/ratelimit` |
| TLS, ACME, config hot-reload | `GoAdmin/gateway` |
| RBAC `(role, action, resource)` | `GoAdmin/gateway/internal/rbac` |
| Hash-chained audit | `GoAdmin/gateway/internal/auditlog` |
| Secrets (publish / WS tokens) | `GoAdmin/gateway/internal/secrets` (GoSecrets) |
| Broker → fan-out source | crm3-micro's Pub/Sub subjects |
| Load-testing 100k connections | `GoAdmin/GoLoad` |
| Error capture on a socket panic | `GoFlare` |

## 5. Explicitly out of scope

- A message *queue* (durable, ordered, consumer groups) — GoStream fans out
  live data; use a real broker for durability and let Phase 1 bridge it in.
- Being a chat *product* (threads, reactions, history UI) — chat-svc owns that;
  GoStream is its transport.
- Client SDKs beyond the documented JSON protocol (a thin JS helper at most).
- Its own auth server — it verifies tokens, it doesn't issue them.

## 6. Status

- [x] **Phase 0 — walking skeleton** — `gostreamd` builds, `go test ./...`
  green (real WS round-trip); hand-rolled RFC 6455, in-memory hub, HTTP publish,
  slow-consumer eviction, presence.
- [ ] Phase 1 — gRPC/internal publish, broker consumer, retained message, metrics
- [ ] Phase 2 — JWT identity + per-topic authorization
- [ ] Phase 3 — multi-instance fan-out + cluster-wide presence
- [ ] Phase 4 — replay buffer / at-least-once / lag signalling
- [ ] Phase 5 — presence topics, socket RPC, binary framing
- [ ] Phase 6 — SSE / WebTransport fallback, TLS, connection limits
- [ ] Phase 7 — governance via GoAdmin gateway
- [ ] Phase 8 — web console
