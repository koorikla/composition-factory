# CF-355 — rawReferencesResource misses hasKey on observed.resources, permitting deletion and failing renames

## 1. Context & Invariant

In `internal/blueprint/edit.go`, `rawReferencesResource` and `rewriteRawResource` inspect and rewrite raw expressions and template bodies for references to composed resources.

In Crossplane `function-go-templating`, template authors frequently guard composed resource lookups and status access with `hasKey`, e.g.:
- `{{- if hasKey $.observed.resources "main-queue" }}`
- `{{- if hasKey .observed.resources "main-queue" }}`
- `{{- if hasKey $observed.resources "main-queue" }}`
- `{{- if hasKey observed.resources "main-queue" }}`

Because `rawReferencesResource` and `rewriteRawResource` do not recognize `hasKey` expressions on `observed.resources`:
1. `b.DeleteResource("main-queue")` relies on `b.StatusReferencingResources("main-queue")` (which delegates to `rawReferencesResource` via `anyStatusFrom`) and `b.Spec.Templates`. Because `rawReferencesResource` returns `false`, `DeleteResource` silently removes the resource without refusal, leaving broken dangling references in dependent resources and templates.
2. `b.RenameResource("main-queue", "primary-queue")` fails to rewrite `hasKey` checks in `r.Fields`, `r.Envelope`, `r.Annotations`, and `cp.Spec.Templates`. The old resource name (`"main-queue"`) remains in place, so `hasKey` evaluates to `false` during composition rendering, silently dropping conditional configurations or resources.

## 2. Requirements & Contract

1. In `internal/blueprint/edit.go`:
   - `rawReferencesResource` must recognize `hasKey` queries on `(?:(?:\$|\$\.|\.)?(?:observed\.)?resources)`.
   - `rewriteRawResource` must rewrite resource arguments inside `hasKey` calls from `from` to `to`.
2. Guard with automated unit tests in `internal/blueprint/resource_edit_test.go`:
   - `TestRawReferencesResource_HasKey`
   - `TestDeleteResource_RefusesWhenHasKeyObservedResources`
   - `TestRenameResource_RewritesHasKeyObservedResources`
3. Ensure `make lint && make lint-strict && make test-race` passes.
