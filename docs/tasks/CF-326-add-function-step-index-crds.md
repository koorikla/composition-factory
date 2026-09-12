# CF-326 — adding functions or pipeline steps omits function input CRDs from index until restart

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: Function input CRDs omitted from /api/kinds after adding function or pipeline step until daemon restart) |
| **Closes** | `#215` — `CF-326 — adding functions or pipeline steps omits function input CRDs from index until restart` |
| **Worktree** | `.worktrees/CF-326` on branch `CF-326-add-function-step-index-crds` |
| **May write** | `internal/api/functions.go`, `internal/api/blueprint.go`, `internal/api/functions_test.go` |
| **Merges after** | `nothing` |

## Symptom

In `internal/api/functions.go`, `handleAddFunction` fetches, caches, and pins a function package in `.cf.lock`, but never updates `srv.Index` or calls `srv.rebuildIndexLocked()`.
Similarly, in `internal/api/blueprint.go:syncBlueprintSourcesLocked`, it only checks if `srv.Providers` changed; changes to `b.Spec.Pipeline` do not trigger an index rebuild.
Consequently, input CRDs for newly added functions or pipeline steps are missing from `GET /api/kinds` and schema lookups until the server is restarted.

## Acceptance test

```go
// internal/api/functions_test.go
func TestAddFunctionOmitsInputCRDFromIndex(t *testing.T) {
	const functionRef = "xpkg.crossplane.io/crossplane-contrib/function-environment-configs:v0.4.0"
	h, _ := testProviderServer(t, func(ref string) (*xpkg.Package, error) {
		return &xpkg.Package{
			Ref:    ref,
			Digest: "sha256:envdigest",
			Docs: [][]byte{
				[]byte(`apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: input.environmentconfigs.fn.crossplane.io
spec:
  group: environmentconfigs.fn.crossplane.io
  names:
    kind: Input
    plural: inputs
  scope: Namespaced
  versions:
    - name: v1beta1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
`),
			},
		}, nil
	})

	rec := do(t, h, "POST", "/api/functions", `{"ref":"`+functionRef+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var kinds struct{ Kinds []index.Kind }
	if code := getJSON(t, h, "/api/kinds?q=Input", &kinds); code != 200 {
		t.Fatalf("GET /api/kinds after add: status %d", code)
	}
	found := false
	for _, k := range kinds.Kinds {
		if k.Kind == "Input" && k.Group == "environmentconfigs.fn.crossplane.io" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Input kind from added function not found in /api/kinds: %+v", kinds.Kinds)
	}
}
```

## Contract

1. In `internal/api/functions.go`:
   - In `handleAddFunction`, after pinning and caching the function, call `srv.rebuildIndexLocked(srv.doc)` so its input CRDs are indexed.
2. In `internal/api/blueprint.go`:
   - In `syncBlueprintSourcesLocked`, also check if pipeline packages changed (or if `b.Spec.Pipeline` changed) compared to the current blueprint, and rebuild the index if new function packages were added.
3. Add acceptance test in `internal/api/functions_test.go`.
4. Verify all api tests pass.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Changes outside `internal/api/`.

## Handover

Branch `CF-326-add-function-step-index-crds`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
