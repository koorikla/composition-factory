# CF-227 — add_provider accepts function packages as providers, producing invalid ClusterProviderConfig manifests

## Severity & Scope
- **Severity**: P1
- **Scale**: engine
- **Touch set**: `internal/api/providers.go`, `internal/api/server_test.go`, `internal/mcp/server_test.go`, `docs/tasks/CF-227-reject-function-as-provider.md`

## Background & Problem
`internal/api/providers.go:220` (`handleAddProvider`) calls `srv.Store.FetchAndSave` but ignores the returned `crds` slice and omits the function-vs-provider validation check that `cmd/cf/provider.go:40-52` enforces. As a result, calling MCP `add_provider` or `POST /api/providers` with an xpkg function reference (e.g. `ghcr.io/crossplane-contrib/function-go-templating:v0.11.0`) succeeds, appends the function reference to `spec.sources` in the blueprint, and subsequent generation via `cf gen` or `/api/generate` emits invalid `providerconfigs/<function>.yaml` with non-existent `kind: ClusterProviderConfig`.

### Root Cause
In `internal/api/providers.go:220`:
```go
	pkg, _, err := srv.Store.FetchAndSave(r.Context(), srv.Lock, req.Ref, srv.fetch)
```
Unlike `internal/api/functions.go:78-91` and `cmd/cf/provider.go:40-52`, `handleAddProvider` does not check if the package CRDs contain function inputs (`crd.IsFunctionInput() || crd.Function`) while containing zero managed resources (`crd.IsManaged()`).

## Expected Behavior & Contract
1. In `internal/api/providers.go:handleAddProvider`:
   - Inspect `crds` returned from `srv.Store.FetchAndSave`:
     ```go
     managed := 0
     inputs := 0
     for _, crd := range crds {
         if crd.IsManaged() {
             managed++
         }
         if crd.IsFunctionInput() || crd.Function {
             inputs++
         }
     }
     if inputs > 0 && managed == 0 {
         writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("package %q is a function package, not a provider (use 'cf function add %s')", req.Ref, req.Ref))
         return
     }
     ```
   - Matches the error contract of `cmd/cf/provider.go:51` verbatim.
2. Error parity in MCP:
   - MCP `add_provider` with a function package returns error parity with HTTP `POST /api/providers`.

## Acceptance Test
- In `internal/api/server_test.go`:
  - Verify that `POST /api/providers` with a function package (e.g. seeded in store with function CRD) returns HTTP 400 Bad Request with error: `package "<ref>" is a function package, not a provider (use 'cf function add <ref>')`.
- In `internal/mcp/server_test.go`:
  - `TestAddProviderFailuresMatchHTTP`: verify that `add_provider` with a function package fails with error parity matching HTTP `POST /api/providers`.
