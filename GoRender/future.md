# GoRender — Feature Roadmap (V5)

A from-scratch **media render service** in Go: take a render spec over HTTP,
compile it to one ffmpeg filtergraph, run a pool of them, stream progress, hand
back the file — reusing the platform layer already built in `GoAdmin` for
identity, RBAC, audit and secrets.

This file is the north star. Phase 0 (a compiling, tested walking skeleton) is
in this repo now; everything past it is planned, not built.

---

## 1. Why this, why Go

Two projects on the drive build video the slow way:

- **`leadMarketing/generator`** — Pillow for slides, then **moviepy** +
  `imageio-ffmpeg` to assemble reels, plus an OpenAI/Whisper transcribe step.
- **`crm6`** (Django) — **moviepy 1.0.3** + `imageio-ffmpeg` + `edge-tts` to
  turn scripts into narrated course videos.

moviepy composites and encodes **frame by frame in a Python loop**. Every
overlay, crossfade and concat is Python-level array work before a frame reaches
the encoder, and the GIL means one job pins one core. A 60-second reel with text
and transitions is 40–90s of wall time; the same job expressed as a single
ffmpeg `-filter_complex` (`drawtext`, `xfade`, `concat`, `amix`) is 5–15s,
because ffmpeg does the compositing in C and the encode in one pass.

Go's job here is not to touch pixels — it is to be the **orchestrator** ffmpeg
never had: a queue, a worker pool sized to `NumCPU` running real ffmpeg
processes in parallel, `context` deadlines, progress parsed from `-progress`,
content-hash dedup, retrys, and object-store upload. A static binary that only
needs ffmpeg beside it.

### What GoRender replaces, concretely

| Today | GoRender |
|---|---|
| `moviepy.concatenate_videoclips` + `TextClip` | `concat` / `xfade` + `drawtext` in one filtergraph |
| a Celery/`joblib` task per video, GIL-serial | `POST /v1/jobs` → pool of `N` ffmpeg processes |
| polling a task result, no progress | SSE `progress` 0–1 from ffmpeg's own `out_time` |
| output written wherever the worker ran | one `OutDir` now, object storage in Phase 2 |
| Pillow slide PNGs shelled from Python | Phase 3: a `slide` template rendering text/brand in-process |

---

## 2. Architecture

```
  clients ─────► REST + SSE  (Go, internal/server)
  (generator,    │   POST /v1/jobs        → validate → job.Store.Create → queue.Push
   crm6, CLI)    │   GET  /v1/jobs/{id}/events  → events.Broker subscription (SSE)
                 │   GET  /v1/jobs/{id}/artifact
                 ▼
        queue.Mem  (in-proc FIFO; Phase 4: Redis / NATS)
                 │  Claim
        ┌────────▼─────────────────────────────┐
        │  worker.Pool   N = NumCPU goroutines │
        │   claim → plan.Build(spec) → media   │
        │   .Encoder.Encode(plan, onProgress)  │
        └───┬───────────────┬──────────────────┘
   plan     │               │  progress fraction
 (filtergraph)              ▼
        ┌───▼──────────┐  events.Broker ──► SSE subscribers
        │  ffmpeg      │
        │  (one proc   │  artifact ──► OutDir/<jobid>.mp4  (Phase 2: GCS/S3)
        │   per job)   │
        └──────────────┘

  job.Store  in-memory, copy-on-read   (Phase 1: Postgres)
```

The seams that matter: `media.Encoder` and `media.Prober` are interfaces (tests
run the pool with a fake encoder), `queue.Mem` is the only thing that knows the
backlog is in-process, `job.Store` is the only thing that knows state isn't
persisted. Each is one file to swap.

---

## 3. Phases

### Phase 0 — walking skeleton — **in repo**
`spec` (slideshow + concat, `Validate`/`Normalize`), `job` + in-memory `Store`,
`queue.Mem`, `media` (`Locate`/`Probe`/`FFmpegEncoder` with `-progress`
parsing), `plan` (the two filtergraph builders), `worker.Pool`, `events.Broker`,
`server` (REST + SSE + artifact download), `gorenderd`. `go test ./...` green
including a real-ffmpeg render of generated PNGs.

### Phase 1 — persistence
Postgres `job.Store` (jobs, progress, artifact ref, error, timestamps) behind
the same interface. Jobs survive a restart; a worker that died mid-render leaves
a `running` job that a reaper requeues. Reuse `GoAdmin`'s migration runner.

### Phase 2 — artifact storage
`artifact.Store` interface: `Put(jobID, reader) → URL`, `Open(jobID)`. Local-dir
impl (now) + GCS/S3 impl. Content-hash the plan so an identical re-submit
returns the existing artifact instead of re-encoding. Signed download URLs.

### Phase 3 — the slide template
`slide` / `carousel` template: render text, background, brand marks in-process
(`fogleman/gg` or an ffmpeg `drawtext`/`drawbox` graph) so the generator's
Pillow step disappears. Font loading, wrapping, safe-area, RTL. This is what
lets `leadMarketing/generator` drop Python entirely for static + carousel
output.

### Phase 4 — real queue + horizontal workers
`queue` interface with a Redis Streams or NATS JetStream impl; `gorenderd`
splits into an API process and N worker processes on separate boxes, each
pulling from the shared queue. Per-tenant fair scheduling.

### Phase 5 — audio & narration
`tts` step (edge-tts-compatible or a pluggable provider) feeding an `audio`
input; `amix`/`aducking` for music under VO; loudness normalisation
(`loudnorm`). Covers the crm6 course-video path end to end.

### Phase 6 — templates as data
A template is a named, parameterised filtergraph + input schema, stored not
compiled in. `POST /v1/templates`, versioned. The generator ships its reel and
carousel layouts as GoRender templates instead of Python functions.

### Phase 7 — platform integration
Drop behind `GoAdmin`'s gateway: JWT identity, RBAC (`render:submit`,
`render:read`), audit every job, secrets (storage creds, TTS keys) from
`GoSecrets`. Per-tenant quotas and retention. Prometheus metrics
(`render_duration_seconds`, `queue_depth`, `ffmpeg_failures_total`).

---

## 4. Non-goals

- Not a video editor or a timeline UI — it renders declared specs.
- Not a streaming/transcoding service — batch render to a file, not live ABR.
- No in-Go codecs — ffmpeg is a hard dependency and that is fine.
- Not a general job runner — it renders media; other work belongs elsewhere.
