# GoRender — Test Plan

## Automated (present)

`cd GoRender && go build ./... && go vet ./... && go test ./...`

- `internal/uid` — 1000 draws are all 32-char lowercase hex and unique.
- `internal/spec` — `Normalize` fills geometry + `seconds_per_image` +
  crossfade defaults; an explicit `crossfade_seconds: 0` survives Normalize
  (pointer field); `Validate` table: missing/unknown template, missing template
  block, empty/blank images, `crossfade >= seconds_per_image`, `concat` with
  one clip, odd (non-even) dimensions, both blocks set at once.
- `internal/plan` — slideshow crossfade chain has the right `xfade` offsets
  (`k*(d-xf)` = 3.5, 7.0 for 3 images) and maps the last stream; `crossfade 0`
  emits a `concat` filter and no `xfade`; a single image maps `v0` with neither;
  audio adds `-i`, `-map N:a`, `-shortest`; computed `Plan.Duration` matches
  `n*d - (n-1)*xf`. Concat (fake `Prober`): a/v `concat` when every clip has
  audio, `a=0` when one lacks it, error when a clip has no video, total duration
  = sum of clip durations. `ff` float formatting (`7.0`→`7`, `0`→`0`).
- `internal/queue` — FIFO order, `Len`, `Claim` blocks then receives after
  `Push`, `Claim` returns `false` on a cancelled ctx, `Push` into a full buffer
  fails once its ctx expires.
- `internal/job` — `Create`/`Get`/`List`, `Get` hands back a copy (mutating it
  doesn't leak), `MarkRunning`→`MarkDone(ok)` sets status/artifact/progress/
  timestamps, `MarkDone(err)` sets failed + error and no artifact, `Update` of
  an unknown id is `false`, `List` is newest-first (injected clock).
- `internal/events` — `Publish` reaches every subscriber of a job, is isolated
  by job id, `cancel` removes the subscriber and closes its channel and is
  idempotent, a slow subscriber (full 16-buffer) is dropped not blocked.
- `internal/worker` — with a fake `Encoder`: a job runs to `succeeded` with
  `progress = 1`, artifact and timestamps set; an encoder error → `failed` with
  the message and no artifact; a `concat` spec with no `Prober` → `failed` at
  plan-build; progress ticks and the terminal state are published as events.
- `internal/media` — `parseSeconds` (`"3.5"`, `"N/A"`, `""`, whitespace,
  garbage), `clamp`, `lastLines`, `Locate` honours explicit overrides verbatim
  and resolves from PATH otherwise (skips if ffmpeg absent).
- `internal/server` — `httptest` with a real worker pool + fake encoder:
  `/healthz` reports `ok`; `POST /v1/jobs` rejects non-JSON (400), an invalid
  spec (422), an unknown field (400); full lifecycle create→poll→`succeeded`→
  download the artifact bytes; artifact before the job finishes → 409; unknown
  job → 404.
- `internal/worker` **real-ffmpeg e2e** (`TestEndToEndSlideshowRealFFmpeg`,
  skipped under `-short` or when ffmpeg is absent) — generates three PNGs, runs
  the full pool → `plan.Build` → `FFmpegEncoder` → real ffmpeg, then `ffprobe`s
  the output: it exists, is non-empty, has a video stream, and is ~5s
  (`3*2 - 2*0.5`) within half a second.

Race detector (`go test -race ./...`) needs a C toolchain — run it in CI.

## Manual smoke

```bash
go run ./cmd/gorenderd &
# make inputs
for i in 1 2 3; do ffmpeg -y -f lavfi -i "color=c=gray:s=640x360:d=1" -frames:v 1 "img$i.png"; done
ID=$(curl -s localhost:8096/v1/jobs -d "{\"template\":\"slideshow\",\"width\":640,\"height\":360,\"fps\":24,\"slideshow\":{\"images\":[\"$PWD/img1.png\",\"$PWD/img2.png\",\"$PWD/img3.png\"],\"seconds_per_image\":2}}" | jq -r .id)
curl -N localhost:8096/v1/jobs/$ID/events         # watch progress to 1
curl -sOJ localhost:8096/v1/jobs/$ID/artifact     # $ID.mp4
```

(Use absolute native paths for `images` — ffmpeg is invoked as a child process
and does not share the shell's path translation.)

## Automated (to add as phases land)

- **Phase 1**: Postgres `job.Store` against a throwaway database — round trip,
  restart survives, a `running` job with no live worker is requeued by the reaper.
- **Phase 2**: `artifact.Store` local + a mock object store; identical spec
  re-submit returns the cached artifact (no second ffmpeg run — assert via a
  counting encoder); signed-URL expiry.
- **Phase 3**: `slide` template — golden-image comparison of rendered text
  slides (wrap, safe-area, RTL, missing-font fallback).
- **Phase 4**: queue interface conformance suite run against both `Mem` and the
  Redis/NATS impl; two worker processes against one queue don't double-render a
  job (claim is exclusive).
- **Phase 5**: `loudnorm` two-pass produces target LUFS within tolerance; music
  ducks under VO.
- **Phase 7**: RBAC — `render:submit` required to POST, `render:read` to GET;
  audit row per job; quota rejection at the limit.
