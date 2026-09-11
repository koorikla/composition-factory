# CF-251 — XRD-less import drops a parameter referenced only inside a named template, so Validate fails

## Severity & Scope
- **Severity**: P1
- **Scale**: engine
- **Touch set**: `internal/adopt/adopt.go`, `internal/adopt/adopt_test.go`

## Background & Problem
Importing a Composition without its XRD (e.g. from the Full-Stack starter) drops parameters that are only referenced inside named template definitions (`spec.templates`), such as `oidcProviderArn` which is dereferenced in the `trust-policy` template.
Because the parameter is dropped while the template definition is preserved in `spec.templates`, the resulting adopted blueprint fails `Validate`:
`validation error — executing "trust-policy" at <.spec.oidcProviderArn>: map has no entry for key "oidcProviderArn"`.

### Mechanism
In `internal/adopt/adopt.go:1536-1545` (`parseGoTemplateBody`), `{{ define }}` blocks are extracted into `bp.Spec.Templates`, and then stripped to produce `cleanTmpl`.
Only `cleanTmpl` is scanned with `reParamVar` and `reEnvVar`.
Consequently, parameters referenced exclusively inside named template definitions are never discovered or declared in `bp.Spec.XRD.Parameters`.
The loss report also fails to name the missing parameter.

## Expected Behavior & Contract
When adopting a Composition:
- Parameters and environment variables referenced within named template definitions (`spec.templates` / `{{ define ... }}` blocks) must be discovered and declared in the adopted blueprint (e.g. via `ensureParamDeclared(bp, pName)` and `ensureEnvDeclared(bp, key, "string")`).
- The resulting adopted blueprint must pass `Validate()` and render/generate successfully.

## Acceptance Test
- An automated test in `internal/adopt/adopt_test.go` adopting a Composition with a named template referencing a parameter exclusively in the template body (such as `.spec.oidcProviderArn` in a `{{ define "trust-policy" }}` block), verifying that the parameter is declared in `bp.Spec.XRD.Parameters`, the blueprint passes `Validate()`, and `emit.Generate()` succeeds.
