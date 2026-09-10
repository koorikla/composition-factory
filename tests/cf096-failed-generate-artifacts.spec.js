const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('when generation fails, output drawer explorer does not announce 6 files and generated artifacts are disabled', async ({ page }) => {
  await page.goto('/')

  // Initial load: generation should be previewing files successfully
  const validChip = page.locator('#valid')
  await expect(validChip).toBeVisible()
  await expect(validChip).toContainText('preview ·')

  // The explorer should initially show available files
  const treeCount = page.locator('#tree-files-count')
  await expect(treeCount).toBeVisible()
  await expect(treeCount).not.toHaveText('1 file')

  // Intercept POST /api/generate to simulate a generation failure (e.g. status 400 with error)
  await page.route('**/api/generate', async route => {
    await route.fulfill({
      status: 400,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'failed to generate: invalid field path' })
    })
  })

  // Trigger generation via store.generate(false)
  await page.evaluate(() => {
    window.store.generate(false)
  })

  // Wait for generate error (header chip shows 'error')
  await expect(validChip).toHaveText('error', { timeout: 10000 })

  // Check that output drawer tree explorer does not announce 6 files when generation fails
  await expect(treeCount).toHaveText('1 file')

  // Check that generated artifact tabs or tree items for non-existent files are disabled or greyed out
  const compTab = page.locator('#tabs button[data-t="comp"]')
  await expect(compTab).toBeDisabled()
  await expect(compTab).toHaveAttribute('aria-disabled', 'true')

  const xrdTab = page.locator('#tabs button[data-t="xrd"]')
  await expect(xrdTab).toBeDisabled()
  await expect(xrdTab).toHaveAttribute('aria-disabled', 'true')

  const fnsTab = page.locator('#tabs button[data-t="fns"]')
  await expect(fnsTab).toBeDisabled()
  await expect(fnsTab).toHaveAttribute('aria-disabled', 'true')

  // Blueprint tab should remain enabled
  const bpTab = page.locator('#tabs button[data-t="bp"]')
  await expect(bpTab).not.toBeDisabled()

  // Tree items for generated files should be marked disabled / aria-disabled
  const compTreeItem = page.locator('#tree-root .tree-item[data-t="comp"]')
  await expect(compTreeItem).toHaveAttribute('aria-disabled', 'true')
  await expect(compTreeItem).toHaveClass(/disabled/)

  const xrdTreeItem = page.locator('#tree-root .tree-item[data-t="xrd"]')
  await expect(xrdTreeItem).toHaveAttribute('aria-disabled', 'true')
  await expect(xrdTreeItem).toHaveClass(/disabled/)

  // Blueprint tab can be clicked and selected
  await bpTab.click()
  await expect(bpTab).toHaveAttribute('aria-pressed', 'true')
})

