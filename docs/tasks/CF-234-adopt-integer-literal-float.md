# CF-234 — adopt re-emits integer literals in float notation (1209600 → 1.2096e+06) in the regenerated Composition

## Severity & Scope
- **Severity**: P1
- **Scale**: engine
- **Touch set**: `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go`, `docs/tasks/CF-234-adopt-integer-literal-float.md`

## Background & Problem
`internal/adopt/adopt.go` decodes the template body with `sigs.k8s.io/yaml`, which goes through JSON and yields `float64` for all numbers.
In `internal/adopt/adopt.go:2394` (`extractFields` default scalar stringification), numbers are stringified with `fmt.Sprint(val)`.
For whole numbers with 6 or more digits such as `1209600` (e.g. `messageRetentionSeconds: 1209600`), `fmt.Sprint(float64(1209600))` yields `"1.2096e+06"`.
The blueprint stores `value: "1.2096e+06"` and `cf gen` writes it back into the Composition unchanged.
This violates the round-trip rule (AGENTS.md §1): `cf gen` → `cf adopt` → `cf gen` mutates `messageRetentionSeconds: 1209600` into `messageRetentionSeconds: 1.2096e+06`.
Notice that integer-preserving formatting like `strconv.FormatFloat(v, 'f', -1, 64)` is already used elsewhere in `adopt.go` (e.g. lines 679-684 and 981-985), or checking whether a `float64` has no fractional component and fits in an `int64`.

## Expected Behavior & Contract
Whole-number literals must round-trip as exact integer strings without exponential notation (`1209600` remains `"1209600"`), preserving the original value across `cf adopt` and subsequent `cf gen`.

## Acceptance Test
- `internal/adopt/adopt_test.go:TestAdoptIntegerLiteralPreservesWholeNumberFormat` verifying `1209600` round-trips as `"1209600"` in blueprint field values.
