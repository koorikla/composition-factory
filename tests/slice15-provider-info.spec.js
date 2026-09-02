// Slice 15 — provider info: a SOURCES row expands to digest, registry host
// and the kinds it serves.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('clicking a provider row expands its details and collapses again', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  const row = page.locator('#lrail .src-row', { hasText: 'provider-aws-sqs' }).first()
  await expect(row).toBeVisible()
  await row.click()
  const detail = page.locator('#lrail .src-detail', { hasText: 'sha256:' })
  await expect(detail).toBeVisible()
  await expect(detail).toContainText('ghcr.io')                 // registry host
  await expect(detail.getByText('Queue', { exact: true }).first()).toBeVisible()  // a served kind
  await row.click()
  await expect(detail).toHaveCount(0)
})
