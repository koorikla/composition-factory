# CF-307 — PreviewExpression helper signatures invert arguments and drop composite resource context

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (Engine scale: in-process template and expression preview fails with standard helper functions) |
| **Closes** | `#196` — `CF-307 — PreviewExpression helper signatures invert arguments and drop composite resource context` |
| **Worktree** | `.worktrees/CF-307` on branch `CF-307-preview-expression-helper-signatures` |
| **May write** | `internal/emit/preview.go`, `internal/emit/preview_test.go` |
| **Merges after** | `nothing` |

## Symptom

Crossplane's `function-go-templating` injects helper functions for querying observed state: `getComposedResource(req, name)`, `getCompositeResource(req)`, and `getExtraResources(req, name)`.
In `internal/emit/preview.go`, these functions have inverted parameter signatures and incorrect context path dereferencing, causing in-process template and expression previews to fail with type mismatch or nil dereference errors.

## Evidence

In `internal/emit/preview.go:281-306`:
1. `funcs["getComposedResource"] = func(name string, observed any) any`: Arguments are declared in inverted order (`name` first, `observed` second). In standard Crossplane compositions, templates invoke `{{ (getComposedResource . "name").status }}`. Passing `.` as the first argument fails with `wrong type for value; expected string; got map[string]interface {}`.
2. `funcs["getCompositeResource"] = func(observed any) any`: Expects `obsMap["composite"]`. When passed the root template context `.`, the map contains key `"observed"`, not `"composite"`. The function returns `nil`, causing `{{ (getCompositeResource .).metadata.name }}` to fail with `nil data; no entry for key "metadata"`.
3. `funcs["getExtraResources"] = func(name string, extra any) any`: Also inverts arguments.
4. `funcs["getResourceCondition"] = func(condType string, res any) map[string]any`: Looks directly for `resMap["status"]`. When passed `(index .observed.resources "name")`, the map contains `"resource"` which contains `"status"`.

## Acceptance test

```go
// internal/emit/preview_test.go
func TestPreviewExpression_CrossplaneHelperSignatures(t *testing.T) {
	bp := &blueprint.Blueprint{
		Spec: blueprint.Spec{
			XRD: blueprint.XRD{Kind: "XDatabase"},
			Resources: []blueprint.Resource{
				{Name: "db-instance", Kind: "Instance"},
			},
		},
	}

	t.Run("getComposedResource standard signature", func(t *testing.T) {
		res, err := PreviewExpression(bp, "", `{{ (getComposedResource . "db-instance").status.atProvider.id }}`)
		if err != nil {
			t.Fatalf("getComposedResource failed: %v", err)
		}
		if res != "db-instance-id-12345" {
			t.Errorf("got %q, want %q", res, "db-instance-id-12345")
		}
	})

	t.Run("getCompositeResource standard signature", func(t *testing.T) {
		res, err := PreviewExpression(bp, "", `{{ (getCompositeResource .).metadata.name }}`)
		if err != nil {
			t.Fatalf("getCompositeResource failed: %v", err)
		}
		if res != "sample-xdatabase" {
			t.Errorf("got %q, want %q", res, "sample-xdatabase")
		}
	})
}
```

**Fails today with:**
```
getComposedResource failed: template: preview:8:24: executing "preview" at <.>: wrong type for value; expected string; got map[string]interface {}
getCompositeResource failed: template: preview:8:25: executing "preview" at <.>: nil data; no entry for key "metadata"
```

## Contract

1. In `internal/emit/preview.go`:
   - `getComposedResource`: Accept `(req any, name string) any`. Support both root context `.` (extracting `req["observed"]["resources"][name]["resource"]` or `req["resources"][name]["resource"]`) and directly passed observed resources map. Support inverted arguments `(name string, req any)` as a fallback for backwards compatibility if needed.
   - `getCompositeResource`: Accept `(req any) any`. If `req` contains `"observed"`, inspect `req["observed"]["composite"]["resource"]`. If `req` contains `"composite"`, inspect `req["composite"]["resource"]`. Return the composite resource map.
   - `getExtraResources`: Accept `(req any, name string) any` and fallback `(name string, req any)`.
   - `getResourceCondition`: Accept `(condType string, res any) map[string]any`. If `res` has key `"resource"`, inspect inner resource map for `"status"`.
2. Standard Crossplane template expressions evaluating `getComposedResource` and `getCompositeResource` must succeed in `PreviewExpression`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Out of scope

- Real cluster preview (handled in `cf serve --cluster`).

## Handover

Branch `CF-307-preview-expression-helper-signatures`, committed, not pushed, not merged. Include failing and passing test logs in the handover note.
