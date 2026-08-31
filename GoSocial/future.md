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

## 7. Beyond parity — features Instagram structurally can't or won't ship

The V5 stack makes a specific set of things *cheap* that a client-first app has
to fake or avoid: server-side video composition (GoRender), real-time at scale
(GoStream), analytics as a user-facing surface (GoSnow), tamper-evident moderation
(GoFlare's hash chain), and a real event log. That combination opens features
that aren't just "Instagram but ours."

### Video that's composed on the server, not the phone

| Feature | Why the stack makes it cheap |
|---|---|
| **Remix / duet / stitch as one real file** — pick any public reel, GoRender composites your camera PiP or side-by-side and re-encodes a single artifact. Not a client overlay. | GoRender already compiles a filtergraph from a spec; a remix is just `[0:v][1:v]hstack` + `amix`. Content-hash dedup means a popular remix renders once. |
| **Provenance tree + attribution splits** — every remix records its parent; you can trace "viral reel → remix → remix → X's March original," and X gets a credit line and a revenue cut automatically. | The spec *is* the lineage. GoSnow rolls up "views attributable to each ancestor." |
| **Templates are filtergraphs** — a creator publishes a template (a GoRender spec with typed slots: "3 clips + 1 photo + your text here"). Followers fill the slots; the server renders deterministically. | Instagram templates are client-side and break across versions. A spec with slots renders identically for everyone, forever. |
| **Thread-to-reel / article-to-reel** — paste text, get a slideshow reel: `drawtext` per paragraph over generated cards, ken-burns, a song bed. | This is exactly `leadMarketing/generator`'s job, which is *why* GoRender exists. |
| **Live → instant VOD** — GoRender transcodes the live segments as they arrive; the moment a stream ends the replayable reel already exists, with chapter markers auto-placed at peak-viewer moments. | GoStream carries the live; GoRender's queue drains segments in parallel; GoSnow marks the peaks. |
| **Auto-recap montages** — "your week in reels": GoRender stitches your top 5 by completion rate into a 15s `xfade` montage with a song, zero effort. | One scheduled GoRender job per user per week; `spec.Weight()` keeps the batch from thrashing the box. |
| **Deterministic version history** — because a render is a content-hashed spec, "what changed between v2 and v3 of this reel" is a real diff: captions, trim points, song swap. | Nothing to store but the specs. |

### Real-time as the product, not a garnish

| Feature | Why the stack makes it cheap |
|---|---|
| **Co-watch rooms** — `room:<id>` syncs playback position across everyone; reactions float over the video; the host can pull a viewer "on stage." | GoStream does ~300k conns/instance at 0 alloc/broadcast — a 10k-person room is nothing. |
| **Choose-the-ending reels** — a branching reel; viewers vote live on `reel:<id>:vote`, the majority branch plays. GoRender pre-rendered every branch. | Votes are a topic; branches are cached artifacts. |
| **Reaction heatmap scrubber** — every like-tap publishes its video timestamp; the scrubber shows a live histogram of where people react, and creators see which second lands. | A `reel:<id>:reacts` topic + a GoSnow rollup by `floor(ts)`. |
| **Ambient friend presence** — "3 friends are watching reels now — tap to join." | GoStream presence on `presence:friends:<userId>`. |
| **Latency-honest live** — the UI shows your real glass-to-glass delay ("you're 2.1s behind live") instead of pretending it's instant. | GoStream and GoRender timestamps are real; just surface them. |

### Analytics you can see, not just the algorithm

| Feature | Why the stack makes it cheap |
|---|---|
| **Public completion rate** — every reel shows "82% watched to the end," not just a like count. Changes what creators optimise for. | GoSnow already computes it for discovery — just expose it. |
| **"Why am I seeing this?"** — one tap on any discovery reel explains it from the *actual* ranking features: "high completion + 2 mutuals liked it + trending in your city." | The explanation is the GoSnow query's own feature vector. |
| **Time-travel feed** — "show my feed as it was last Tuesday" / "this account 6 months ago." | The event-sourced core: `replay?upto=N` is already how state is rebuilt. |
| **Sound-about-to-blow** — trend forecasting from `velocity = Δlikes/Δmin`: "this sound is up 340% in 2h" *before* it saturates. Early-mover edge for creators. | A GoSnow windowed aggregate over the song-usage events. |
| **Sound-first discovery** — the song catalog is a real service with waveforms and BPM, so you can browse by audio: "reels using this 3-second drop," "reels at 128 BPM." | The catalog already has the metadata; the join is `posts.songId`. |
| **Bring-your-own-dashboard** — a creator runs sandboxed SQL over their own rows, or exports Parquet. | GoSnow with GoGate RBAC scoping to `author_id = me`. |

### Trust, made transparent

| Feature | Why the stack makes it cheap |
|---|---|
| **Public, tamper-evident moderation log** — when a reel is limited the creator sees exactly why and can appeal; aggregate stats are public ("0.3% of reels limited this month"). | GoFlare's audit is a hash chain — altering one entry breaks every later link. |
| **Timed shadow-limit, not shadowban limbo** — a report spike auto-limits reach for 2h pending review, then auto-restores if cleared. Always a timer, always a reason. | GoFlare's "rate-limit this route" generalised to "limit this reel." |
| **Rate-limit your own account** — "I'm doomscrolling; cap me at 20 reels/hour today." | GoGate's per-key limiter, opt-in, user-set. |
| **Consent receipts** — your face or voice in someone's remix triggers a notification and a veto window *before* it publishes. | The provenance tree makes "who is in this" a queryable fact. |
| **"We blocked 4 scripted follow attempts on your account this week."** | GoFlare edge WAF events, surfaced to the user instead of hidden. |

### Openness

| Feature | Why the stack makes it cheap |
|---|---|
| **An account export that actually works** — GoDoc streams a real archive: every reel's source clips, DMs as readable HTML, the follow graph as CSV, the full event log as JSON. Leave without losing anything. | GoDoc is a stateless CSV/HTML/JSON streamer; the event log is already the source of truth. |
| **Bring-your-own-domain pages** — `marian.page` served through GoGate, your reels as a portfolio, your layout. Link-in-bio, inverted. | GoGate's route store + per-tenant `<slug>.pages.gosocial/*` routing. |
| **Self-hosted embeds** — an `<iframe>` that streams from your object store, no tracker. | HLS + poster already live in object storage. |
| **Federated follow** (aspirational) — the event log as an ActivityPub-style outbox; follow someone on another GoSocial instance. | GoGate's HTTP↔queue bridge + the append-only event log are most of an outbox already. |
| **Conditional / scheduled posting** — "publish this when my last reel hits 10k views." | GoSnow threshold → GoGate queue bridge fires it. |

None of these need a new service — they're recombinations of the five that
already exist, plus the domain the app already has.

---

## 8. What carries over unchanged

- `internal/social` — the follow graph, DMs, feed projector, `SongID` on a post.
- `internal/events` — the `EventRecord` / `Apply` / `Rebuild` model; it just gets
  a Postgres/WAL backend and a Parquet sink instead of a slice.
- The event-sourcing discipline: `/debug/replay?upto=N` still reconstructs state
  as of any event — now that's also how GoSnow backfills.
