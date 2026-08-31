# GoDoc

A from-scratch **stateless document / export service** in Go — `POST` a spec
plus data, get a **CSV**, **XML** or **PDF** file back. Nothing is stored; every
instance is interchangeable, so it scales flat behind a load balancer.

It exists to take document generation off the app's request path. PayrollEngine
builds payroll XML with `xmlbuilder2` on the Node event loop — a 1,000-employee
run is ~3 s during which that instance serves nothing else. CrmLara and
crm3-micro generate invoice and report PDFs/CSVs the same way. GoDoc does the
same work in ~0.15 s on a separate process pool.

Full plan and rationale: [`future.md`](future.md).

## Status: Phase 0 — walking skeleton

`godocd` compiles, `go test ./...` is green, and all three formats render end to
end over HTTP:

- **CSV** — `encoding/csv` streamed to the response writer; positional `rows`
  or keyed `records` projected through `columns` (derived + sorted when
  omitted); custom delimiter, optional Excel BOM.
- **XML** — hand-written streaming encoder. Generic JSON→XML (`@key` →
  attribute, `#text` → text, arrays repeat, proper escaping) **or** a named
  template — Phase 0 ships `payroll` (the shape PayrollEngine builds today).
- **PDF** — `go-pdf/fpdf` with two templates: `table` (a paginated report with
  a repeating header and page numbers) and `invoice` (parties, line items,
  tax, totals, notes).

A per-request timeout, a body-size cap and an output-size cap keep one bad
request from hurting the process. XML and CSV are stdlib; PDF adds the one
`go-pdf/fpdf` dependency (pure Go). Source maps of the standard formats,
per-tenant templates, a headless-Chrome HTML→PDF path and object-store delivery
are later phases — see `future.md` §3.

## Layout

| Path | Role |
|---|---|
| `cmd/godocd` | server entrypoint, flags, graceful shutdown |
| `internal/spec` | the request body per format + `Validate` |
| `internal/csvdoc` | streaming CSV renderer |
| `internal/xmldoc` | streaming XML: generic JSON→XML + the template registry |
| `internal/pdfdoc` | PDF via `go-pdf/fpdf`: the `table` and `invoice` templates |
| `internal/render` | dispatch: spec → renderer + content type + filename |
| `internal/server` | `POST /v1/{csv,xml,pdf}` and `/v1/render`, `/v1/templates`, health |
| `internal/uid` | random ids over `crypto/rand` |

## Run

```bash
cd GoDoc
go test ./...
go run ./cmd/godocd                       # :8098, open
go run ./cmd/godocd -token "$T" -timeout 15s -max-body 33554432
```

Flags (each also an env var — `GODOC_ADDR`, `GODOC_TOKEN`, `GODOC_MAX_BODY`,
`GODOC_TIMEOUT`):

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8098` | listen address |
| `-token` | _(none)_ | bearer token required for `/v1/*`; empty = open |
| `-max-body` | `8388608` | max request body in bytes |
| `-timeout` | `30s` | per-request render deadline |

## Try it

```bash
curl localhost:8098/v1/templates          # {"pdf":["table","invoice"],"xml":["payroll"]}

# CSV from keyed records
curl -sOJ localhost:8098/v1/csv -d '{
  "columns": ["id", "name", "net"],
  "records": [{"id":1,"name":"Ana","net":2500}, {"id":2,"name":"Bo","net":1700}]
}'

# payroll XML
curl -s localhost:8098/v1/xml -d '{
  "template": "payroll", "indent": true,
  "data": {
    "period": "2026-08",
    "employer": { "name": "Acme SRL", "tax_id": "RO123" },
    "employees": [
      {"id":"E1","name":"Ana","gross":3000,"tax":450,"deductions":50,"net":2500}
    ]
  }
}'

# generic JSON → XML (no template)
curl -s localhost:8098/v1/xml -d '{"root":"order","data":{"@id":"7","item":["a","b"]}}'
# → <order id="7"><item>a</item><item>b</item></order>

# invoice PDF
curl -sOJ localhost:8098/v1/pdf -d '{
  "template": "invoice",
  "data": {
    "number": "INV-2026-014", "date": "2026-08-30", "currency": "EUR", "tax_rate": 19,
    "from": {"name": "Acme SRL", "address": "Str. Exemplu 1\nBucuresti"},
    "to":   {"name": "Client GmbH", "address": "Berlin"},
    "items": [
      {"description": "Consulting", "quantity": 10, "unit_price": 120},
      {"description": "Hosting", "quantity": 1, "unit_price": 49.99}
    ],
    "notes": "Payment within 14 days."
  }
}'
```

### API

| Method + path | Purpose |
|---|---|
| `POST /v1/csv` · `/v1/xml` · `/v1/pdf` | body is that format's spec; returns the file with `Content-Disposition` |
| `POST /v1/render` | body is `{format, filename?, csv?/xml?/pdf?}` — one endpoint, format in the payload |
| `GET /v1/templates` | named templates, by output kind |
| `GET /_godoc/healthz` · `/version` | liveness, build version |

Errors are JSON: `400` (bad JSON), `422` (spec fails validation or a render
error), `413` (body too large), `504` (render exceeded `-timeout`).
