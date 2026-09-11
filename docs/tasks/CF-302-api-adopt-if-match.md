# CF-302: handleAdoptBlueprint ignores If-Match header, overwriting concurrent edits when persisting

## Context
When a client calls `POST /api/blueprint/adopt` with `persist: true` and provides an `If-Match` HTTP header, `handleAdoptBlueprint` in `internal/api/adopt.go` ignores the header and persists the adopted blueprint over `srv.Blueprint` without checking optimistic concurrency control (OCC).
Concurrent edits made to the blueprint are silently overwritten with HTTP 200 OK instead of failing with HTTP 412 Precondition Failed.

Furthermore, in `internal/mcp/tools.go`, the `adopt_composition` tool input lacks `revision` and `if_match` fields to bridge concurrency tokens.

## Contract
1. In `internal/api/adopt.go`:
   When `req.Persist && srv.Blueprint != ""`:
   Under `srv.mu.Lock()`:
   Check `ifMatch := r.Header.Get("If-Match")`.
   If `ifMatch != ""`:
     Compute current ETag: `curBytes, err := json.Marshal(srv.loadBlueprintLocked())`.
     If `err == nil && !etagMatches(ifMatch, etagFor(curBytes))`:
       Unlock and respond with `writeJSONError(w, http.StatusPreconditionFailed, "precondition failed: If-Match header does not match current blueprint revision")`.
       Return immediately without calling `persistBlueprint`.
2. In `internal/mcp/tools.go`:
   In `adopt_composition` tool registration and handler:
   Accept optional `revision` / `if_match` string parameter, and if supplied, set `If-Match` header on the internal HTTP request.

## Acceptance Test
In `internal/api/adopt_test.go`:
`TestAdoptEndpoint_IfMatchStaleRejected`:
- Set up an API test server with a valid initial blueprint.
- Send `POST /api/blueprint/adopt` with `persist: true` and `If-Match: "\"mismatched-etag\""`.
- Verify response status is `http.StatusPreconditionFailed` (412) with error body containing `"precondition failed: If-Match header does not match current blueprint revision"`.
- Verify the blueprint on disk remains unmodified.
- Also verify matching ETag succeeds and persists (200 OK).

## Gates
- `make lint`
- `make lint-strict`
- `make test`
- `make test-race`
