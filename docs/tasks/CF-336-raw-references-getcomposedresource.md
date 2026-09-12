# Task Brief: CF-336 (Issue #225) — rawReferencesResource misses getComposedResource

## Problem Statement

In `internal/blueprint/edit.go`, `rawReferencesResource` and `rewriteRawResource` only recognize `observed.resources.<name>` and `index ... resources "<name>"`.
In Crossplane compositions using `function-go-templating`, the standard idiom for accessing composed resources and their status/conditions is the built-in helper function `getComposedResource`, e.g.:
- `{{ (getComposedResource . "main").status.id }}`
- `{{ (getComposedResource $ "main").status.id }}`
- `{{ (getComposedResource . 'main').status.id }}`
- `{{ (getResourceCondition "Ready" (getComposedResource . "main")).Status }}`

Because `rawReferencesResource` and `rewriteRawResource` miss `getComposedResource`:
1. `b.DeleteResource("main")` silently removes the target resource without refusal, leaving broken dangling references.
2. `b.RenameResource("main", "primary")` fails to identify and rewrite references inside `getComposedResource`.

## Scope of Changes

- In `internal/blueprint/edit.go`:
  - Update `rawReferencesResource(raw, name string) bool` to check for `getComposedResource` calls with the quoted resource name (matching `.`, `$`, `$.`, or variable context, and double, single, or backtick quotes). Ensure word boundaries / quote termination prevent prefix-sharing false positives (e.g. searching for `"main"` must NOT match `"main-queue"`).
  - Update `rewriteRawResource(raw, from, to string) string` to replace resource name arguments in `getComposedResource` calls from `from` to `to` across double, single, and backtick quote variants.
- In `internal/blueprint/resource_edit_test.go`:
  - Add tests for `rawReferencesResource` checking `getComposedResource` with `.`, `$`, single quotes, nested inside helper calls like `getResourceCondition`, and verifying prefix sharing is rejected.
  - Add tests for `DeleteResource` refusing when `getComposedResource` exists.
  - Add tests for `RenameResource` rewriting `getComposedResource` calls.

## Acceptance Test

Verbatim tests in `internal/blueprint/resource_edit_test.go`:
- `TestRawReferencesResource_GetComposedResource`
- `TestDeleteResource_RefusesWhenGetComposedResourceExists`
- `TestRenameResource_RewritesGetComposedResource`
