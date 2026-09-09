const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('status chip has role="status", aria-live="polite", and is keyboard focusable', async ({ page }) => {
  await page.goto('/')
  const validChip = page.locator('#valid')
  await expect(validChip).toHaveAttribute('role', 'status')
  await expect(validChip).toHaveAttribute('aria-live', 'polite')
  await expect(validChip).toHaveAttribute('tabindex', '0')

  // Verify focusability and focus-visible rule
  await validChip.focus()
  await expect(validChip).toBeFocused()

  const hasFocusVisibleRule = await page.evaluate(() => {
    for (const sheet of document.styleSheets) {
      try {
        for (const rule of sheet.cssRules) {
          if (rule.selectorText && rule.selectorText.includes('#valid:focus-visible')) {
            return true
          }
        }
      } catch (_) {}
    }
    return false
  })
  expect(hasFocusVisibleRule).toBe(true)
})

test('pressing Enter or Space on focused status chip expands collapsed drawer', async ({ page }) => {
  await page.goto('/')
  const drawer = page.locator('#region-output')
  const validChip = page.locator('#valid')

  // Collapse drawer
  await page.click('#drawer-min-btn')
  expect((await drawer.boundingBox()).height).toBeLessThanOrEqual(48)

  // Focus valid chip and press Enter
  await validChip.focus()
  await expect(validChip).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(drawer).not.toHaveAttribute('data-collapsed', '')
  expect((await drawer.boundingBox()).height).toBeGreaterThan(100)

  // Collapse again and press Space
  await page.click('#drawer-min-btn')
  expect((await drawer.boundingBox()).height).toBeLessThanOrEqual(48)

  await validChip.focus()
  await expect(validChip).toBeFocused()
  await page.keyboard.press('Space')
  await expect(drawer).not.toHaveAttribute('data-collapsed', '')
  expect((await drawer.boundingBox()).height).toBeGreaterThan(100)
})

test('Validate and Generate announce results with accessible screen-reader text', async ({ page, request }) => {
  test.setTimeout(90000)
  await page.goto('/')
  const validChip = page.locator('#valid')

  // Initial generate outcome sets accessible announcement text
  await expect(validChip).toHaveAttribute('aria-label', /(Preview|Generated)/i, { timeout: 10000 })

  // Trigger validate
  await page.click('#validateBtn')
  await expect(validChip).toContainText(/(?:valid|validate ok|render ok) · \d+ resources?|(?:validation|render) check unavailable/, { timeout: 90000 })
  await expect(validChip).toHaveAttribute('aria-label', /(?:Validation succeeded|(?:validation|render) check unavailable)/i)

  // Inject render error
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  doc.spec.resources.find(r => r.name === 'dead-letter').fields.tags =
    { raw: '{purpose: {{ $spec.doesNotExist | quote }}}' }
  const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(put.ok()).toBeTruthy()

  await page.goto('/')
  await page.click('#validateBtn')
  await expect(validChip).toContainText(/(?:validation|render) error|(?:validation|render) check unavailable/, { timeout: 90000 })
  await expect(validChip).toHaveAttribute('aria-label', /(?:Validation error|Render error|(?:validation|render) check unavailable)/i, { timeout: 90000 })
})
