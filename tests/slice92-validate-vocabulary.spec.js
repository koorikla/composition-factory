// Slice 92 — Harmonize Validate vocabulary and preserve validation results (CF-066)
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('clicking Validate displays validating… then valid · N resources', async ({ page }) => {
  test.setTimeout(90000)
  await page.goto('/')

  const validChip = page.locator('#valid')
  const valBtn = page.locator('#validateBtn')

  // Wait for initial preview generation to complete
  await expect(validChip).toHaveText(/(preview|written|ok) · \d+ files/, { timeout: 10000 })
  await expect(valBtn).toBeEnabled()

  // Delay /api/render response slightly to reliably observe "validating…" state
  await page.route('**/api/render', async (route) => {
    await new Promise((r) => setTimeout(r, 200))
    await route.continue()
  })

  await valBtn.click()

  // Must display "validating…" while in flight
  await expect(validChip).toHaveText('validating…', { timeout: 5000 })

  // Then transition to "valid · N resources" (or "validate ok · N resources" or "validation check unavailable")
  await expect(validChip).toHaveText(/(?:valid|validate ok) · \d+ resources?|validation check unavailable/, { timeout: 90000 })
})

test('validation result is not erased by idle background preview generation', async ({ page }) => {
  test.setTimeout(90000)
  await page.goto('/')

  const validChip = page.locator('#valid')
  const valBtn = page.locator('#validateBtn')

  // Wait for initial preview generation to complete
  await expect(validChip).toHaveText(/(preview|written|ok) · \d+ files/, { timeout: 10000 })
  await expect(valBtn).toBeEnabled()

  // Trigger manual Validate
  await valBtn.click()
  await expect(validChip).toHaveText(/(?:valid|validate ok) · \d+ resources?|validation check unavailable/, { timeout: 90000 })
  const validatedText = await validChip.textContent()

  // Trigger idle background preview generation (store.generate(false))
  await page.evaluate(() => window.store.generate(false))

  // Wait well past the 300ms debounce
  await page.waitForTimeout(600)

  // Validation result must persist and not be overwritten by preview
  await expect(validChip).toHaveText(validatedText)

  // When the blueprint actually changes, debounced preview generation updates the chip
  await page.evaluate(() => {
    window.store.replaceDoc((doc) => {
      doc.spec.xrd.parameters.region.description = 'Target deployment region'
    })
  })

  // Debounced preview (300ms) runs and updates chip to preview mode
  await expect(validChip).toHaveText(/(preview|written|ok) · \d+ files/, { timeout: 10000 })
})
