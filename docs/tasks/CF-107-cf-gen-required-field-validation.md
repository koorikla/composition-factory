# CF-107 — Omitting a CRD-required field passes cf gen and API generation

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (Strict schema validation contract broken; generation succeeds on invalid resources) |
| **Closes** | `CF-107 — *(engine)* Omitting a CRD-required field (region on every Bucket) passes cf gen, PUT /api/blueprint and POST /api/generate with exit 0 and no warning; only --validate catches it.` |
| **Worktree** | `.worktrees/CF-107` on branch `CF-107-cf-gen-required-field-validation` |
| **May write** | `internal/emit/`, `internal/api/`, `cmd/cf/`, `docs/tasks/CF-107*` |
| **Merges after** | nothing |

## Symptom

Omitting a CRD-required field (such as `region` on SQS `Queue` or S3 `Bucket`) passed `cf gen`, `PUT /api/blueprint`, and `POST /api/generate` with exit 0 and no warning. Only `--validate` (which invoked `crossplane render`) caught it.
The API server rejects the composed resource at apply time. README:10 and AGENTS.md §1 claim strict validation at generate time; generation must refuse when an effective-required field has no value, wire, or guard.

## Solution

1. Implement `CheckRequiredFields(b *blueprint.Blueprint, crds []schema.CRD) error` in `internal/emit/plan.go`.
2. Inspect schema `RequiredLeaves` and `RequiredBranches` against blueprint resource fields (taking into account conventions, native kinds, and optional parameter wires).
3. Call `CheckRequiredFields` in `emit.Generate` and API server's `validateBlueprintAgainstCRDs`.
4. Ensure typo / unknown-field suggestions from `emit.Composition` run before required-field checks so nearest-match error reporting is preserved.
5. Add unit and CLI tests in `internal/emit/emit_test.go` and `cmd/cf/gen_test.go`.

## Acceptance test

- `TestGenerateRejectsMissingRequiredField` in `internal/emit/emit_test.go`:
  When a required field like `region` is omitted, `Generate` returns an error `missing required field "region" in Queue spec.forProvider`.
- `TestGenRejectsMissingRequiredField` in `cmd/cf/gen_test.go`:
  `cf gen` exits non-zero with error mentioning `missing required field "region"`.
