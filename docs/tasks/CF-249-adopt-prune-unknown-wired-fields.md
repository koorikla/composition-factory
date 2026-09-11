# CF-249 — Import keeps a wired forProvider field the CRD lacks; the next preview, Validate and Generate refuse it

## Severity & Scope
- **Severity**: P2
- **Scale**: engine
- **Touch set**: `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go`

## Background & Problem
`pruneUnknownForProviderFields` (`internal/adopt/adopt.go:2589`) drops unknown literal forProvider fields and names them in the loss report, but at `adopt.go:2654-2656` it skips every field where `fld.From != "" || fld.Raw != "" || fld.Template != ""`.
A wired field like `retentionDays: {{ $spec.retentionDays }}` (which is not a field in the CRD schema for SQS Queue) survives as `fields.retentionDays: {from: params.retentionDays}` while its literal sibling `bogusSetting: 'yes'` is pruned.
The adopted blueprint is subsequently rejected by `internal/emit/composition.go:540`: preview generate answers 400, and Validate/Generate stop with:
`resource "main-queue": field "retentionDays" is not in Queue spec.forProvider (an unknown field is silently pruned by the API server on apply, so it must be caught here)`.

## Expected Behavior & Contract
1. In `pruneUnknownForProviderFields`, all fields under `r.Fields` not defined in the CRD schema's `spec.forProvider` must be pruned and recorded in the loss report, including wired (`From`), `Raw`, or `Template` fields.
2. In XRD-less adoption (when no XRD is provided and no BaseBlueprint contains the parameter), any parameter in `bp.Spec.XRD.Parameters` that was orphaned because all its referencing fields were pruned is removed (and reported in the loss report) so that the adopted blueprint validates and generates cleanly.

## Acceptance Test
- `TestPruneUnknownWiredFields` in `internal/adopt/adopt_test.go`:
  - Adopt a Composition containing both an unknown literal field (`bogusSetting: 'yes'`) and an unknown wired field (`retentionDays: {{ $spec.retentionDays }}`).
  - Both unknown fields are pruned and named in the loss report.
  - The adopted blueprint passes `Validate()` and `emit.Generate()` without schema errors.
