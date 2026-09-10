# CF-124 — Every successful parameter add or rename is followed by a false error toast and a sticky inspector banner `rename parameter: "newParam" is not declared`

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: successful action shows false error toast and sticky inspector error banner) |
| **Closes** | `CF-124 — Every successful parameter add or rename is followed by a false error toast and a sticky inspector banner rename parameter: "newParam" is not declared.` |
| **Worktree** | `.worktrees/CF-124` on branch `CF-124-param-rename-double-fire` |
| **May write** | `web-proto/js/regions/inspector.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

When a user adds or renames a parameter in the XRD inspector:
1. When editing a parameter name in `<input data-pn="...">`, pressing Enter executes:
   ```javascript
   e.preventDefault();
   e.target.blur();
   e.target.dispatchEvent(new Event("change", { bubbles: true }));
   ```
2. Calling `e.target.blur()` synchronously fires the input's native `blur` event, which in browsers triggers a `change` event if the value changed.
3. Next, `e.target.dispatchEvent(new Event("change", ...))` triggers a *second* `change` event on the same input.
4. Furthermore, in `onBoxChange` (`web-proto/js/regions/inspector.js:2225-2231`):
   ```javascript
   if (t.hasAttribute("data-pn")) {
     var oldName = t.getAttribute("data-pn"), newName = t.value.trim();
     if (!newName || newName === oldName) { render(); return; }
     op(function () { return store.renameParameter(oldName, newName); })
       .then(function (r) { if (r === null) render(); });
     return;
   }
   ```
   Notice that `t.getAttribute("data-pn")` is still `oldName` because `data-pn` is not updated synchronously or between change events.
5. The first `change` event sends `POST /api/blueprint/parameters/newParam/rename` with `{"name":"region"}`, which succeeds (HTTP 200) and renames `newParam` to `region` in the blueprint document on the server and in local store.
6. The second `change` event fires immediately with `oldName = "newParam"` and `newName = "region"`. It sends `POST /api/blueprint/parameters/newParam/rename`, but `newParam` has already been renamed! The server responds with HTTP 404:
   `{"error": "rename parameter: \"newParam\" is not declared"}`.
7. The second call reports an error:
   - A red warning toast `⚠️ rename parameter: "newParam" is not declared ×` is displayed to the user.
   - A persistent warning banner `rename parameter: "newParam" is not declared` is rendered at the top of the inspector panel.
8. The operation actually succeeded, but the user is misled into believing it failed.

## Acceptance Test

Write this test in a new spec `tests/cf124-param-rename-double-fire.spec.js`:

```javascript
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('adding and renaming a parameter with Enter does not show false error toast or inspector error banner', async ({ page }) => {
  await page.goto('/')
  // Select XRD
  await page.click('#xrd-card')

  // Click Add parameter
  await page.click('#addParamBtn')

  // Find the newly added parameter input (default "newParam")
  const input = page.locator('#insp input[data-pn="newParam"]')
  await expect(input).toBeVisible()

  // Type new name and press Enter
  await input.fill('region')
  await input.press('Enter')

  // Wait a short duration for network round-trips
  await page.waitForTimeout(600)

  // Verify toast does not show error
  const toast = page.locator('#toast')
  if (await toast.isVisible()) {
    const toastText = await toast.textContent()
    expect(toastText).not.toMatch(/is not declared/i)
  }

  // Verify inspector error banner does not show error
  const banner = page.locator('#insp .warnbar, #insp [role="alert"]')
  await expect(banner).toHaveCount(0)

  // Verify the parameter has been renamed to "region"
  const renamedInput = page.locator('#insp input[data-pn="region"]')
  await expect(renamedInput).toBeVisible()
  await expect(renamedInput).toHaveValue('region')
})
```

## Contract

- Editing a parameter name and pressing Enter must fire exactly one rename request.
- Blurring the input or pressing Enter must not trigger duplicate rename requests for an already processed value or obsolete `data-pn`.
- Updating `data-pn` on the input element upon dispatching/initiating the rename ensures any subsequent blur or change event sees `newName === oldName` and no-ops.
- Additionally, `Enter` keydown in `inspector.js` must avoid dispatching duplicate `change` events when `blur()` already triggers `change`.
- No false error toast `⚠️ rename parameter: ... is not declared` or sticky inspector banner shall appear on successful parameter add and rename.
- `make lint && make lint-strict && make test-race` must pass.
- All Playwright tests must pass.

## Verification

```sh
make lint && make lint-strict && make test-race
npx playwright test tests/cf124-param-rename-double-fire.spec.js
```

## Handover

Branch `CF-124-param-rename-double-fire`, committed, not pushed, not merged. In your final report: the failing run and the passing run of the acceptance test, both pasted; every gate you ran; every judgement call you made.
