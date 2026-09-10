const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('dragging a parameter onto a resource output row does not wire status or stick header chip on error', async ({ page }) => {
  await page.goto('/')

  // Verify header chip initially shows preview/written, not error
  const validChip = page.locator('#valid')
  await expect(validChip).toBeVisible()
  expect(await validChip.textContent()).not.toMatch(/error/i)

  // Find a parameter port on the XRD card (e.g. region)
  const paramPort = page.locator('.node[data-id="xrd"] .port[data-path="region"] .d.out').first()
  await expect(paramPort).toBeVisible()

  // Find a resource card (e.g. instance) with an output port
  const outputPort = page.locator('.node:not([data-id="xrd"]) .port[data-path^="status."]').first()
  await expect(outputPort).toBeVisible()

  // Drag the parameter dot and drop directly on the output port
  await paramPort.hover()
  await page.mouse.down()
  await outputPort.hover()
  await page.mouse.up()

  // Wait briefly for any potential network requests or store mutations
  await page.waitForTimeout(500)

  // Verify the header chip did not stick on error
  expect(await validChip.textContent()).not.toMatch(/error/i)

  // Verify no invalid wire was created in the blueprint
  const d = await page.evaluate(() => window.store ? window.store.state.doc : null)
  if (d && d.spec && d.spec.resources) {
    for (const r of d.spec.resources) {
      if (r.fields) {
        for (const k of Object.keys(r.fields)) {
          expect(k).not.toMatch(/^status\./)
        }
      }
    }
  }
})

test('output rows do not get wire-target-hover class and drop falls back to field picker', async ({ page }) => {
  await page.goto('/')

  const validChip = page.locator('#valid')
  await expect(validChip).toBeVisible()

  const paramPort = page.locator('.node[data-id="xrd"] .port[data-path="region"] .d.out').first()
  const outputPort = page.locator('.node:not([data-id="xrd"]) .port[data-path^="status."]').first()
  await expect(paramPort).toBeVisible()
  await expect(outputPort).toBeVisible()

  // Drag over outputPort and verify it does NOT receive wire-target-hover class
  await paramPort.hover()
  await page.mouse.down()
  await outputPort.hover()

  const hasTargetHover = await outputPort.evaluate((el) => el.classList.contains('wire-target-hover'))
  expect(hasTargetHover).toBe(false)

  // Releasing over the output row falls back to the resource card and opens the wire picker
  await page.mouse.up()
  const picker = page.locator('#wire-picker')
  await expect(picker).toBeVisible()

  // Picker should only contain spec fields / envelope / annotations, never status fields
  const itemTexts = await picker.locator('.wire-picker-item .path').allTextContents()
  for (const text of itemTexts) {
    expect(text).not.toMatch(/^status\./)
  }
})

test('rejected replaceDoc write does not stick header chip on error', async ({ page }) => {
  await page.goto('/')
  const validChip = page.locator('#valid')
  await expect(validChip).toHaveText(/preview|written|ok/)

  // Trigger a rejected write by attempting to save an invalid doc causing 400 from the server
  await page.evaluate(async () => {
    await window.store.replaceDoc((d) => {
      d.apiVersion = "invalid/v1"
      return d
    })
  })

  // Wait briefly for store error handling
  await page.waitForTimeout(300)

  // Verify the header chip remains on preview/written and is not stuck on error
  expect(await validChip.textContent()).not.toMatch(/error/i)
})
