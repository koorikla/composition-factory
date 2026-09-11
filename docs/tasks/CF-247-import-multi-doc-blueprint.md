# CF-247 — Import and cf gen silently drop every YAML document after the first and answer 200 / exit 0

## Severity & Scope
- **Severity**: P2
- **Scale**: engine
- **Touch set**: `internal/api/import_test.go`

## Background & Problem
`POST /api/blueprint/import` accepted a multi-document YAML stream, persisted the first document, and silently discarded everything after the first `---` — a second complete blueprint, an invalid blueprint, or non-YAML garbage.
With the fix in CF-243 (`internal/blueprint/yaml.go`), `blueprint.Parse` and `blueprint.ParseAny` enforce that a single blueprint YAML stream must contain exactly one document.
`POST /api/blueprint/import` needs regression tests ensuring multi-document streams and garbage trailing documents return HTTP 400 Bad Request instead of 200 OK.

## Expected Behavior & Contract
When a request is posted to `POST /api/blueprint/import`:
- If the body contains multiple YAML documents (e.g. two blueprints separated by `---`), it must be rejected with HTTP 400 Bad Request.
- If the body contains trailing invalid YAML garbage after `---`, it must be rejected with HTTP 400 Bad Request.

## Acceptance Test
- `TestImportRejectsMultiDocumentYAML` in `internal/api/import_test.go`:
  - `POST /api/blueprint/import` with two concatenated blueprints separated by `---` returns HTTP 400.
  - `POST /api/blueprint/import` with a blueprint followed by invalid YAML after `---` returns HTTP 400.
