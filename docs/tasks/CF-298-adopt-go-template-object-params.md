# CF-298: Adopt crashes on Go templates referencing object parameters instead of dropping in loss report

## Context
When adopting a Crossplane Composition with Go templating where an expression references an XRD parameter that is a typed object (e.g. `{{ .observed.composite.resource.spec.config }}` or `{{ $spec.config }}`), `cf adopt` wires the parameter directly into fields, envelope, or annotations (`from: params.config`).
Subsequent `bp.Validate()` rejects whole-object wiring:
`resource "test-bucket" field "objectLockEnabled": parameter "config" is a typed object — a from: mapping cannot render the whole object; wire one of its declared members instead (params.config.<member>; declared members: region)`
and `cf adopt` aborts with exit code 1.

CF-274 added `isWholeObjectParam` checks to classic composition patch processing (`internal/adopt/adopt.go:2161, 2182, 2200, 2221, 2246`). But Go template adoption was never updated to check `isWholeObjectParam`.

## Contract
In `internal/adopt/adopt.go`:
In Go template parameter extraction:
- Resource annotations (around line 2426):
  When `reParamVar` matches parameter name `paramName := m[1]`:
  If `isWholeObjectParam(bp, paramName)`:
    Do NOT wire `From: "params." + paramName`.
    Record drop in `report`:
    `report.Record("resource."+res.Name+".metadata.annotations."+rawK, fmt.Sprintf("unsupported whole-object parameter wire from %q; wire individual object members instead", paramName))`
- Resource envelope (around line 2692):
  When `reParamVar` matches `paramName := m[1]`:
  If `isWholeObjectParam(bp, paramName)`:
    Do NOT wire `From: "params." + paramName`.
    Record drop in `report`:
    `report.Record("resource."+res.Name+".spec."+path, fmt.Sprintf("unsupported whole-object parameter wire from %q; wire individual object members instead", paramName))`
- Field extraction (`extractFields` around line 2809 and line 2873):
  When `reParamVar` matches `paramName := m[1]`:
  If `isWholeObjectParam(bp, paramName)`:
    Do NOT wire `From: "params." + paramName`.
    Record drop in `report`:
    `report.Record("resource."+resourceName+".spec."+path, fmt.Sprintf("unsupported whole-object parameter wire from %q; wire individual object members instead", paramName))`

When whole-object parameter wires are dropped:
1. The field/annotation/envelope remains unwired (or default).
2. The drop reason is recorded in `report.Drops`.
3. `bp.Validate()` succeeds.
4. `Adopt()` returns without error.

## Acceptance Test
In `internal/adopt/adopt_test.go`:
`TestCF298_AdoptGoTemplateObjectParamDrop`:
Feed an XRD declaring `spec.config` as a typed object with sub-properties (`region`), and a Go template Composition referencing `{{ .observed.composite.resource.spec.config }}`.
Verify:
1. `Adopt()` returns without error.
2. `bp.Validate()` succeeds.
3. `res.Fields["objectLockEnabled"]` is not wired to `params.config`.
4. `report.Drops` contains the recorded drop for the whole-object parameter wire.

## Gates
- `make lint`
- `make lint-strict`
- `make test-race`
