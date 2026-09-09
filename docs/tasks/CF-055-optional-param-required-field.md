# CF-055 — An optional parameter wired into a required provider field renders invalid, and the error blames the field

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P1 |
| **Closes** | `CF-055 — An optional parameter wired into a required provider field renders invalid, and the error blames the field.` |
| **Worktree** | `.worktrees/CF-055` on branch `CF-055-optional-param-required-field`, branched from `main` |
| **May write** | `web-proto/js/regions/canvas.js`, `web-proto/js/regions/inspector.js`, `web-proto/css/proto.css`, `tests/` |
| **Merges after** | nothing |

## Symptom

When an optional XR parameter (where `required` is false / unticked) is wired to a required resource field (e.g. `$region` wired to Bucket's `spec.forProvider.region`), the emitter wraps the field in an optional check:
`{{- if or (hasKey $spec "region") }}` (go-templating) or `if _spec.get("region") is not None` (Python).
During `Validate` / `crossplane composition render` against a sample XR that does not supply the optional parameter, the field is omitted from the rendered manifest.
Crossplane validation then fails with:
`line 47: resource "bucket" (Bucket): missing required field "spec.forProvider.region"`

Meanwhile, on the Canvas:
- The wire is drawn normally.
- In the Inspector, the field appears wired (`+ params.region`).
- The user is blamed with a message pointing to a line number in generated YAML, while the real issue is that the source parameter is optional (`req` is unticked).

## Requirements

1. **In the Drag-to-Card Field Picker (`canvas.js`)**:
   - When wiring an optional parameter to a required field (`item.required === true && srcOwner === XR_ID && !param.required`):
     - Display a warning badge (e.g. `<span class="wire-picker-opt-warning" title="Optional parameter bound to required field">optional → req</span>`).
     - When selected, offer/prompt to mark the parameter as required (e.g. `confirm("Parameter '$" + srcPath + "' is optional, but '" + item.path + "' is required.\n\nMark parameter as required to guarantee presence in render?")`), similar to `item.typeMismatch`.
2. **In the Inspector (`inspector.js`)**:
   - When an optional parameter is bound into a required field (`f.required`):
     - Display an inline warning badge or hint under the bound wire (e.g. `<span class="wire-warn" style="color:var(--warn);font-size:10px" title="Optional parameter wired to required field: render will omit if parameter is missing">⚠️ optional param into required field</span>`).
3. **On the Canvas Card (`canvas.js`)**:
   - On the target resource card's port row, if the bound parameter is optional but the field is required, show a subtle warning marker / indicator (e.g. warning color on dot or port title warning).
4. **Automated E2E Test**:
   - Add a Playwright test in `tests/` (e.g. in `tests/slice60-drag-to-card-picker.spec.js` or `tests/slice70-canvas-ux-visibility.spec.js`) verifying the warning badge and prompt behavior.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice60-drag-to-card-picker.spec.js
```
