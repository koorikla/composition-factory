# CF-117 — When Validate fails on a required field fed by an optional parameter, the error still says `missing required field ...` and never names the parameter or the promote action

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-117 — When Validate fails on a required field fed by an optional parameter, the error still says missing required field "spec.forProvider.region" and never names the parameter or the promote action.` |
| **Worktree** | `.worktrees/CF-117` on branch `CF-117-missing-required-optional-param-hint` |
| **May write** | `internal/emit/`, `internal/api/`, `cmd/cf/`, `web-proto/js/regions/output.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

When an XRD parameter is optional (`required: false` and no default) and wired into a required provider field (e.g. `params.region` wired to `Queue`'s required field `spec.forProvider.region`), `SampleXR` synthesizes an XR containing only required parameters or parameters with defaults. Therefore, the optional parameter is omitted from the sample XR.

The composition template surrounds the field assignment with an optional check (e.g. `{{- if (hasKey $spec "region") }}`), so the field is omitted from the rendered composed resource.

During validation (`POST /api/render`, `cf gen --validate`, or Validate in the canvas), `emit.ValidateRendered` validates the rendered resources against the CRD OpenAPI schemas. When checking required properties of `spec.forProvider` (or spec envelope/native fields), it detects that the required field is missing and currently emits:
```
line 51: resource "dead-letter" (Queue): missing required field "spec.forProvider.region" in Queue spec.forProvider
```

The error message blames only the field and says `missing required field "spec.forProvider.region"`. It never mentions:
1. That the resource field is actually wired to `params.region` in the blueprint.
2. That `params.region` is optional in the XRD.
3. The actionable fix: mark the parameter as required in the XRD (promote action) or provide a default value.

Residue of CF-055: CF-055 added the warning badge in the picker and inspector, but the render validation error message was unchanged (documented in `docs/comp-runs/2026-09-09-fix-verification.md` §CF-055).

## Contract

1. **Error Diagnostics Enhancement**:
   When `ValidateRendered` (or validation called with a blueprint / options) reports a missing required field on a composed resource, if the blueprint defines a wiring for that resource field from an optional XRD parameter (`from: params.<name>` or `params.<name>.<member>` where the parameter or member is optional / `required: false` and has no default):
   - The error message must name the parameter (e.g. `params.region`).
   - The error message must explain that it is optional.
   - The error message must point at the promote action (e.g. `(fed by optional parameter params.region; mark parameter required in the XRD or provide a default)`).
   Example format:
   `line 51: resource "dead-letter" (Queue): missing required field "spec.forProvider.region" in Queue spec.forProvider (fed by optional parameter params.region; mark parameter required in the XRD or provide a default)`

2. **Integration Across Emission Seams**:
   - `emit.ValidateRendered` / `emit.ValidateRenderedWithBlueprint`: Provide or extend the validation function so both the HTTP API (`/api/render`), the CLI (`cf gen --validate`), and unit tests benefit from the enhanced error diagnostics identically.
   - Ensure backwards compatibility: calling `ValidateRendered(stream, crds)` without blueprint continues to work and produces the standard missing required field message.

3. **In the Web Canvas UI (`output.js` / Error Formatting)**:
   - When displaying render validation errors in the drawer and top banner, if the message mentions `fed by optional parameter params.<name>` or `mark parameter required`, ensure the text is clearly displayed and not stripped or malformed by `formatErrorMessage`.

4. **Verification Gates**:
   - `make lint` must pass.
   - `make lint-strict` must pass.
   - `make test-race` must pass.
   - `make test-e2e` must pass.

## Acceptance Test

Write unit and integration tests:
1. In `internal/emit/render_validate_test.go`:
   Add a test verifying that when a blueprint has an optional parameter wired to a required field, render validation includes the parameter name, notes that it is optional, and suggests marking it required.
2. In Playwright e2e (e.g. `tests/cf117-optional-param-validate-error.spec.js`):
   Load a blueprint with `parameters.region.required = false` wired to `region` on a Queue. Click Validate (`#validateBtn`). Verify that the error message displayed in `#render-warn` and/or `#render-warn-text` contains `params.region`, names it optional, and points to marking it required.
