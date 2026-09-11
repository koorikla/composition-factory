# CF-265 — adopt prunes dot-notation map patches like spec.forProvider.tags.env, dropping fields and parameters

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: adopt fieldpath fidelity for map patches) |
| **Closes** | `CF-265 — adopt prunes dot-notation map patches like spec.forProvider.tags.env, dropping fields and parameters` |
| **Worktree** | `.worktrees/CF-265` on branch `CF-265-adopt-dot-map-patch` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go` |
| **Merges after** | `CF-263` |

## Symptom

In Crossplane Compositions, patches targeting map fields (e.g. `spec.forProvider.tags.<key>` or `spec.forProvider.labels.<key>`) are commonly written with dot notation:
```yaml
- type: FromCompositeFieldPath
  fromFieldPath: spec.parameters.env
  toFieldPath: spec.forProvider.tags.env
```
When `cf adopt` processes such a patch, it assigns `res.Fields["tags.env"] = Field{From: "params.env"}`. However, `blueprint.ParseFieldPath("tags.env")` expects map bracket syntax (`tags[env]`). When `pruneUnknownFields` validates `res.Fields` against provider CRD OpenAPI schema, `"tags.env"` is not recognized as a map access, fails schema lookup (since only `"tags"` exists in schema leaves), and is pruned as an unknown field. Subsequently, `pruneOrphanedParameters` deletes the corresponding XRD parameter because no fields reference it anymore.

The patch and parameter are silently wiped from the adopted blueprint with:
```
# adopt: dropped resource.q.fields.tags.env (field "tags.env" is not in Queue spec.forProvider (unknown field pruned by schema))
# adopt: dropped xrd.parameters.env (parameter orphaned by pruned unknown field dropped)
```

## Mechanism

1. In `internal/adopt/adopt.go:1897-1906`:
   ```go
   if strings.HasPrefix(toPath, "spec.forProvider.") {
       targetField := strings.TrimPrefix(toPath, "spec.forProvider.")
       ...
       res.Fields[targetField] = blueprint.Field{From: "params." + paramName}
   ```
   `targetField` preserves the raw dot notation (`tags.env`) instead of converting it to map bracket notation (`tags[env]`), unlike `metadata.labels` handling at line 1953.
2. In `internal/adopt/adopt.go:2813-2828`:
   `pruneUnknownFields` parses `targetField` using `blueprint.ParseFieldPath("tags.env")`, returning `isMap = false`. Schema validation fails to find `tags.env` in the known field list, pruning the field.
3. In `internal/adopt/adopt.go:2892-2900`:
   `pruneOrphanedParameters` drops `xrd.parameters.env` as orphaned.

## Contract

1. In `internal/adopt/adopt.go`, normalize dot-notation map field paths under `spec.forProvider` when the prefix corresponds to a known map property in the schema or a recognized map field (such as `tags`, `labels`, or any property whose OpenAPI schema defines `additionalProperties`):
   - Convert `tags.<key>` to `tags[<key>]`.
2. `pruneUnknownFields` must recognize the bracket notation and validate that the map root exists in the CRD schema.
3. The field and XRD parameter must be retained in the adopted blueprint and round-trip successfully through `cf gen`.

## Acceptance Test

Unit test `TestAdopt_DotNotationMapPatch` in `internal/adopt/adopt_test.go`:
Adopt a Composition containing a patch to `spec.forProvider.tags.env`.
Verify:
1. `bp.Spec.Resources["q"].Fields["tags[env]"]` is populated with `From: "params.env"`.
2. `bp.Spec.XRD.Parameters["env"]` is preserved.
3. No schema drop warning is issued for `tags.env` or `tags[env]`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-265-adopt-dot-map-patch`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
