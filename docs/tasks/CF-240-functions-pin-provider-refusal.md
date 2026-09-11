# CF-240 — POST /api/functions answers 400 for a provider package but still pins it into the lockfile

## Severity & Scope
- **Severity**: P2
- **Scale**: engine
- **Touch set**: `internal/api/functions.go`, `internal/api/functions_test.go` (and `internal/cache/store.go` if helper needed)

## Background & Problem
`POST /api/functions` with a package ref that turns out to be a provider package correctly answers 400 Bad Request ("package ... is a provider package, not a function (use 'cf provider add ...')").
However, `internal/api/functions.go:68` calls `srv.Store.FetchAndSave(r.Context(), srv.Lock, req.Ref, srv.fetch)` *before* inspecting whether the package's CRDs classify it as a function or a provider (`functions.go:88`).
Because `FetchAndSave` pins the lockfile before or immediately upon fetching, the lockfile gets updated with a pin for that provider package even though the request was refused with 400.
The lockfile is the reproducibility record: a 4xx request must not mutate the lockfile with un-added packages.

## Expected Behavior & Contract
When `POST /api/functions` receives a package ref that is classified as a provider package (or fails validation), it must answer 400 Bad Request and leave the lockfile completely unchanged (no pin added).
The pin must only be written to `srv.Lock` once the package is confirmed to be a valid function package.

## Acceptance Test
- Test in `internal/api/functions_test.go`:
  Simulate `POST /api/functions` with a provider package ref (having managed CRDs and no function inputs). Verify that the handler returns HTTP 400 and that the lockfile does not contain the ref.
