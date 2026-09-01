// Slice 16 — provider remove: from the expanded SOURCES detail, with the
// server refusing (409, referencers named) while the blueprint still uses it.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('removing a provider the blueprint uses is refused with referencers named', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await page.locator('#lrail .src-row', { hasText: 'provider-aws-sqs' }).first().click()
  page.on('dialog', d => d.accept())
  await page.click('#src-remove-btn')
  await expect(page.locator('#region-palette [role="alert"]').first())
    .toContainText(/work-queue|dead-letter|in use/i)   // server names what still uses it
  const kinds = await page.locator('#lrail').textContent()
  // still listed — nothing was removed
  await page.click('#rtabs button[data-r="src"]')
  await expect(page.locator('#lrail .src-row', { hasText: 'provider-aws-sqs' }).first()).toBeVisible()
})

test('removing an unused provider deletes it and its kinds leave the palette', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  const s3row = page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' }).first()
  test.skip(!(await s3row.count()), 's3 provider not cached')
  await s3row.click()
  page.on('dialog', d => d.accept())
  await page.click('#src-remove-btn')
  await expect(page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' })).toHaveCount(0, { timeout: 10000 })
  await page.click('#rtabs button[data-r="kinds"]')
  await expect(page.locator('#lrail .kind[data-kind="Bucket"]')).toHaveCount(0)
  // restore for other tests: add it back through the real endpoint
  const re = await request.post(ENGINE + '/api/providers', { data: { ref: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0' } })
  expect(re.ok()).toBeTruthy()
})
