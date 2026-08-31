# GoDoc — Feature Roadmap (V5)

A from-scratch **stateless document / export service** in Go: POST a spec, get a
CSV / XML / PDF back, nothing stored — reusing the platform layer already built
in `GoAdmin` for identity, RBAC, audit and secrets, and taking document
generation off every app's request path.

This file is the north star. Phase 0 (a compiling, tested walking skeleton) is
in this repo now; everything past it is planned, not built.

---

## 1. Why this, why Go

Three projects generate documents on their web tier:

- **PayrollEngine** (NestJS) builds payroll XML with `xmlbuilder2`. A
  1,000-employee run is CPU-bound Node — ~3 s during which that instance's
  event loop serves no one else. Runs are monthly and bursty.
- **CrmLara** (Laravel) renders invoice and report PDFs inline.
- **crm3-micro** exports CSV/PDF reports from the gateway.

Each blocks the request thread, each reimplements the plumbing, and none can be
scaled independently of the app. GoDoc is a **stateless** worker: decode →
validate → stream, no session, no disk. `encoding/xml` and `encoding/csv` write
straight to the socket; `go-pdf/fpdf` builds a report in a few milliseconds. N
instances behind a load balancer, scale on CPU, restart freely.

### What GoDoc replaces, concretely

| Today | GoDoc |
|---|---|
| `xmlbuilder2` payroll XML, ~3 s on the event loop | `POST /v1/xml` template `payroll`, ~0.15 s off it |
| Laravel `Barryvdh\DomPDF` / `Snappy` per request | `POST /v1/pdf` template `invoice` / `table` |
| `league/csv` / `fputcsv` in a controller | `POST /v1/csv`, streamed |
| a queue job + a temp file + a download route | one request, `Content-Disposition` on the response |

---

## 2. Architecture

```
  app ──POST /v1/{csv,xml,pdf}──► godocd (Go, internal/server)
        {spec + data}             │  decode → spec.Validate
                                  │  render.Do(w, req)   (deadline + size cap)
                                  ▼
             ┌───────────────┬────────────────┬──────────────┐
        internal/csvdoc  internal/xmldoc  internal/pdfdoc
        encoding/csv     streaming XML     go-pdf/fpdf
        → response       generic + templates   table | invoice
                              │
                         template registry (payroll; more added here)

  cross-cutting (reuse GoAdmin): JWT identity, RBAC on templates, audit of who
  exported what, GoSecrets for signing keys / storage creds.
```

Seams: each renderer takes an `io.Writer` and its sub-spec — adding a format is
a new package + a `render.Do` case; adding a template is a function in that
format's registry. `render` is the only place that maps a format to a
content type.

---

## 3. Phases

### Phase 0 — walking skeleton — **in repo**
`spec` (per-format request + `Validate`), `csvdoc` (streamed), `xmldoc`
(hand-written streaming encoder: generic JSON→XML + the `payroll` template),
`pdfdoc` (`go-pdf/fpdf`, `table` + `invoice`), `render` (dispatch), `server`
(`/v1/{csv,xml,pdf}` + `/v1/render` + `/v1/templates` + health, with a render
deadline / body cap / output cap), `godocd`. `go test ./...` green.

### Phase 1 — the template library & data binding
- A **template = a named spec + an input schema**, registered per format, so
  PayrollEngine's exact XML (and CrmLara's invoice layout) live here as data,
  not code. JSON-Schema validation of the input before render.
- CSV: number/date formatting per column, computed columns, group subtotals.
- XML: XSD-driven output (validate against a schema on the way out), namespaces,
  CDATA control, canonical ordering.
- PDF: a declarative block language (heading / table / key-value / spacer /
  page-break) so a new report is a JSON template, not Go.

### Phase 2 — HTML → PDF
- An optional headless-Chrome (or `weasyprint`) worker pool for
  pixel-accurate, CSS-styled PDFs — invoices with a brand, letterheads,
  charts. Same `POST /v1/pdf` with `template: "html"` + an HTML/URL + assets.
  Isolated (seccomp / container) because it runs a browser.
