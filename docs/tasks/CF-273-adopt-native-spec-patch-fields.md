# CF-273 — adopt routes native K8s spec patches into envelope, causing gen failure and crashes on list paths

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: native Kubernetes kind patch routing in adopt) |
| **Closes** | `CF-273 — adopt routes native K8s spec patches into envelope, causing gen failure and crashes on list paths` |
| **Worktree** | `.worktrees/CF-273` on branch `CF-273-adopt-native-spec-patch-fields` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-266` |

## Symptom

When adopting a Crossplane Composition containing native Kubernetes resources (`provider: k8s`, e.g. `apps/v1` `Deployment`, `Service`, `ConfigMap`, `Secret`), patches targeting fields under `spec.` (e.g. `spec.replicas` or `spec.template.spec.containers[0].image`) are routed into `res.Envelope` rather than `res.Fields`.

This causes two severe failures:
1. **Validation crash on list/subpath indexing:** If the target field path contains array indices or non-camelCase segments (such as `spec.template.spec.containers[0].image`), `internal/blueprint/envelope.go:validateResourceEnvelope` rejects the path (`segment "containers[0]" is not a valid envelope key`), causing `cf adopt` to abort with a fatal validation error.
2. **Generation refusal on simple spec paths:** If the target field path happens to be a simple camelCase identifier (such as `spec.replicas`), `cf adopt` outputs a blueprint with `envelope: replicas: from: params.replicas`. However, native Kubernetes kinds have no Crossplane envelope (`crd.Native == true`). Running `cf gen` on the adopted blueprint immediately aborts with:
   `resource "deployment": kind "Deployment" is a native Kubernetes kind, and a native object has no Crossplane envelope — it is the composed object itself, not a managed resource, so there is no managementPolicies, writeConnectionSecretToRef or providerConfigRef to set. Remove the envelope block; every settable field is addressable through fields: (e.g. spec.template.spec.containers[0].image)`

This directly breaks the Round-Trip Rule (`cf adopt` -> `cf gen` must round-trip) for any Composition utilizing native Kubernetes kinds with spec parameter patches.

## Mechanism

In `internal/adopt/adopt.go:1932-1945` (`applyPatch`):
```go
} else if strings.HasPrefix(toPath, "spec.") {
    targetField := strings.TrimPrefix(toPath, "spec.")
    targetField = normalizeMapFieldPath(targetField)
    if isParamPatch && paramName != "" && targetField != "" && isValidParamIdentifier(paramName) && len(strings.Split(paramName, ".")) <= 2 {
        if res.Envelope == nil {
            res.Envelope = make(map[string]blueprint.Field)
        }
        res.Envelope[targetField] = blueprint.Field{
            From: "params." + paramName,
        }
        ensureParamDeclared(bp, paramName)
    }
```
`applyPatch` assumes that any `toPath` starting with `spec.` (other than `spec.forProvider.` and `spec.initProvider.`) is a Crossplane managed resource envelope key (such as `spec.providerConfigRef.name` or `spec.managementPolicies`). It fails to check if `res.Provider == blueprint.NativeProvider`.

By contrast:
- `internal/adopt/adopt.go:2296` (`extractFields`) correctly places base spec fields for native resources under `res.Fields` with prefix `"spec"` (e.g. `res.Fields["spec.replicas"]`).
- `internal/adopt/adopt.go:1964` explicitly checks `if res.Provider != blueprint.NativeProvider` when handling `metadata.` fields.
- `internal/emit/envelope.go:43-49` (`checkEnvelopePaths`) strictly rejects any native Kubernetes resource that specifies an `envelope` block.

## Contract

1. In `internal/adopt/adopt.go` (`applyPatch`):
   - Check if `res.Provider == blueprint.NativeProvider`:
     - If `res.Provider == blueprint.NativeProvider` and `toPath` starts with `spec.`, route the patch directly into `res.Fields[toPath]` (e.g. `res.Fields["spec.replicas"]` or `res.Fields["spec.template.spec.containers[0].image"]`) with `From: "params." + paramName`.
     - Do not create or populate `res.Envelope` for native resources.
2. Verify that both simple spec paths (e.g. `spec.replicas`) and indexed subpaths (e.g. `spec.template.spec.containers[0].image`) adopt cleanly into `res.Fields` without validation aborts.
3. Verify that the adopted blueprint passes `bp.Validate()` and round-trips successfully through `cf gen`.

## Acceptance Test

Go unit test in `internal/adopt/adopt_test.go`:
1. Adopt a Composition with a native `Deployment` (`provider: k8s`) having patches to `spec.replicas` and `spec.template.spec.containers[0].image`.
2. Verify adoption succeeds without error.
3. Verify `bp.Spec.Resources[0].Envelope` is nil or empty.
4. Verify `bp.Spec.Resources[0].Fields["spec.replicas"].From == "params.replicas"`.
5. Verify `bp.Spec.Resources[0].Fields["spec.template.spec.containers[0].image"].From == "params.image"`.
6. Pass the adopted blueprint to `emit.Generate` and verify it generates the Composition YAML without envelope errors.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-273-adopt-native-spec-patch-fields`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
