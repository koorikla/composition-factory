# CF-236 — composition-only adopt replaces the pinned function-auto-ready package with a hardcoded default and never reports it

## Severity & Scope
- **Severity**: P1
- **Scale**: engine
- **Touch set**: `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go`, `docs/tasks/CF-236-adopt-function-pkg-loss-report.md`

## Background & Problem
When a Composition is adopted without its `functions.yaml` (e.g. from `kubectl get composition -o yaml`), `internal/adopt/adopt.go:1414` assigns default `pkg = "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"` to any `function-auto-ready` step whose package is unknown.
Then lines `:1513` / `:1526` treat the step as the built-in default and drop it from `spec.pipeline`.
The loss report never mentions that the function package could not be recovered or that a default was assumed.
Subsequent `cf gen` produces a `functions.yaml` with the default package version rather than preserving the pinned version (or warning the author of the downgrade).

## Expected Behavior & Contract
When the package of a referenced function in a composition cannot be read from the input (e.g. adopting a composition file directly without `functions.yaml`), the loss report must explicitly record the step and the assumed default package (or note that function package versions could not be recovered without `functions.yaml`), so that the author is informed of the assumption rather than silently downgrading a cluster's Function.

## Acceptance Test
- Unit test in `internal/adopt/adopt_test.go` asserting that adopting a composition without `functions.yaml` includes a note in `LossReport` naming the unrecovered function package and assumed default.
