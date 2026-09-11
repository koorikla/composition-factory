# CF-236 — composition-only adopt replaces the pinned function-auto-ready package with a hardcoded default and never reports it

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 (engine scale: round-trip fidelity & schema validity) |
| **Closes** | `CF-236 — composition-only adopt replaces the pinned function-auto-ready package with a hardcoded default and never reports it` (#131) |
| **Worktree** | `.worktrees/CF-236` on branch `CF-236-adopt-reported-auto-ready-pkg` |
| **May write** | `internal/adopt/adopt.go`, `internal/adopt/tree.go`, `internal/adopt/adopt_test.go`, `internal/adopt/cf187_adopt_test.go`, `internal/adopt/environment_test.go` |
| **Merges after** | nothing |

## Symptom

When a Composition is adopted without its `functions.yaml` (the form `kubectl get composition -o yaml` gives), `internal/adopt/adopt.go` assigns `pkg = "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"` to any `function-auto-ready` step whose package is unknown, and treats the step as the built-in default and drops it from `spec.pipeline`. The loss report never mentions the pipeline; adopt exits 2 only for unrelated parameter facts. Subsequent `cf gen` regenerates `functions.yaml` with the default package version (e.g. `v0.5.0`), silently downgrading any pinned version (e.g. `v0.5.1` from `testdata/xqueue-pipeline.cf.yaml`).

## Mechanism

In `internal/adopt/adopt.go:parsePipelineComposition`:
1. Steps referencing `function-auto-ready` whose package cannot be read from `step["input"]["package"]` or `opts.FunctionPackages` are assigned the hardcoded default package `xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0`.
2. When there are no other custom steps, the auto-ready step is dropped from `spec.pipeline` without recording anything in `report`.
3. In composition-only adoptions (where no XRD is present and no `functions.yaml` is provided), assuming the default package loses any pinned package version without alerting the user in the loss report.
4. When an XRD is present alongside the Composition (such as in a Configuration package tree, multi-document import, or Lane C live export), `function-auto-ready` is the standard engine pipeline function and is handled losslessly.

## Contract

1. In `internal/adopt/adopt.go:parsePipelineComposition`, track whether a step's package reference was assumed due to absent `functions.yaml`.
2. In composition-only adoptions (where no XRD document is provided), if `function-auto-ready` has an assumed package, record it in the `LossReport`: `report.Record("pipeline."+stepID, fmt.Sprintf("without functions.yaml, function package could not be recovered (assumed default %s)", pkg))`.
3. When XRD is present (Configuration tree or multi-document import), `function-auto-ready` without custom inputs remains the default engine step and is not reported as a loss, preserving lossless round-tripping for Section 1 / Lane C and CF-217.
4. For custom pipeline functions (not `function-auto-ready`), an unrecovered package is always recorded as a loss in `report`.
5. When `functions.yaml` (or `kind: Function` document) is supplied in the input, the package is recovered from the document, no loss is reported, and any non-default package version is preserved in `bp.Spec.Pipeline`.

## Acceptance Test

Unit test `TestCF236_AdoptFunctionPackageLossReport` in `internal/adopt/adopt_test.go`:
- Adopts a Composition alone referencing `function-auto-ready` without `functions.yaml`. Asserts that `report.IsLossy()` is true, drop `pipeline.auto-ready` exists, and the reason mentions `without functions.yaml` and `xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0`.
- Adopts the same Composition with a `kind: Function` manifest specifying `v0.5.1`. Asserts that no `pipeline.` loss is recorded.

## Verification

```sh
make lint && make lint-strict && make test-race && make test-docker
```

## Handover

Branch `CF-236-adopt-reported-auto-ready-pkg`, committed in `.worktrees/CF-236`, not pushed, not merged.
