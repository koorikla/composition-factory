# CF-377 — fanOut ignores parameters referenced in spec.templates failing parameter deletion with HTTP 409

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 |
| **Closes** | `CF-377 — fanOut ignores parameters referenced in spec.templates failing parameter deletion with HTTP 409` (#268) |
| **Worktree** | `.worktrees/CF-377` on branch `CF-377-template-param-fanout`, branched from `main` |
| **May write** | `web-proto/js/wires.js`, `web-proto/js/regions/inspector/xrd.js`, `tests/slice95-template-param-fanout.spec.js` |
| **Merges after** | nothing |

## Defect Summary

`fanOutMap` in `web-proto/js/wires.js:259-305` aggregates parameter wires across `listWires(doc)` and `resources` (`r.when`, `r.forEach`), but completely omits `doc.spec.templates`.

Consequently, `fanOut(doc, name)` returns `0` when a parameter is referenced inside template definitions (e.g. `doc.spec.templates["cf.policy"]: "{{ .spec.policy }}"`).

When a user attempts to delete the parameter in the palette:
1. `fanOut` returns 0, so the UI skips the unwire confirmation prompt and sends `DELETE /api/blueprint/parameters/<name>`.
2. The server's `b.DeleteParameter(name)` in `internal/blueprint/edit.go:629-633` checks:
   ```go
   for tName, body := range b.Spec.Templates {
       if rawReferencesParam(body, name) {
           return fmt.Errorf("delete parameter %q: still referenced by template %q", name, tName)
       }
   }
   ```
   and rejects the deletion with `HTTP 409 Conflict`.
3. In addition, `cleanParamRefs` (`web-proto/js/regions/inspector/xrd.js:77-140`) omits `draft.spec.templates`, so unwiring never removes template references or cleans templates.

## Acceptance Test

In `tests/slice95-template-param-fanout.spec.js`:

```javascript
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers')
guardPageErrors()

test.describe('CF-377 — fanOut template parameter reference', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request)
  })

  test('parameter referenced in spec.templates reflects fan-out count >= 1 and prompts unwire on delete', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    doc.spec.xrd = doc.spec.xrd || {}
    doc.spec.xrd.parameters = doc.spec.xrd.parameters || {}
    doc.spec.xrd.parameters.policy = {
      type: 'string',
      default: 'standard'
    }
    doc.spec.templates = {
      'cf.policy': '{{ .spec.policy }}'
    }

    const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
    if (!put.ok()) {
      throw new Error('PUT blueprint failed: ' + (await put.text()))
    }

    await page.goto('/')
    await canvasSettled(page)

    // Check shared rail / palette shows 1 bound for policy
    const card = page.locator('.card:has([data-param-del="policy"])')
    await expect(card.locator('.bind')).toHaveText('1 bound')

    // Click delete on policy parameter
    await page.click('[data-param-del="policy"]')

    // Confirm dialog / prompt should appear because fanOut > 0
    await expect(page.locator('.confirm-dialog, .prompt-unwire, [data-testid="unwire-confirm"]')).toBeVisible({ timeout: 2000 })
  })
})
```

## Contract

1. In `web-proto/js/wires.js`:
   In `fanOutMap(doc)`:
   Inspect `doc.spec.templates`. If `doc.spec.templates` is an object, for every template body string, check if it references declared parameters (or any parameter names) using raw reference matching (matching `isRawParamRef` or regex `(?:\$spec|\.spec|\$params|\.params|params)\.<param>` as in `isRawParamRef`). Increment the fanout count for that parameter.
2. In `web-proto/js/regions/inspector/xrd.js`:
   In `cleanParamRefs(draft, pn)`:
   If `draft.spec.templates` is defined, remove or clean templates referencing `pn` (or if deleting templates containing `pn`: `delete draft.spec.templates[tName]` when `isRawParamRef(draft.spec.templates[tName], pn)`).

## Verification

```sh
npm run lint:js
npm run typecheck
npx playwright test tests/slice95-template-param-fanout.spec.js
```
