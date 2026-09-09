// tests/cf088-on-demand-source.spec.js
// CF-088 — Uncached declared sources load on demand; canvas banner must not prescribe CLI commands.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('canvas banner never directs the user to run cf provider add in shell', async ({ page }) => {
  await page.goto('/')
  // Verify top chip is valid, not error
  const validChip = page.locator('#valid')
  await expect(validChip).toBeVisible()
  const chipText = await validChip.textContent()
  expect(chipText).not.toContain('error')

  // Check no toast or banner prescribes cf provider add
  const bodyText = await page.locator('body').innerText()
  expect(bodyText).not.toContain('run: cf provider add')
})
