# CF-239 — GET /api/catalogue?type=<unknown> silently answers the unfiltered catalogue instead of 400

## Severity & Scope
- **Severity**: P3
- **Scale**: engine
- **Touch set**: `internal/api/catalogue.go`, `internal/api/catalogue_test.go`

## Background & Problem
`GET /api/catalogue` validates that `type` is an allowed parameter name (`internal/api/catalogue.go:64`) but never validates its value.
`catalogue.Search` only checks `typ == "function"` and `typ == "provider"`, so any other value (e.g. `type=bogus` or `type=functions`) silently matches all entries and returns the unfiltered list of 476 items with 200 OK.
Other query parameters on the API validate their arguments and return 400 Bad Request on invalid inputs (e.g. `invalid limit: "abc"`).

## Expected Behavior & Contract
When `type` is supplied with a non-empty value other than `"function"` or `"provider"`, `GET /api/catalogue` must respond with HTTP 400 Bad Request and a clear error message naming the parameter and the accepted values (e.g. `invalid type: "bogus" (must be "function" or "provider")`).

## Acceptance Test
- Unit test in `internal/api/catalogue_test.go` verifying that `GET /api/catalogue?type=bogus` and `GET /api/catalogue?type=functions` return HTTP 400 Bad Request with an appropriate error message, while valid values (`""`, `"function"`, `"provider"`) return 200 OK.
