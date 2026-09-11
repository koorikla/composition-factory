const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('header and artifacts panel reconcile counts with labelled previews', async ({ page }) => {
  await page.goto('/')

  const validChip = page.locator('#valid')
  const treeCount = page.locator('#tree-files-count')

  // Initial load: preview mode with 4 manifests and 3 previews (blueprint, package, rbac)
  await expect(validChip).toBeVisible()
  await expect(validChip).toHaveText('preview · 4 files · 3 previews')
  await expect(treeCount).toBeVisible()
  await expect(treeCount).toHaveText('4 files · 3 previews')

  // Click Generate to write to disk
  page.on('dialog', dialog => dialog.accept())
  await page.click('#generateBtn')

  // Written mode: header and artifacts badge show written and preview counts
  await expect(validChip).toHaveText('written · 4 files · 3 previews', { timeout: 10000 })
  await expect(treeCount).toHaveText('4 written · 3 previews')
})

test('when generation fails, artifacts badge falls back to 1 file and chip shows error', async ({ page }) => {
  await page.goto('/')

  const validChip = page.locator('#valid')
  const treeCount = page.locator('#tree-files-count')

  await expect(treeCount).toHaveText('4 files · 3 previews')

  // Intercept POST /api/generate to simulate failure
  await page.route('**/api/generate', async route => {
    await route.fulfill({
      status: 400,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'failed to generate: invalid field path' })
    })
  })

  // Trigger generation
  await page.evaluate(() => {
    window.store.generate(false)
  })

  await expect(validChip).toHaveText('error', { timeout: 10000 })
  await expect(treeCount).toHaveText('1 file')
})
