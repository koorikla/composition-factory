// Slice 2 — the Add-provider button: SOURCES tab adds a real provider by ref,
// its kinds appear in the palette; failures surface verbatim.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('an invalid ref shows the server error verbatim and adds nothing', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs [data-tab="sources"], #rtabs button:has-text("SOURCES")')
  const input = page.locator('#src-add-ref')
  await expect(input).toBeEnabled()
  await input.fill('not-a-valid-ref!!')
  await page.click('#src-add-btn')
  await expect(page.locator('#region-palette .warnbar, #region-palette [role="alert"]').first())
    .toContainText(/invalid|not-a-valid-ref/i)
})

test('adding a provider by ref lists it in SOURCES and its kinds in KINDS', async ({ page, request }) => {
  const REF = 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0'
  const have = await (await request.get(ENGINE + '/api/providers')).json().catch(() => null)
  const cached = !!(have && have.providers?.some(p => p.ref === REF))
  await page.goto('/')
  await page.click('#rtabs [data-tab="sources"], #rtabs button:has-text("SOURCES")')
  if (!cached) {
    await page.locator('#src-add-ref').fill(REF)
    await page.click('#src-add-btn')
  }
  // pull is base-layer-only (~KBs): allow a generous margin, then rows must exist
  await expect(page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' })).toBeVisible({ timeout: 30000 })
  await page.click('#rtabs [data-tab="kinds"], #rtabs button:has-text("KINDS")')
  await expect(page.locator('#lrail').getByText('Bucket', { exact: true }).first()).toBeVisible()
})
