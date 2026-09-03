// Slice 69 — expression authoring: preview and snippets (Track 3)
// The inspector exposes snippet catalogue and live in-process preview
// when a field is in Raw (R) template mode.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('inspector in raw mode displays snippet selector, quick chips, and live preview', async ({ page }) => {
  page.on('dialog', d => d.accept())
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')

  // Switch region field to Raw (R) mode
  const rawBtn = page.locator('#insp button[data-m="r"][data-path="region"]')
  await expect(rawBtn).toBeVisible()
  await rawBtn.click()

  // Verify raw textarea, snippet selector, and quick chips are rendered
  const textarea = page.locator('#insp textarea[data-raw="region"]')
  await expect(textarea).toBeVisible()

  const snippetSelect = page.locator('#insp select[data-insert-snippet="region"]')
  await expect(snippetSelect).toBeVisible()

  const xrChip = page.locator('#insp button[data-quick-snippet="region"][data-snippet-val="{{ $xr }}"]')
  await expect(xrChip).toBeVisible()

  // Click $xr snippet chip
  await xrChip.click()
  await expect(textarea).toHaveValue('{{ $xr }}')

  // Live preview should show rendered output
  const preview = page.locator('#insp .expr-preview[data-preview-for="region"]')
  await expect(preview).toBeVisible()
  await expect(preview).toHaveClass(/ok/)
  await expect(preview.locator('.expr-preview-body')).toContainText('sample-xnotify')
})

test('typing template expressions updates live preview and shows syntax/runtime errors', async ({ page }) => {
  page.on('dialog', d => d.accept())
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')

  // Switch region field to Raw mode
  await page.locator('#insp button[data-m="r"][data-path="region"]').click()
  const textarea = page.locator('#insp textarea[data-raw="region"]')
  const preview = page.locator('#insp .expr-preview[data-preview-for="region"]')

  // Type valid interpolation
  await textarea.fill('{{ $xr }}-custom-queue')
  await expect(preview).toBeVisible()
  await expect(preview).toHaveClass(/ok/)
  await expect(preview.locator('.expr-preview-body')).toContainText('sample-xnotify-custom-queue')

  // Type invalid syntax
  await textarea.fill('{{ $xr')
  await expect(preview).toBeVisible()
  await expect(preview).toHaveClass(/err/)
  await expect(preview.locator('.expr-preview-body')).toContainText('unclosed action')

  // Type missing key
  await textarea.fill('{{ $spec.unknownKey }}')
  await expect(preview).toBeVisible()
  await expect(preview).toHaveClass(/err/)
  await expect(preview.locator('.expr-preview-body')).toContainText('map has no entry for key')
})

test('snippet dropdown inserts parameter and status expressions', async ({ page }) => {
  page.on('dialog', d => d.accept())
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')

  await page.locator('#insp button[data-m="r"][data-path="region"]').click()
  const textarea = page.locator('#insp textarea[data-raw="region"]')
  const snippetSelect = page.locator('#insp select[data-insert-snippet="region"]')
  const preview = page.locator('#insp .expr-preview[data-preview-for="region"]')

  // Select providerName parameter snippet
  await snippetSelect.selectOption('{{ $spec.providerName }}')
  await expect(textarea).toHaveValue('{{ $spec.providerName }}')
  await expect(preview).toBeVisible()
  await expect(preview).toHaveClass(/ok/)
  await expect(preview.locator('.expr-preview-body')).toContainText('sample')
})
