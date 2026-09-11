# Task Brief: CF-297 — adopt discoverObjectParamsFromPatches skips nested parameters, wiring whole objects and crashing

## Context & Problem

When adopting a Crossplane Composition containing nested object parameters where a patch referencing a parent object precedes a patch referencing a nested member (e.g. `spec.parameters.network.vpc` before `spec.parameters.network.vpc.id`), `cf adopt` fails validation and crashes with exit code 1 instead of dropping the unsupported whole-object wire into the loss report:

\`\`\`
cf: error: adopt composition: validate adopted blueprint: resource "bucket" field "objectLockEnabled": parameter "network" is a typed object — a from: mapping cannot render the whole object; wire one of its declared members instead (params.network.<member>; declared members: id)
\`\`\`

## Root Cause

In `internal/adopt/adopt.go:2007` (`discoverObjectParamsFromPatches`):
\`\`\`go
if paramName != "" && strings.Contains(paramName, ".") && !isReservedCompositeField(paramName) && isValidParamIdentifier(paramName) && len(strings.Split(paramName, ".")) <= 2 {
    ensureParamDeclared(bp, paramName)
}
\`\`\`
Line 2007 retains the legacy restriction `len(strings.Split(paramName, ".")) <= 2`. When CF-281 lifted the 2-level nesting limit across `applyPatch` and updated `ensureParamDeclared` to build arbitrary property trees, `discoverObjectParamsFromPatches` was overlooked.

Because `paramName` for nested members like `network.vpc.id` has 3 segments (`len(strings.Split("network.vpc.id", ".")) == 3`), line 2007 skips `ensureParamDeclared(bp, "network.vpc.id")` during the pre-discovery pass.

Consequently, when `applyPatch` evaluates an earlier patch referencing the parent object `spec.parameters.network.vpc`, `isWholeObjectParam(bp, "network.vpc")` returns `false` because `network.vpc` has not yet been registered as an object with child properties. `applyPatch` therefore wires `From: "params.network.vpc"`. When the subsequent patch for `network.vpc.id` is processed, `ensureParamDeclared` adds `id` under `network.vpc`, making `network.vpc` an object.

At the end of adoption, `bp.Validate()` detects that `params.network.vpc` is wired directly into a resource field, and rejects it with validation error, aborting `cf adopt` with exit code 1.

## Fix

In `internal/adopt/adopt.go:2007`, remove `len(strings.Split(paramName, ".")) <= 2`:
\`\`\`go
if paramName != "" && strings.Contains(paramName, ".") && !isReservedCompositeField(paramName) && isValidParamIdentifier(paramName) {
    ensureParamDeclared(bp, paramName)
}
\`\`\`

## Acceptance Test

In `internal/adopt/adopt_test.go`, add `TestCF297_AdoptNestedObjectParams`:
Feed a composition where a patch from `spec.parameters.network.vpc` precedes a patch from `spec.parameters.network.vpc.id`:
- `cf adopt` succeeds without validation failure.
- `report.Drops` contains the dropped whole-object wire (`spec.parameters.network.vpc`).
- `bp.Spec.Resources` has `params.network.vpc.id` correctly wired.
- `bp.Validate()` passes cleanly.

## Gates

- `make lint`
- `make lint-strict`
- `make test`
- `make test-race`
