# Where Go pays off on F:\

**Infrastructure audit · F:\ drive · 2026-08-30**

Twenty-odd projects, mostly Python and Node, each carrying one workload that
fights its runtime. Seven of them are the same shape — and that shape is a Go
service.

| | |
|---|---|
| **23** | project trees scanned |
| **5** | stacks — Go · Node · Python · PHP · Flutter |
| **7** | Go services worth extracting |
| **6–30×** | indicative throughput / latency gain |

---

## 01 — The scan: what's on the drive

Grouped by runtime. The Go column is small and every project in it is a Phase-0
skeleton — the platform ambition is already there, unfinished.

The estate splits cleanly. The **CRMs and dashboards** (crm3-micro's 13 NestJS
services, ShopFloor3D, CrmLara, crm6, PayrollEngine) are request/response apps
that are fine as they are. The **data and media work** (scraper, leadMarketing's
pipeline and generator, chill's collector, the dataScience folder) is where a
runtime choice actually costs wall-clock time. And the **V5 Go projects** are
half-built versions of exactly the shared services the rest of the estate keeps
re-implementing badly.

| Stack | Projects |
|---|---|
| **Go · V5** | GoFlare · GoUptime · GoSnow · GoAdmin · GoPlatform · infra-tracker |
| **Node / TS** | crm3-micro ×13 · ShopFloor3D · PayrollEngine · ai-engine · leadMarketing (Next) · shop-products · MobileTasks · GoMobile |
| **Python** | scraper (Playwright) · crm_pipeline · generator (moviepy·ffmpeg) · crm6 (Django·Celery) · chill/youtube-collector · dataScience |
| **PHP** | CrmLara (Laravel 12) · chill/backend (Laravel 6) · SassDesk.com |
| **Flutter** | MobileChill |

---

## 02 — Hotspot map: the bottleneck, and what absorbs it

Left: the specific operation that fights its runtime today. Right: one Go
service, shared across every project that has that operation.

| Bottlenecked today | → | Go service that absorbs it |
|---|---|---|
| scraper / crm_pipeline — HTML + registry pull | | **GoScrape** — crawl / scrape API |
| Playwright headless scrape — Chromium per worker | | GoScrape |
| chill/youtube-collector — channel crawl | | GoScrape |
| generator — moviepy reel render (Python frame loop) | | **GoRender** — ffmpeg fan-out |
| crm6 — moviepy + edge-tts course video | | GoRender |
| crm3-micro gateway — HTTP → Pub/Sub + JWT | | **GoGate** — edge gateway / BFF |
| ShopFloor3D / CrmLara — API front door | | GoGate |
| ShopFloor3D — Socket.io live overlays | | **GoStream** — WebSocket fan-out |
| crm3-micro/chat-svc · MobileTasks push | | GoStream |
| filter_leads.py — per-row scoring, 217 CSVs | | **GoLeads** — DuckDB CSV lake |
| crm_pipeline/ml — score + country opportunity | | GoLeads |
| PayrollEngine — xmlbuilder2 payroll XML | | **GoDoc** — XML / PDF / CSV export |

**GoSearch** (a bleve / SQLite-FTS service replacing chill's Elasticsearch node)
is the seventh — left off the map only because it swaps a dependency rather than
unblocking a single hot path. **GoScrape overlaps GoUptime's** planned dead-route
crawler; **GoGate is GoAdmin's gateway**, productised.

---

## 03 — Performance scan: the gap, per workload

Improvement factor. Raw before → after on each row. ↓ marks a lower-is-better
metric (latency, memory, wall-clock).

| Service | Workload | Before → after | Factor |
|---|---|---|---|
| **GoScrape** | HTML + registry scraping | 100 → 3,000 pages/s | 30× |
| | Headless-browser scrape | 3 → 14 pages/s | 5× |
| | Scraper RAM footprint ↓ | 2.0 GB → 0.4 GB | 5× |
| **GoRender** | Reel render, 60 s ↓ | 65 s → 9 s | 7× |
| | Parallel jobs / box | 1 (GIL) → 8 | 8× |
| **GoGate** | Gateway throughput | 6,000 → 90,000 rps | 15× |
| | Gateway p99 under load ↓ | 55 ms → 6 ms | 9× |
| | Gateway RAM ↓ | 512 MiB → 48 MiB | 11× |
| **GoStream** | WebSocket conns / instance | 15,000 → 300,000 | 20× |
| | RAM per connection ↓ | 40 KB → 6 KB | 7× |
| **GoLeads** | Score + aggregate, 32k rows ↓ | 2,500 ms → 120 ms | 21× |
| | Same at 1M rows ↓ | 14 s → 0.5 s | 28× |
| **GoDoc** | Payroll XML, 1k employees ↓ | 3,200 ms → 280 ms | 11× |
| **GoSearch** | Search service RAM floor ↓ | 1,500 MB → 120 MB | 12× |
| | Cold start ↓ | 30 s → 0.4 s | 25× |

**These are indicative, not measured on your data.** They come from the workload
shape (parse-bound, IO-bound, connection-bound, frame-bound) crossed with
well-known runtime characteristics — Python's GIL serialising lxml/bs4, moviepy's
per-frame Python loop vs a single ffmpeg filtergraph, Node's event loop under
fan-out, pandas' cold import + concat vs DuckDB's columnar scan. Treat them as
"which order of magnitude", then benchmark the one you build first.

---

## 04 — The services: seven extractions, ranked by payoff-to-effort

Each is a standalone binary with an HTTP (or gRPC) surface. None require touching
the apps that call them beyond swapping a client.

### GoScrape — concurrent crawl / scrape API
One politeness-and-rate-limit layer for every scraper you run.
- **Replaces:** Python `asyncio` + Playwright + BeautifulSoup across `scraper/`, `crm_pipeline/scrapers/`, `youtube-collector`
- **Feeds:** scraper · crm_pipeline · chill · GoUptime
- **Mechanism:** worker pool of goroutines; `colly` + `net/html` for static pages, `chromedp` only where JS renders; per-domain token bucket, robots, retry/backoff; NDJSON stream out
- **Delta:** HTML + registry pull ~15–40× throughput (100 → 1–3k pages/s), ~5× less RAM. Headless ~5× and needed far less often
- **Effort:** M — colly does most of it

### GoRender — media render orchestrator
A job queue that drives ffmpeg directly instead of through Python.
- **Replaces:** moviepy 1.0.3 + `imageio-ffmpeg` in `generator/` and `crm6`; Pillow slide gen
- **Feeds:** generator · crm6 · coursecasts
- **Mechanism:** queue → workers = `NumCPU`, each shells one ffmpeg filtergraph (`drawtext`, `concat`, `xfade`, `amix`); SSE progress; content-hash dedup; object-store upload
- **Delta:** 60-second reel ~6–12× per job (65s → 6–10s), and jobs run in parallel instead of GIL-serial
- **Effort:** M–L — porting each template's filtergraph is the work

### GoGate — edge gateway / BFF
JWT, rate-limit, cache, and the HTTP↔queue bridge, in front of any app.
- **Replaces:** crm3-micro's NestJS gateway (512 MiB, HTTP→Pub/Sub); the bare API front on ShopFloor3D, CrmLara, SassDesk
- **Feeds:** crm3-micro · ShopFloor3D · CrmLara · GoAdmin
- **Mechanism:** `net/http` + `httputil.ReverseProxy`; JWT verify middleware; token-bucket limiter; `singleflight` coalescing; TTL response cache; ~150-line HTTP↔Pub/Sub bridge
- **Delta:** throughput ~12–20× (6k → 90k rps), p99 ~9× (55 → 6 ms), RAM 512 MiB → ~48 MiB
- **Effort:** S–M — proxy + JWT is a day

### GoStream — WebSocket fan-out
Topic pub/sub over sockets that holds real connection counts.
- **Replaces:** Socket.io in ShopFloor3D; the chat-svc transport; MobileTasks live updates
- **Feeds:** ShopFloor3D · crm3-micro · MobileTasks
- **Mechanism:** `nhooyr/websocket` or `gobwas/ws` (epoll); subscribe by topic; HTTP/gRPC publish ingress; per-conn send buffer with slow-consumer drop; presence
- **Delta:** connection density ~10–30× (15k → 300k / instance), ~5–10× less RAM per connection, flat p99 under broadcast
- **Effort:** M

### GoLeads — CSV lake + dedupe + score API
Every scraped CSV as one queryable table.
- **Replaces:** `filter_leads.py`'s per-row loop; `crm_pipeline/ml/lead_scorer.py`; the pandas scripts in `dataScience/`
- **Feeds:** scraper · crm_pipeline · dataScience · leadMarketing
- **Mechanism:** embedded DuckDB (`marcboeker/go-duckdb`); `read_csv('**/*.csv')` as one virtual table; dedupe + scoring as SQL; `GROUP BY country, industry`; Parquet / NDJSON export
- **Delta:** score + aggregate 32k rows ~15–40× (2.5s → 0.12s cold) — and it keeps scaling where pandas goes memory-bound
- **Effort:** S — mostly SQL and a thin handler

### GoDoc — stateless document / export service
XML, PDF and CSV generation off the app's event loop.
- **Replaces:** `xmlbuilder2` payroll XML in PayrollEngine; invoice + report PDF/CSV exports in CrmLara and crm3-micro
- **Feeds:** PayrollEngine · CrmLara · crm3-micro
- **Mechanism:** streaming `encoding/xml`; `go-pdf/fpdf` or `maroto` for PDF; `encoding/csv` straight to the response writer. Stateless, scales flat
- **Delta:** 1,000-employee payroll run ~6–12× (3.2s → 0.3s) and it stops blocking every other request on that Node instance
- **Effort:** S–M

### GoSearch — embedded search API
Typo-tolerant search and autocomplete without a JVM.
- **Replaces:** chill's Elasticsearch 7.17 node + Laravel Scout; ad-hoc `LIKE` search in the CRMs
- **Feeds:** chill · CrmLara · crm3-micro · lead data
- **Mechanism:** `blevesearch/bleve` or a wrapper over SQLite FTS5; `POST /index`, `GET /search?q=`; prefix + fuzzy for autocomplete
- **Delta:** query latency is already fine both ways — the win is ~10–20× less RAM (1.5 GB → ~120 MB) and an instant cold start instead of 20–40s JVM warmup
- **Effort:** M — only if chill's search actually matters

---

## 05 — Judgement calls: where Go is *not* the answer

- **Leave it** — the CRM request/response layers (NestJS controllers, Django
  views, Laravel resolvers). They're IO-bound on Postgres, not CPU-bound; a
  rewrite buys single-digit-millisecond wins nobody feels and costs months.
- **Leave it** — the dataScience folder's deep-learning and HF-NLP work. That
  ecosystem is Python and staying Python — Go has no torch.
- **Already is** — GoSnow is a DuckDB-backed columnar engine by design; it
  doesn't *consume* a dataframe service, it *is* one. GoLeads is the small,
  focused cousin of it.
- **Finish first** — GoFlare and GoUptime are Phase-0 skeletons that already own
  two of these jobs (error ingest, uptime + dead-route crawl). Extending them
  beats starting GoScrape / GoIngest from zero.
- **Start here** — GoLeads (smallest effort, data's already in `F:\scraper`,
  DuckDB does the hard part). Then GoGate, which you half-have in GoAdmin.

---

*Performance figures are order-of-magnitude estimates from workload shape +
runtime characteristics, not measurements. Interactive version:
`claude.ai/code/artifact/5eb4b68e-7594-4c8d-b327-eaaea04a2ee8`*
