// Slice 28 — hovering a KINDS row shows a preview card: scope, field counts,
// and the required fields with their types/descriptions.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('hovering a kind shows the preview with required fields', async ({ page }) => {
  await page.goto('/')
  await page.hover('#lrail .kind[data-kind="Queue"][data-av*=".m."]')
  const prev = page.locator('#kind-preview')
  await expect(prev).toBeVisible({ timeout: 4000 })
  await expect(prev).toContainText('Queue')
  await expect(prev).toContainText('Namespaced')
  await expect(prev).toContainText('region')       // the one required field
  await expect(prev).toContainText('string')
  await expect(prev).toContainText(/18 .*fields|fields.*18/i)
})

test('the preview hides on leave and does not linger while scrolling kinds', async ({ page }) => {
  await page.goto('/')
  await page.hover('#lrail .kind[data-kind="Queue"][data-av*=".m."]')
  await expect(page.locator('#kind-preview')).toBeVisible({ timeout: 4000 })
  await page.hover('#psearch')                      // leave the rail rows
  await expect(page.locator('#kind-preview')).toBeHidden()
})
