# GoRender

A from-scratch **media render service** in Go — submit a spec (still images +
audio → video, or clips → one video), a pool of workers compiles it to a single
**ffmpeg filtergraph** and runs it, progress streams back over SSE, and the
finished MP4 is downloadable.

It exists to take the render step off Python. `leadMarketing/generator` and
`crm6` both build video through **moviepy**, which encodes frame-by-frame in a
Python loop under the GIL. GoRender hands the whole job to one ffmpeg invocation
and runs `NumCPU` of them at once.

Full plan and rationale: [`future.md`](future.md).

## Status: Phase 0 — walking skeleton

`gorenderd` compiles, `go test ./...` is green (including a real-ffmpeg
end-to-end render), and the pipeline runs end to end:
**POST spec → queue → worker → plan → ffmpeg → artifact**, with SSE progress.

Everything is in-memory: the job store and the queue do not survive a restart,
outputs land in a local directory, and there are two templates — `slideshow`
and `concat`. Object storage, a Postgres job table, a Redis/NATS queue, Pillow
text-slide generation, the crm6 TTS path and per-template filtergraph libraries
are later phases — see `future.md` §3.

Needs **ffmpeg** and **ffprobe** on `PATH` (or `-ffmpeg` / `-ffprobe`). Nothing
else — the Go side is standard library only.

## Layout

| Path | Role |
|---|---|
| `cmd/gorenderd` | server entrypoint, flags, graceful shutdown |
| `internal/spec` | the render request model + defaults + `Validate` |
| `internal/job` | job + lifecycle state + in-memory `Store` (Postgres stand-in) |
| `internal/queue` | in-memory job backlog (`Mem`) — Redis/NATS stand-in |
| `internal/media` | the only place that shells to ffmpeg/ffprobe: `Locate`, `Probe`, `Encoder` |
| `internal/plan` | compiles a `Spec` into a `media.Plan` (ffmpeg args + filtergraph) |
| `internal/worker` | the pool: claim → build plan → encode → record result |
| `internal/events` | per-job progress fan-out for SSE |
| `internal/server` | REST + SSE |
| `internal/uid` | random ids over `crypto/rand` |

## Run

```bash
cd GoRender
go test ./...
go run ./cmd/gorenderd                        # :8096, workers = NumCPU, out ./out
go run ./cmd/gorenderd -workers 4 -out /var/renders -job-timeout 10m
```

Flags (each also an env var — `GORENDER_ADDR`, `GORENDER_OUT_DIR`,
`GORENDER_WORKERS`, `GORENDER_QUEUE`, `GORENDER_JOB_TIMEOUT`, `GORENDER_FFMPEG`,
`GORENDER_FFPROBE`):

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8096` | listen address |
| `-out` | `./out` | directory for finished renders |
| `-workers` | `NumCPU` | concurrent ffmpeg jobs |
| `-queue` | `128` | jobs allowed to wait before submit is rejected |
| `-job-timeout` | `30m` | hard cap on one render (`0` = none) |
| `-ffmpeg` / `-ffprobe` | _(PATH)_ | explicit binary paths |

## Try it

```bash
curl localhost:8096/healthz          # reports the resolved ffmpeg/ffprobe paths

# slideshow: three images, 4s each, 0.5s crossfade, over an audio bed
curl -s localhost:8096/v1/jobs -d '{
  "template": "slideshow",
  "width": 1080, "height": 1920, "fps": 30,
  "slideshow": {
    "images": ["/abs/a.jpg", "/abs/b.jpg", "/abs/c.jpg"],
    "audio": "/abs/bed.mp3",
    "seconds_per_image": 4,
    "crossfade_seconds": 0.5
  }
}'
# → {"id":"<jobid>","status":"queued",...}

curl -s localhost:8096/v1/jobs/<jobid>            # poll status + progress
curl -N localhost:8096/v1/jobs/<jobid>/events     # SSE: {"progress":0.42,...} until done
curl -sOJ localhost:8096/v1/jobs/<jobid>/artifact # download <jobid>.mp4

# concat: join clips (audio kept only if every clip has it)
curl -s localhost:8096/v1/jobs -d '{
  "template": "concat",
  "width": 1920, "height": 1080, "fps": 30,
  "concat": { "clips": ["/abs/intro.mp4", "/abs/body.mp4", "/abs/outro.mp4"] }
}'
```

### API

| Method + path | Purpose |
|---|---|
| `GET /healthz` | liveness + resolved ffmpeg/ffprobe + queue depth |
| `GET /v1/version` | build version |
| `POST /v1/jobs` | submit a spec; `202` + the queued job |
| `GET /v1/jobs` | all jobs, newest first |
| `GET /v1/jobs/{id}` | one job (status, `progress` 0–1, `artifact`, `error`) |
| `GET /v1/jobs/{id}/events` | Server-Sent Events progress stream, ends on terminal state |
| `GET /v1/jobs/{id}/artifact` | the finished MP4 (`409` until the job succeeds) |

### Templates

| `template` | Block | Produces |
|---|---|---|
| `slideshow` | `images[]`, optional `audio`, `seconds_per_image`, `crossfade_seconds` | each image scaled+letterboxed to the canvas, held, crossfaded (or hard-cut when `crossfade_seconds` is `0`); `-shortest` against the audio bed |
| `concat` | `clips[]` (≥ 2) | clips normalised to the canvas and joined; `concat` filter with audio when every clip has an audio stream, video-only otherwise |

Canvas defaults to portrait **1080×1920 @ 30 fps** — the reel/story shape the
Python generators target. Output is H.264 / yuv420p / AAC, `+faststart`.