- Server-side chart rendering (SVG → PDF) for report dashboards.

### Phase 3 — more formats
- **XLSX** (real spreadsheets: multiple sheets, formulas, styles, freeze panes)
  via a from-scratch or `excelize`-backed writer.
- **DOCX** from a template + merge fields.
- **JSON / NDJSON / Parquet** bulk export (feeds a data pipeline).
- **iCal**, **vCard** for the CRM.

### Phase 4 — big exports & delivery
- Streaming straight to an object store (`PUT` to S3/GCS) with a signed
  download URL, so a million-row CSV never buffers.
- Async mode: `POST` returns a job id, poll or webhook on completion; the sync
  path stays for small docs.
- Backpressure: a bounded render worker pool, `429` + `Retry-After` when full
  (the GoRender / ingest pattern).
- Chunked / resumable download.

### Phase 5 — correctness & fidelity
- Golden-file tests: render → extract text / structure → diff against a
  checked-in expectation, per template.
- Deterministic output (fixed timestamps / ids in a "reproducible" mode) so a
  re-render byte-matches — needed for audit and signing.
- Font embedding + Unicode / RTL / CJK for PDF; currency and locale formatting.
- **Digital signatures**: sign a PDF (PAdES) / an XML (XAdES) with a key from
  GoSecrets — the payroll and invoice use case.

### Phase 6 — governance (reuse, not rebuild)
- Mount `/v1` behind **GoAdmin's gateway**: JWT identity, RBAC
  (`doc:render:<format>`, `doc:template:write`), **hash-chained audit** of every
  export (who, what template, row count, when), **GoSecrets** for signing keys.
- Per-tenant template namespaces and quotas; rate limits per identity.
- A retention view for async artifacts.

### Phase 7 — web console (Node)
- Template gallery + live editor (edit the JSON template, see the rendered
  sample), an export log, per-format usage charts. SPA behind the gateway like
  gofile / GoObserv.

---

## 4. What to reuse from V5 (don't rebuild)

| Need | Reuse |
|---|---|
| JWT / auth on `/v1` | `GoGate/internal/auth` |
| Rate limiting per identity | `GoGate/internal/ratelimit` |
| Backpressured worker pool | `GoRender/internal/worker`, `GoFlare/internal/ingest` |
| Object storage for big exports | `GoFlare/internal/blob`, `GoSnow/internal/storage` |
| RBAC `(role, action, resource)` | `GoAdmin/gateway/internal/rbac` |
| Hash-chained audit of exports | `GoAdmin/gateway/internal/auditlog` |
| Signing keys at rest | `GoAdmin/gateway/internal/secrets` (GoSecrets) |
| TLS, ACME, config reload | `GoAdmin/gateway` |
| Load-testing a payroll burst | `GoAdmin/GoLoad` |

## 5. Explicitly out of scope

- Being a reporting / BI tool (query builders, scheduled dashboards) — GoDoc
  renders a document from data it is given.
- A template *designer* UI in Phase 0–5 (the console in Phase 7, maybe).
- Storing the source data — the caller owns it; GoDoc is stateless by design.
- Email delivery of the rendered doc — that's GoEmail's job; hand it the bytes.

## 6. Status

- [x] **Phase 0 — walking skeleton** — `godocd` builds, `go test ./...` green;
  CSV (streamed), XML (generic + `payroll` template), PDF (`table` + `invoice`)
  render over HTTP with a deadline / size cap; stateless.
- [ ] Phase 1 — the template library (schema-validated), richer CSV/XML/PDF
- [ ] Phase 2 — HTML → PDF via a headless-browser pool
- [ ] Phase 3 — XLSX / DOCX / Parquet / iCal
- [ ] Phase 4 — object-store streaming, async jobs, backpressure
- [ ] Phase 5 — golden-file tests, determinism, PDF/XML signing
- [ ] Phase 6 — governance via GoAdmin gateway
- [ ] Phase 7 — web console
