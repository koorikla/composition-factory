# CF-387 — Canvas fails to render wires from object parameter members to composed resource fields

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-387 — Canvas fails to render wires from object parameter members to composed resource fields` (#278) |
| **Worktree** | `.worktrees/CF-387` on branch `CF-387-canvas-object-member-wires`, branched from `main` |
| **May write** | `web-proto/js/regions/canvas.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

When a composed resource field is wired from a member of an object parameter (e.g. `from: "params.dbConfig.host"`):
- The wire is recognized and valid in backend validation and in the resource inspector.
- However, on the visual canvas, no wire spline or hit target is rendered (`#wires path.wire-path` is completely absent).
- Users cannot visually confirm the wiring, cannot hover/inspect the connection on canvas, and cannot click to select or delete the wire from the canvas.

## Mechanism

1. In `web-proto/js/regions/canvas.js:748`, `drawWires()` resolves parameter wire source position using:
   `a = portPos(XR_ID, w.param, cwRect);`
   For nested object parameter member wires, `w.param` is e.g. `"dbConfig.host"`.
2. In `canvas.js:676-683`, `portPos(owner, path, cwRect)` executes:
   `canvasEl.querySelector('.port[data-owner="' + CSS.escape(owner) + '"][data-path="' + CSS.escape(path) + '"] .d')`
   looking for `[data-owner="xrd"][data-path="dbConfig.host"]`.
3. However, `xrCardHTML` in `canvas.js:153-165` only generates port rows for top-level parameters (`data-path="dbConfig"`). It does not generate ports for nested members, nor does `portPos` provide a fallback to `w.param.split('.')[0]`.
4. As a result, `portPos` returns `null`, and `if (!a || !b) return;` at `canvas.js:755` silently skips drawing the wire spline.
5. In addition, `fans[w.param]` at `canvas.js:729` keys on `w.param` rather than the root parameter prefix, so multiple wires from members of the same object parameter do not register as sharing the port.

## Acceptance Test

Verbatim in `tests/slice94-object-member-wire.spec.js`:

```javascript
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers')
guardPageErrors()

test.describe('Object parameter member wire rendering (CF-387, #278)', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request)
  })

  test('canvas renders wire connecting object parameter member to resource field', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    doc.spec.xrd.parameters = doc.spec.xrd.parameters || {}
    doc.spec.xrd.parameters.dbConfig = {
      type: 'object',
      required: true,
      properties: {
        host: { type: 'string', required: true }
      }
    }
    const res = doc.spec.resources[0]
    res.fields = res.fields || {}
    res.fields['region'] = { from: 'params.dbConfig.host', value: '', raw: '' }

    const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
    if (!put.ok()) {
      throw new Error('PUT blueprint failed: ' + (await put.text()))
    }

    await page.goto('/')
    await canvasSettled(page)

    // The wire must be drawn on canvas connecting the XRD card to the resource
    const expectedTitle = `$dbConfig.host \u2192 ${res.name}.region`
    const wirePath = page.locator(`svg.wires path.wire-path[title*="${expectedTitle}"]`)
    await expect(wirePath).toBeVisible({ timeout: 2000 })

    const wireHit = page.locator(`svg.wires path.wire-hit[title*="${expectedTitle}"]`)
    await expect(wireHit).toHaveCount(1)
  })
})
```

Fails today with:
```
Error: expect(locator).toBeVisible() failed
Locator: locator('svg.wires path.wire-path[title*="$dbConfig.host → work-queue.region"]')
Expected: visible
Timeout: 2000ms
Error: element(s) not found
```

## Contract

1. In `web-proto/js/regions/canvas.js`:
   - `portPos(XR_ID, w.param, cwRect)` or `drawWires()` must resolve object parameter member wires (`dbConfig.host`) to the parent parameter's port on the XRD card (`dbConfig`).
   - Wire fan-out tracking for parameter ports must treat wires from members of the same object parameter as sharing the port (e.g. keying by the root parameter prefix `w.param.split('.')[0]`).
2. The SVG wire path and hit target must render on canvas with the correct title (`$dbConfig.host → <resource>.<field>`).
3. Wires originating from object parameter members must be selectable and deletable from the canvas via existing wire interaction handlers.

## Verification

```sh
npm run lint:js
make lint
make test
npx playwright test tests/slice94-object-member-wire.spec.js
```
