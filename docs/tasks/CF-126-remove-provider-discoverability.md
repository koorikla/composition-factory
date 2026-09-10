# CF-126 — "Remove provider" is reachable only ~1300 px down inside the expanded provider entry, after its full kind list, and speaks of "the cache"

> **Read `docs/task-execution-contract.md` before you start.** It governs where you
> work, which ports you may bind, what "done" means, and how you hand back. This
> brief only says *what*.

| | |
|---|---|
| **Severity** | P2 (UX scale: completable only after scrolling ~1300 px past hundreds of kinds; terminology mismatch speaks of "the cache" for blueprint sources) |
| **Closes** | `CF-126 — "Remove provider" is reachable only ~1300 px down inside the expanded provider entry, after its full kind list; the row itself offers nothing on hover, click or right-click, and the control speaks of "the cache" for what the user sees as the blueprint's sources.` |
| **Worktree** | `.worktrees/CF-126` on branch `CF-126-remove-provider-discoverability` |
| **May write** | `web-proto/js/regions/palette.js`, `tests/` |
| **Merges after** | nothing |

## Symptom

1. In the SOURCES rail, clicking an installed provider expands `.src-detail`.
2. Inside `.src-detail`, the `#src-remove-btn` button is rendered at the very bottom, *after* the entire list of kind checkboxes (`kindsHtml`). For providers with 50-150 kinds (e.g. AWS, Azure, GCP providers), this forces the user to scroll ~1300 px down past all kinds just to find the remove button.
3. The provider row (`.src-row`) itself offers no direct affordance or action on hover or click to remove the provider.
4. The button tooltip says `title="Remove this provider from the cache"`, and the confirm dialog prompts `window.confirm("Remove " + ref + " from the cache?")`. But to the user, this panel is the blueprint's "Sources" (and they are removing the provider from the blueprint's sources), not a cache management tool.

Note: The backend refusal when the provider is still referenced by resources (e.g. "still referenced by resources: instance") is correct, well placed, and must remain unchanged.

## Contract

1. In `web-proto/js/regions/palette.js`:
   - In `.src-detail`: render `#src-remove-btn` at the TOP of the expanded detail view (above or before `kindsHtml`), immediately visible upon expanding without any vertical scrolling.
   - On the provider row (`.src-row`): offer a direct remove button or action (e.g. `<button class="src-row-remove" data-remove-ref="..." title="Remove this provider from sources">&#215;</button>` or hover action) so users can remove a provider directly from the row without even expanding it, or when hovered. Make sure clicking it stops propagation (`e.stopPropagation()`) so it does not toggle the expand state.
   - Update terminology:
     - Replace "from the cache" with "from sources" in the remove button title: `title="Remove this provider from sources"` (or similar).
     - Replace "from the cache?" with "from sources?" in the confirm dialog: `Remove <ref> from sources?`.
   - Preserve selector `#src-remove-btn` and its click handler so existing tests (e.g. `tests/slice16-provider-remove.spec.js`) continue to work seamlessly.
   - If an unused provider is removed via either `#src-remove-btn` or the row remove button, it must call `api.removeProvider(ref)` and refresh the sources and kinds, exactly as before. If the provider is in use, the backend error must continue to be displayed in the warnbar alert.

## Acceptance Test

Write this test in a new spec `tests/cf126-remove-provider-discoverability.spec.js`:

```javascript
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('"Remove provider" button is immediately visible at top of expanded detail without scrolling and uses sources terminology', async ({ page, request }) => {
  // Install an unused provider with kinds (e.g. provider-aws-s3)
  const pre = await request.post(ENGINE + '/api/providers', { data: { ref: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0' } })
  expect(pre.ok()).toBeTruthy()

  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const s3row = page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' }).first()
  await expect(s3row).toBeVisible({ timeout: 10000 })

  // Expand provider details
  await s3row.click()

  // Verify remove button is visible in the viewport immediately without scrolling
  const removeBtn = page.locator('#src-remove-btn')
  await expect(removeBtn).toBeVisible()

  // Verify button title speaks of sources, not cache
  const title = await removeBtn.getAttribute('title')
  expect(title).toMatch(/sources/i)
  expect(title).not.toMatch(/cache/i)

  // Verify button position is above kind checkboxes (top of detail)
  const btnBox = await removeBtn.boundingBox()
  const firstKind = page.locator('#lrail .src-detail label input[data-pick-kind]').first()
  if (await firstKind.count() > 0) {
    const kindBox = await firstKind.boundingBox()
    expect(btnBox.y).toBeLessThan(kindBox.y)
  }

  // Verify confirmation dialog text speaks of sources, not cache
  let dialogMessage = ''
  page.on('dialog', d => {
    dialogMessage = d.message()
    d.accept()
  })

  await removeBtn.click()
  expect(dialogMessage).toMatch(/sources/i)
  expect(dialogMessage).not.toMatch(/cache/i)

  // Verify provider was removed
  await expect(page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' })).toHaveCount(0, { timeout: 10000 })
})

test('provider row offers direct remove action without requiring expansion', async ({ page, request }) => {
  const pre = await request.post(ENGINE + '/api/providers', { data: { ref: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0' } })
  expect(pre.ok()).toBeTruthy()

  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const s3row = page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' }).first()
  await expect(s3row).toBeVisible({ timeout: 10000 })

  // Provider row should have a remove button/affordance
  const rowRemove = s3row.locator('.src-row-remove, button[data-remove-ref]')
  await expect(rowRemove).toBeVisible()

  page.on('dialog', d => d.accept())
  await rowRemove.click()

  // Verify provider was removed without needing to expand full kinds list
  await expect(page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' })).toHaveCount(0, { timeout: 10000 })
})
```
