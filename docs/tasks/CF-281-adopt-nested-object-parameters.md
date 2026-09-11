# CF-281 — adopt drops or corrupts nested object parameters with 2 or more levels of nesting

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 |
| **Closes** | `CF-281 — adopt drops or corrupts nested object parameters with 2 or more levels of nesting` |
| **Worktree** | `.worktrees/CF-281` on branch `CF-281-adopt-nested-object-parameters` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | nothing |

## Symptom

Blueprint supports arbitrary nesting of object parameters (`internal/blueprint/load.go:263`: *"An object member recurses: members nest to arbitrary depth"*, and `ParamChain` walks dotted parameter paths `params.a.b.c` to any depth).

However, `internal/adopt/adopt.go` still enforces legacy depth limits and shallow single-level object handling:

1. **Classic Composition Patches Dropped**:
   In `applyPatch` (`internal/adopt/adopt.go:1918, 1936`):
   ```go
   if isParamPatch && paramName != "" && targetField != "" && !isReservedCompositeField(paramName) && isValidParamIdentifier(paramName) && len(strings.Split(paramName, ".")) <= 2 {
   ```
   When a classic composition patches a parameter nested 2 or more levels deep (e.g. `fromFieldPath: spec.parameters.network.vpc.id` or `spec.parameters.database.backup.retentionDays`), `len(strings.Split(paramName, "."))` is `3 > 2`.
   `applyPatch` drops the patch, recording `unsupported fromFieldPath "spec.parameters.network.vpc.id" in patch` in the loss report, even when the XRD explicitly defines the nested schema.

2. **Go Template and XRD-Less Adoption Fails Blueprint Validation**:
   In `ensureParamDeclaredTyped` (`internal/adopt/adopt.go:2011-2053`):
   ```go
   parts := strings.Split(paramPath, ".")
   root := parts[0]
   if len(parts) == 1 { ... }
   // Nested object member
   ...
   member := parts[1]
   if mp, mExists := rootParam.Properties[member]; !mExists {
       rootParam.Properties[member] = blueprint.Parameter{Type: typ}
   }
   ```
   When adopting a Go-template composition (or any composition where parameters are inferred) referencing a nested parameter like `{{ $spec.network.vpc.id }}`, `ensureParamDeclaredTyped` only evaluates `parts[0]` (`"network"`) and `parts[1]` (`"vpc"`), creating `"vpc"` as a leaf scalar of type `"string"`. The sub-property `"id"` is discarded.
   `extractFields` correctly wires `From: "params.network.vpc.id"`.
   When `Adopt` validates the resulting blueprint via `bp.Validate()`, `ParamChain` sees that `network.vpc` has type `"string"`, not `"object"`, and `Adopt` aborts with an error:
   `validate adopted blueprint: resource "bucket" field "vpcId": params.network.vpc has type "string", not "object" — a member reference needs an object with declared properties`.

3. **Orphan Pruning Fails to Recurse**:
   In `pruneOrphanedParameters` (`internal/adopt/adopt.go:3037-3066`), only top-level object members (`name` and `m`) are inspected. Sub-properties in nested objects (`network.vpc.id`) are not traversed or pruned recursively when unknown wired fields are pruned.

## Contract

1. **Support Arbitrary Dotted Parameter Paths in Classic Patches**:
   Remove the `len(strings.Split(paramName, ".")) <= 2` restriction in `applyPatch` (lines 1918 and 1936). Any `fromFieldPath` starting with `spec.parameters.` or `spec.` where `isValidParamIdentifier(paramName)` is true must be accepted.
2. **Recursive Parameter Declaration in `ensureParamDeclaredTyped`**:
   Update `ensureParamDeclaredTyped` to recursively walk and create intermediate `blueprint.Parameter{Type: "object", Properties: ...}` nodes for all intermediate segments `parts[1..n-1]`, setting the leaf parameter of type `typ` at `parts[n-1]`.
3. **Recursive Orphan Pruning**:
   Update `pruneOrphanedParameters` and `isParameterReferenced` so that nested object property trees are pruned recursively when their members are not referenced anywhere in the blueprint.
4. **Verification Gates**:
   - `make lint` must pass.
   - `make lint-strict` must pass.
   - `make test-race` must pass.

## Acceptance Test

1. In `internal/adopt/adopt_test.go`, add `TestAdoptClassicComposition_NestedObjectParameters`:
   - Composition with `FromCompositeFieldPath` from `spec.parameters.network.vpc.id` to `spec.forProvider.vpcId`.
   - Adopts cleanly without dropping the patch in the loss report.
   - Resource `fields["vpcId"]` has `From: "params.network.vpc.id"`.
   - `bp.Validate()` passes.
2. In `internal/adopt/adopt_test.go`, add `TestAdoptGoTemplate_NestedObjectParameters`:
   - Composition with Go template referencing `{{ $spec.network.vpc.id }}` and `{{ $spec.network.vpc.cidr }}`.
   - Adopts cleanly without validation error.
   - `bp.Spec.XRD.Parameters["network"]` has `Type: "object"`, property `"vpc"` with `Type: "object"`, and properties `"id"` and `"cidr"` with `Type: "string"`.
   - Resource field `vpcId` has `From: "params.network.vpc.id"`.
   - `bp.Validate()` passes.
