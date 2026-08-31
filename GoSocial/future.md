# GoSocial — Feature Roadmap (V5)

An Instagram-style social backend — register, follow-gated DMs, an event-sourced
feed, posts, stories, and **a collection of reels** — that stops re-implementing
toy versions of the platform and **runs on the five V5 services instead**.

This file is the north star. What's in the repo now (`100-go-social-v2`, copied
in) is Phase 0: four processes, all infrastructure hand-rolled in-process.
Everything past it is planned, not built.

---

## 1. The realisation: GoSocial already is a bad copy of V5

Every hand-rolled piece in GoSocial has a real, standalone, benchmarked
counterpart one directory up.

| GoSocial today (toy, in-process) | V5 service (real, standalone) | What GoSocial 2 does |
|---|---|---|
| `internal/gateway` — plugin router + JWT-auth plugin | **GoGate** — route match, JWT verify, token-bucket limit, singleflight coalescing cache, HTTP↔queue bridge, per-route Transport, mTLS | GoGate is the only front door. Feed responses cached with one upstream fill per stampede (the U17 work); posting rate-limited per user; JWT resolved at the edge and passed downstream as `X-Auth-*`. |
| `internal/notify` — a WebSocket hub | **GoStream** — hand-rolled RFC 6455, topic pub/sub, slow-consumer eviction, ~300k conns/instance, 0-alloc broadcast | Every live surface is a GoStream topic: like counts, comment streams, DM delivery, "typing", presence, **"1.2k watching now" on a reel**. |
| `internal/ratelimit` — one token bucket | GoGate's limiter + `internal/inflight` per-route cap | Per-route rate + per-upstream in-flight cap; a report spike on one reel → shadow-limit *that reel*. |
| `internal/breaker` — generic circuit breaker (with the documented gRPC-error-model gotcha) | GoGate + GoFlare edge | Transport-level failures only; app-level `NotFound` never trips it. |
| **posts are just `{type, content, songId}` strings — no media at all** | **GoRender** — ffmpeg orchestrator: `drawtext` / `concat` / `xfade` / `amix`, SSE progress, content-hash dedup, cost-weighted pool admission (`spec.Weight()`) | **The reels pipeline.** See §3. |
| `internal/events` — in-memory append-only store | **GoSnow** — columnar engine, DuckDB over Parquet micro-partitions in object storage | The event log dumps to Parquet nightly; discovery, trending, and creator analytics are SQL over it. |
| — nothing — | **GoFlare** — error tracking fused with an edge proxy (WAF, rate-limit-a-route) | Captures every API 5xx with the trace id; WAF blocks scripted mass-follow; "this reel is a spike of reports → one click to limit it while you review." |
| — nothing — | **GoDoc** — stateless CSV / XML / PDF | GDPR "download your data" (CSV), creator monthly earnings statement (PDF). |
| — nothing — | **GoUptime** — uptime monitor + dead-route crawler | Public status page; crawls the deployed frontend for dead routes after each deploy. |

GoSocial 2 keeps its **domain** — `internal/social` (users, follow graph,
DMs, the feed projector) — and deletes almost everything else.

---

## 2. Architecture

```
                         ┌────────── GoGate (edge) ──────────┐
   mobile / web  ──TLS──▶ │ JWT · rate-limit · feed cache      │
                         │ WAF hook (GoFlare) · HTTP→queue     │
                         └───┬───────────────┬────────────────┘
                             │ REST/GraphQL  │ WS upgrade
                             ▼               ▼
                     ┌─────────────┐   ┌───────────┐
                     │ GoSocial    │   │ GoStream  │◀── likes/comments/DM/presence
                     │ domain API  │   │  (topics) │     published by the domain API
                     │ (internal/  │   └───────────┘
                     │  social)    │
                     └──┬───┬───┬──┘
              reel job  │   │   │  event append
                        ▼   │   ▼
                  ┌──────────┐ │ ┌────────────────┐
                  │ GoRender │ │ │ event log      │──nightly──▶ Parquet ──▶ GoSnow
                  │ (ffmpeg) │ │ │ (Postgres/WAL) │              (discovery,
                  └────┬─────┘ │ └────────────────┘               trending,
                       │ HLS + poster                             analytics)
                       ▼
                  object store (S3/R2)
```

- **GoSocial domain API** owns the follow graph, the feed, and *publishing* to
  GoStream and GoRender — it never terminates a socket or runs ffmpeg itself.
- **GoStream** never knows what a "reel" is — it moves `topic → bytes`.
- **GoRender** never knows what a "post" is — it compiles a spec to a filtergraph.

---

## 3. The reels pipeline (the headline feature)

### 3.1 Song catalog — a real service
- `GET /songs` → 50 licensed loops, each `{id, title, artist, durationMs, waveformPeaks, previewUrl}`.
- Stored once in object storage; GoRender pulls `songs/<id>.m4a` when it needs it.
- Client picks a song and an in/out trim; the reel spec carries `{songId, songStartMs}`.

