# CF-274 — adopt wires whole object parameters into from fields, causing validation failure and crashing CLI

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: object parameter patch handling in adopt) |
| **Closes** | `CF-274 — adopt wires whole object parameters into from fields, causing validation failure and crashing CLI` |
| **Worktree** | `.worktrees/CF-274` on branch `CF-274-adopt-whole-object-param-drop` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-266` |

## Symptom

When adopting a Crossplane Composition where a patch references a composite parameter that is a typed object (e.g. `fromFieldPath: spec.parameters.config`), `cf adopt` wires the entire object directly into a resource field (`from: params.config`).

When the adopted blueprint is validated via `bp.Validate()`, validation rejects whole-object wiring with:
```
cf: error: adopt composition: validate adopted blueprint: resource "bucket" field "objectLockEnabled": parameter "config" is a typed object — a from: mapping cannot render the whole object; wire one of its declared members instead (params.config.<member>; declared members: region)
```
`cf adopt` aborts with exit code 1, crashing the CLI instead of adopting the remaining resources and cleanly recording the unsupported whole-object wire in `LossReport`.

## Mechanism

In `internal/adopt/adopt.go:1915-1925` (`applyPatch`):
```go
if strings.HasPrefix(toPath, "spec.forProvider.") {
    targetField := strings.TrimPrefix(toPath, "spec.forProvider.")
    targetField = normalizeMapFieldPath(targetField)
    if isParamPatch && paramName != "" && targetField != "" && !isReservedCompositeField(paramName) && isValidParamIdentifier(paramName) && len(strings.Split(paramName, ".")) <= 2 {
        if res.Fields == nil {
            res.Fields = make(map[string]blueprint.Field)
        }
        res.Fields[targetField] = blueprint.Field{
            From: "params." + paramName,
        }
        ensureParamDeclared(bp, paramName)
    }
```
`applyPatch` extracts `paramName` from `fromFieldPath`. If `paramName` has no dots (e.g. `config`), it assigns `From: "params.config"`.
However, if `config` is already declared in `bp.Spec.XRD.Parameters` as `type: object`, or if other patches reference members of `config` causing it to be inferred as an object parameter, a wire of the form `from: params.config` is invalid according to `internal/blueprint/validate_fields.go:175-188`.

## Contract

1. In `internal/adopt/adopt.go`:
   - Before wiring `From: "params." + paramName`, check if `paramName` refers to an object parameter without member qualification:
     - If `param` is known to be an object (or after patch collection, any field referencing a root object parameter without member notation), record an entry in `LossReport`:
       `report.Record(patchPath, fmt.Sprintf("unsupported whole-object parameter wire from %q to %q; wire individual object members instead", fromPath, toPath))`
     - Do not assign the invalid `From: "params." + paramName` field wire.
2. Ensure `cf adopt` completes successfully, returns exit code 2 (indicating recorded loss), and emits a valid blueprint that passes `bp.Validate()`.

## Acceptance Test

Go unit test in `internal/adopt/adopt_test.go`:
1. Adopt a Composition with a patch from `spec.parameters.config` to `spec.forProvider.objectLockEnabled` alongside member patch `spec.parameters.config.region` to `spec.forProvider.region`.
2. Verify that `Adopt` completes without error.
3. Verify that `res.Fields["objectLockEnabled"]` is not wired to `params.config`.
4. Verify that `res.Fields["region"]` is wired to `params.config.region`.
5. Verify that `report.Drops` contains an entry recording the whole-object patch drop.
6. Verify that `bp.Validate()` succeeds.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-274-adopt-whole-object-param-drop`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
