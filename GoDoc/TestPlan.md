# GoDoc — Test Plan

## Automated (present)

`cd GoDoc && go build ./... && go vet ./... && go test ./...`

- `internal/uid` — 1000 draws are 32-char lowercase hex and unique.
- `internal/spec` — `Validate` table across all three formats: csv needs rows
  xor records, a 2-char delimiter is rejected, a missing sub-block is caught;
  xml needs `data`, rejects a non-JSON `data` and an invalid `root`; pdf needs
  a known template (`table`/`invoice`) and valid `data`, rejects a bad
  orientation; the outer request rejects a missing / unknown `format`.
  `WantDeclaration` defaults true and honours an explicit false.
- `internal/csvdoc` — positional rows quote a field with a comma; keyed records
  with no `columns` derive a **sorted** header; records project through an
  explicit `columns` (missing key → empty field, extra key → ignored); a custom
  delimiter, `no_header` and a UTF-8 BOM all apply; `cell` formats
  nil/string/bool/int/float.
- `internal/xmldoc` — every rendering is re-parsed to confirm it is well-formed.
  Generic: `@key` → attribute, `#text` → text, an array repeats its element, a
  scalar/`null` becomes text/an empty element, `&` `<` `>` are escaped in text
  and `"` also in attributes; the `<?xml?>` prolog toggles and indent adds
  newlines. `payroll` template: attributes, a `count`, and computed
  `<totals>` (2500.00 net summed to 4200.00). An unknown template errors.
- `internal/pdfdoc` — `table` and `invoice` both produce a byte stream that
  starts `%PDF-`, contains the trailer, and is non-trivial in size; 200 rows
  span ≥ 2 pages (counted via the `/Type /Page` objects, which fpdf leaves
  uncompressed); an unknown template errors. *(fpdf compresses the content
  stream, so asserting on visible text is a Phase-5 golden-file test.)*
- `internal/server` — over an `httptest` server: `/v1/csv` returns
  `text/csv` + an `attachment` disposition + the exact bytes; `/v1/xml` returns
  well-formed `application/xml`; `/v1/pdf` returns `application/pdf` starting
  `%PDF-`; `/v1/render` honours `format` in the body and a custom `filename`;
  the error matrix — bad JSON `400`, spec-invalid / unknown-format /
  bad-template `422`; bearer-token auth (`401` without, `200` with);
  `/v1/templates` lists `payroll` and `invoice`, `/_godoc/healthz` is `ok`.

Race detector (`go test -race ./...`) needs a C toolchain — run it in CI.

## Manual smoke

```bash
go run ./cmd/godocd &
curl -s localhost:8098/v1/templates
curl -sOJ localhost:8098/v1/csv -d '{"columns":["id","name"],"records":[{"id":1,"name":"Ana"}]}'
curl -s   localhost:8098/v1/xml -d '{"template":"payroll","indent":true,"data":{"period":"2026-08","employer":{"name":"Acme"},"employees":[{"id":"E1","name":"Ana","gross":3000,"tax":450,"deductions":50,"net":2500}]}}'
curl -sOJ localhost:8098/v1/pdf -d '{"template":"invoice","data":{"number":"INV-1","tax_rate":19,"from":{"name":"Acme"},"to":{"name":"Client"},"items":[{"description":"Work","quantity":8,"unit_price":100}]}}'
# a 1,000-row PDF table renders in well under a second:
python -c 'import json;print(json.dumps({"template":"table","data":{"columns":["id","name","net"],"rows":[[i,"E%d"%i,2450.0] for i in range(1000)]}}))' \
  | curl -s -X POST localhost:8098/v1/pdf --data-binary @- -o payroll.pdf -w '%{time_total}s %{size_download}B\n'
```

## Automated (to add as phases land)

- **Phase 1**: an input that fails the template's JSON-Schema is `422` before
  any rendering; CSV group subtotals sum correctly; XML validates against its XSD.
- **Phase 2**: an HTML→PDF render of a fixture matches a golden image within a
  pixel-diff tolerance; the browser worker is killed and respawned on a crash.
- **Phase 3**: an XLSX opens in a reader with the right sheet/formula/format; a
  DOCX merge fills every field.
- **Phase 4**: a 5M-row CSV streams to a mock object store at constant memory;
  async mode returns a job id and webhooks on completion; the pool returns `429`
  at capacity.
- **Phase 5**: "reproducible" mode renders byte-identical twice; a signed PDF
  verifies (PAdES) against the GoSecrets key.
- **Phase 6**: RBAC denies `POST /v1/pdf` without `doc:render:pdf`; one audit
  entry per export with the row count.