### 3.2 Upload → render
```
POST /reels            (multipart: one or more clips + {songId, captions[], trims[]})
  → domain API stores the raw upload, emits ReelUploaded
  → domain API POSTs a GoRender job:
       template: "reel"
       size: 1080x1920 @ 30
       video: concat(clips) with xfade between them
       audio: amix( original at -18dB , song at 0dB , song trimmed to songStartMs )
       text:  drawtext per caption with fade in/out
       out:   HLS ladder (1080/720/480) + poster.jpg
  → GoRender: content-hash the spec — identical re-upload returns the existing artifact free
  → SSE /reels/{id}/progress  streams the ffmpeg -progress fraction to the client
  → on done: domain API emits ReelPublished{ hlsUrl, posterUrl, durationMs }
```
- GoRender's `spec.Weight()` already bounds concurrency by pixel cost — a burst
  of 4K uploads queues instead of thrashing the box.
- Its `tailWriter` + `io.MultiWriter` (the P8 work) keep ffmpeg's stderr bounded
  and mirrored to logs.

### 3.3 Reels feed
- `GET /reels/feed?cursor=` → **70% from people you follow, 30% discovery**.
- The discovery 30% comes from GoSnow (§4); the followed 70% from the feed projector.
- Cached at GoGate per `(userId, cursor)` with `Policy.CacheTTL` + singleflight —
  a celebrity posting doesn't stampede the projector.
- Cursor pagination (identity-anchored), never offset — new reels inserted at the
  head don't skip or repeat rows.

### 3.4 Live reel room
When a client opens a reel it subscribes to GoStream topic `reel:<id>`:
- `like` events → the domain API publishes `{likes: n}` deltas
- `comment` events → streamed live, no refresh
- presence → GoStream's own presence gives "**N watching now**"
- the publisher is always the domain API; viewers are read-only subscribers
  (`AllowClientPublish=false`), so a viewer can't spoof a like count.

### 3.5 Stories (same pipeline, 24h TTL)
- `POST /stories` → same GoRender transcode, output tagged with `expiresAt`.
- "seen by" receipts over GoStream topic `story:<id>:seen`.
- A sweep drops expired story artifacts from the object store.

---

## 4. Discovery, trending, analytics (GoSnow)

Nightly: dump the event log to `events/dt=YYYY-MM-DD/*.parquet`. Then it's SQL.

| Question | Query shape |
|---|---|
| **Discovery slice** (the 30%) | `reels` ranked by `completion_rate * likes_per_view * recency_decay`, excluding ones the viewer follows or has seen |
| **Trending** | `velocity = Δlikes / Δminutes` over the last hour, top 50 |
| **Creator dashboard** | per-reel: views, unique viewers, avg watch %, follows gained, comment rate |
| **Follower growth** | daily `follows − unfollows` per user, 30-day sparkline |
| **70/30 tuning** | measure whether the discovery slice actually retains — A/B the ratio |

`gosnowd` mounts behind GoGate and inherits its RBAC + audit — a creator only
sees their own rows.

---

## 5. Safety, observability, ops

| Concern | Service | How |
|---|---|---|
| API errors | **GoFlare** | every 5xx captured with the edge trace id; SDK enriches with the stack when installed |
| Spam / mass-follow | **GoFlare** edge WAF | rate-limit the follow route per IP + per account; scripted patterns blocked |
| Report spike on a reel | **GoFlare** | "this issue is a spike of reports on `reel:<id>` — one click to shadow-limit its reach while you review" |
| Data export (GDPR) | **GoDoc** | `GET /me/export` → CSV of posts, follows, DMs, streamed |
| Creator earnings statement | **GoDoc** | monthly PDF from the GoSnow numbers |
| Status page + dead routes | **GoUptime** | public status; crawls the deployed frontend after each deploy |
| Push notifications | **GoStream** → bridge | a `push` topic fans out to APNs/FCM (the MobileTasks pattern) |

---

## 6. Phases

| Phase | Scope | Depends on |
|---|---|---|
| **1 — De-toy** | Delete `internal/gateway` / `internal/notify` / `internal/ratelimit`; front with GoGate, live surfaces on GoStream. Domain API keeps only `internal/social` + event store. | GoGate, GoStream (done) |
| **2 — Reels** | Song catalog service; `POST /reels` → GoRender job → HLS + poster; SSE progress; reels feed (followed only). | GoRender (Phase 0 done; needs the `reel` template) |
| **3 — Live rooms** | `reel:<id>` topics — live likes/comments/presence. Stories with TTL. | GoStream |
| **4 — Discovery** | Nightly Parquet dump; GoSnow discovery + trending; the 30% slice; cursor pagination. | **GoSnow U10** (DuckDB engine — currently blocked) |
| **5 — Analytics & safety** | Creator dashboard, GoDoc exports/statements, GoFlare WAF + report-spike limiting, GoUptime status page. | GoFlare, GoDoc, GoUptime |
| **6 — Multi-tenant pages** | "GoLangMyPage"-style curated pages; GoGate routes `<slug>.pages.gosocial/*` per tenant; a page can load an external slide deck as its feed. | GoGate route store |

Phase 4 is the one real blocker: the discovery slice wants GoSnow's DuckDB
engine, which is [[covergo-concept-coverage-status]]'s U10 — cgo, CI-only on the
current toolchain.

---

## 7. What carries over unchanged

- `internal/social` — the follow graph, DMs, feed projector, `SongID` on a post.
- `internal/events` — the `EventRecord` / `Apply` / `Rebuild` model; it just gets
  a Postgres/WAL backend and a Parquet sink instead of a slice.
- The event-sourcing discipline: `/debug/replay?upto=N` still reconstructs state
  as of any event — now that's also how GoSnow backfills.
