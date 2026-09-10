# CF-123 — A parameter dot can be dropped on a resource card's *output* row, rejecting the write and sticking the header chip on `error`

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: dropping a wire on an output row triggers an invalid write, developer error message, and sticky error chip) |
| **Closes** | `CF-123 — A parameter dot can be dropped on a resource card's output row; the server answers 400 with field "status.atProvider.…" is not in Instance spec.forProvider (an unknown field is silently pruned …) and the header chip sticks on error until the next successful edit.` |
| **Worktree** | `.worktrees/CF-123` on branch `CF-123-reject-drop-on-output-row` |
| **May write** | `web-proto/js/regions/canvas.js`, `web-proto/js/regions/output.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

1. When dragging a parameter dot from the XRD card (or any outgoing wire source) on the canvas, output rows on resource cards (e.g. status fields under the "outputs" group such as `status.atProvider.allowMajorVersionUpgrade`) highlight as valid drop targets (`wire-target-hover`) and accept the drop.
2. In `web-proto/js/regions/canvas.js:1609-1620`:
   ```javascript
   if (targetPort) {
     const tOwner = targetPort.getAttribute("data-owner");
     const tPath = targetPort.getAttribute("data-path");
     if (tOwner && tOwner !== owner) {
       if (dir === "out" && tOwner !== XR_ID) {
         applyWire(owner, path, tOwner, tPath);
       ...
   ```
   When `tPath` is an output row (e.g. `status.atProvider.endpoint`), `applyWire` writes `r.fields["status.atProvider.endpoint"] = { from: "params.region" }` and calls `store.replaceDoc`.
3. The API server returns HTTP 400 Bad Request because status fields are not part of `spec.forProvider`:
   `field "status.atProvider.endpoint" is not in Instance spec.forProvider (an unknown field is silently pruned by the API server on apply, so it must be caught here)`.
4. The write is rejected, and `store.replaceDoc` emits an `error` event.
5. In `web-proto/js/regions/output.js:1066-1072`:
   ```javascript
   store.subscribe("error", function (err) {
     if (err) {
       isValidating = false;
       validatedRevision = -1;
       chipErr(err.message || String(err));
     }
   });
   ```
   The header chip `#valid` turns red and displays `error`. The document on disk and in memory was completely untouched (the write was rejected), but the header chip remains stuck on `● error` until an unrelated successful edit occurs.

## Contract

1. Output rows on resource cards must NEVER accept incoming wires:
   - Output rows are outputs (`dir: "out"`), representing status fields that other resources may depend on. An incoming wire (from a parameter or another resource's status) cannot be wired into an output field.
   - When dragging an outgoing wire (`dir === "out"`), output rows (`.port[data-path^="status."]`) must not highlight as drop targets on hover (no `wire-target-hover` class) and releasing over them must not call `applyWire`.
   - Dropping onto a resource card generally outside any valid input port should still open the field picker (which lists only valid input fields, envelope fields, and annotations—never status outputs).
2. A rejected write (failed `replaceDoc`, `updateParameter`, etc.) where `state.doc` was NOT modified must not leave the header status chip permanently stuck on `error`. The header chip indicates the state of the active document's manifests and validation. A failed mutation attempt that left the document unchanged should surface a toast or transient error, but must not corrupt or stick the header chip state for an unchanged, valid document.
3. `make lint && make lint-strict && make test-race` must pass cleanly.
4. All existing tests in Playwright suite must pass.

## Acceptance Test

Write this test in a new spec `tests/cf123-output-row-drop.spec.js`:

```javascript
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('dragging a parameter onto a resource output row does not wire status or stick header chip on error', async ({ page }) => {
  await page.goto('/')

  // Verify header chip initially shows preview/written, not error
  const validChip = page.locator('#valid')
  await expect(validChip).toBeVisible()
  expect(await validChip.textContent()).not.toMatch(/error/i)

  // Find a parameter port on the XRD card (e.g. region)
  const paramPort = page.locator('.node[data-id="xrd"] .port[data-path="region"] .d.out').first()
  await expect(paramPort).toBeVisible()

  // Find a resource card (e.g. instance) with an output port
  const outputPort = page.locator('.node:not([data-id="xrd"]) .port[data-path^="status."]').first()
  await expect(outputPort).toBeVisible()

  // Drag the parameter dot and drop directly on the output port
  await paramPort.hover()
  await page.mouse.down()
  await outputPort.hover()
  await page.mouse.up()

  // Wait briefly for any potential network requests or store mutations
  await page.waitForTimeout(500)

  // Verify the header chip did not stick on error
  expect(await validChip.textContent()).not.toMatch(/error/i)

  // Verify no invalid wire was created in the blueprint
  const d = await page.evaluate(() => window.store ? window.store.state.doc : null)
  if (d && d.spec && d.spec.resources) {
    for (const r of d.spec.resources) {
      if (r.fields) {
        for (const k of Object.keys(r.fields)) {
          expect(k).not.toMatch(/^status\./)
        }
      }
    }
  }
})
```

## Verification

```sh
make lint && make lint-strict && make test-race
npx playwright test tests/cf123-output-row-drop.spec.js
```

## Handover

Branch `CF-123-reject-drop-on-output-row`, committed, not pushed, not merged. In your final report: the failing run and the passing run of the acceptance test, both pasted; every gate you ran; every judgement call you made.
