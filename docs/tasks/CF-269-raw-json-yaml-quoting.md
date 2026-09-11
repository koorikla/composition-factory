# CF-269 — raw JSON in string field emitted as unquoted YAML mapping, failing cf gen --validate and adopt

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Engine scale: emission quoting for JSON string scalars) |
| **Closes** | `CF-269 — raw JSON in string field emitted as unquoted YAML mapping, failing cf gen --validate and adopt` |
| **Worktree** | `.worktrees/CF-269` on branch `CF-269-raw-json-yaml-quoting` |
| **May write** | `internal/emit/structured.go`, `internal/emit/structured_test.go` |
| **Merges after** | `CF-266` |

## Symptom

When a blueprint defines a raw JSON string for a string field (e.g. `testdata/irsa.cf.yaml:96` setting `policy: {raw: '{"Version":"2012-10-17",...}'}` on `RolePolicy.spec.forProvider.policy`), the emitter outputs `policy: {"Version":...}` as an unquoted inline mapping rather than a quoted string scalar or block scalar.

This causes:
1. `cf gen --validate` to fail: `cf: error: render validation failed: line 85: resource "role-policy" (RolePolicy): field "spec.forProvider.policy": invalid type: expected string, got object`.
2. Adopting the generated output with `cf adopt` fails: the schema prunes inner properties as unknown fields on an object, silently dropping the `policy` field completely from the adopted blueprint.
3. This breaks the Section 1 Round-Trip Rule (`cf gen` -> `cf adopt` -> `cf gen` reproducing original fields).

## Mechanism

In `internal/emit/structured.go:150-154`:
```go
case f.Raw != "":
    rhs = blueprint.NormalizeRawGoTemplate(f.Raw)
```
`NormalizeRawGoTemplate` strips template escaping but leaves the string value unquoted. When the string contains valid JSON syntax starting with `{` or `[`, YAML parsers parse it as a nested mapping or sequence rather than a string scalar.

## Contract

1. In `internal/emit/structured.go`:
   - When emitting a field whose target OpenAPI schema type is `string` (or when `f.Raw` starts with `{` or `[` and is destined for a string field), emit it as a quoted string scalar or YAML block scalar (e.g. `|` or double-quoted with proper escaping) rather than raw unquoted JSON mapping.
2. Verify that running `cf gen testdata/irsa.cf.yaml --validate` passes without schema type mismatch errors.
3. Verify that adopting the resulting generated Composition with `cf adopt` preserves the `policy` field without schema pruning drops.

## Acceptance Test

Go unit test in `internal/emit/structured_test.go`:
1. Generate Composition for a blueprint defining a raw JSON policy string on a string field (`spec.forProvider.policy`).
2. Verify that the emitted YAML treats `policy` as a string scalar (validating with `cf gen --validate` schema validation).
3. Round-trip adopt the generated Composition through `adopt.Adopt` and assert that `res.Fields["policy"]` is retained with its raw JSON value and no drops are recorded for `policy`.

## Verification

```sh
make lint && make lint-strict && make test-race
```

## Handover

Branch `CF-269-raw-json-yaml-quoting`, committed, not pushed, not merged. Final report includes failing (RED) and passing (GREEN) runs of the acceptance test and all standard gates.
