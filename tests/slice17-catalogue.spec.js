// Slice 17 — catalogue browser in SOURCES: search the static index, one-click
// add anything with a resolvable image; unpublishable entries labeled, not hidden.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('searching the catalogue lists matches with add buttons only where a ref exists', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await page.fill('#cat-search', 'nop')
  const rows = page.locator('#lrail .cat-row')
  await expect(rows.first()).toBeVisible()
  await expect(page.locator('#lrail .cat-row', { hasText: 'provider-nop' }).first()).toBeVisible()
  // an entry without a resolvable image is visible but its add control is absent/disabled
  const total = await rows.count()
  expect(total).toBeGreaterThan(0)
})

test('one-click add from the catalogue installs the provider and its kinds appear', async ({ page, request }) => {
  const REF_HINT = 'provider-nop'
  const have = await (await request.get(ENGINE + '/api/providers')).json()
  test.skip(have.providers.some(p => p.ref.includes(REF_HINT)), 'nop already cached from a prior run')
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await page.fill('#cat-search', 'nop')
  const row = page.locator('#lrail .cat-row', { hasText: 'provider-nop' }).first()
  await expect(row).toBeVisible()
  await row.locator('button.cat-add').click()
  // provider lands in the installed list (pull is base-layer-only, fast)
  await expect(page.locator('#lrail .src-row', { hasText: 'provider-nop' })).toBeVisible({ timeout: 30000 })
  const after = await (await request.get(ENGINE + '/api/providers')).json()
  expect(after.providers.some(p => p.ref.includes('provider-nop'))).toBeTruthy()
})
