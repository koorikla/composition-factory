# CF-125 — `providerName` looks editable (name input, enabled `×`) but rename and delete are reverted with terminal instructions shown in browser

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: editable affordance on a required locked parameter, reverted with CLI terminal instruction in the browser) |
| **Closes** | `CF-125 — providerName looks editable (name input, enabled ×) but rename and delete are reverted with run cf serve without --blueprint to scaffold one, a terminal instruction shown in the browser.` |
| **Worktree** | `.worktrees/CF-125` on branch `CF-125-providerName-locked-ui` |
| **May write** | `web-proto/js/regions/inspector.js`, `internal/blueprint/validate_params.go`, `internal/blueprint/load_test.go`, `tests/` |
| **Merges after** | nothing |

## Symptom

1. In the XRD Inspector Parameters list, `providerName` is rendered using the exact same editable text input (`<input class="tin bold" data-pn="providerName">`) and enabled delete button (`<button class="del" data-pd="providerName">&#215;</button>`) as any user-defined parameter.
2. If a canvas user tries to rename `providerName` (e.g. to `region` or `dbProvider`), or clicks the `×` to delete it, the API server rejects the request with HTTP 400:
   `spec.xrd.parameters.providerName is required for a Namespaced XRD: run cf init (or cf serve without --blueprint) to scaffold one, or add: providerName: {type: string, required: true}`
3. This message tells a graphical browser user running the canvas to execute a CLI command in their terminal (`run cf init (or cf serve without --blueprint)...`), and the operation reverts without visual cue as to why the UI appeared to allow it in the first place.
4. When an XRD has Namespaced scope and managed resources (the standard blueprint setup), `providerName` is a required Crossplane infrastructure parameter that configures `providerConfigRef` for all composed resources. It is structural and cannot be deleted or renamed while managed resources are present.

## Contract

1. In `web-proto/js/regions/inspector.js`:
   - When parameter `n === "providerName"` and the XRD is Namespaced with managed resources (or generally for `providerName` when locked):
     - The parameter name input must be marked `readonly` (or `disabled`), styled appropriately (e.g. `title="providerName is required for managed resources in Namespaced XRD"`), or clearly indicate it is locked.
     - The delete button (`×`) for `providerName` must either be disabled (`disabled`, `title="providerName is required for managed resources in Namespaced XRD"`, `style="cursor:not-allowed;opacity:0.5"`) or clicking it must show a clear canvas-focused message explaining why it cannot be deleted without sending a failing request.
     - Type and `req` checkbox should similarly reflect that `providerName: {type: string, required: true}` is required for Namespaced XRD.
2. In `internal/blueprint/validate_params.go`:
   - Clarify the error message so it does not prescribe CLI terminal commands when invoked from web/MCP/canvas, e.g.:
     `spec.xrd.parameters.providerName is required for a Namespaced XRD with managed resources: add providerName: {type: string, required: true}`
   - Any tests in `internal/blueprint/load_test.go` expecting the previous message string should be updated to match.
3. No browser user should ever be instructed to "run cf serve without --blueprint" when attempting an action in the browser.

## Acceptance Test

Write this test in a new spec `tests/cf125-providername-locked.spec.js`:

```javascript
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('providerName parameter row is locked against rename and deletion with clear visual cue', async ({ page }) => {
  await page.goto('/')
  // Select XRD
  await page.click('.node[data-id="xrd"] .node-h')

  // Find providerName name input
  const nameInput = page.locator('#insp input[data-pn="providerName"]')
  await expect(nameInput).toBeVisible()

  // Verify nameInput is readonly or disabled
  const isReadOnly = await nameInput.getAttribute('readonly')
  const isDisabled = await nameInput.getAttribute('disabled')
  expect(isReadOnly !== null || isDisabled !== null).toBe(true)

  // Find providerName delete button
  const delBtn = page.locator('#insp button[data-pd="providerName"]')
  await expect(delBtn).toBeVisible()

  // Verify delBtn is disabled
  const isDelDisabled = await delBtn.getAttribute('disabled')
  expect(isDelDisabled !== null).toBe(true)

  // Verify body never mentions terminal command instruction "run cf serve without --blueprint"
  const bodyText = await page.locator('body').innerText()
  expect(bodyText).not.toMatch(/run cf serve without --blueprint/i)
})
```

## Verification

```sh
make lint && make lint-strict && make test-race
npx playwright test tests/cf125-providername-locked.spec.js
```

## Handover

Branch `CF-125-providerName-locked-ui`, committed, not pushed, not merged. In your final report: the failing run and the passing run of the acceptance test, both pasted; every gate you ran; every judgement call you made.
