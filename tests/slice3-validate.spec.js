// Slice 3 — Validate: a real `crossplane composition render` of the current
// blueprint against a synthesized sample XR, result in the topbar chip.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('Validate renders the composition for real and reports the resource count', async ({ page }) => {
  await page.goto('/')
  await page.click('#validateBtn')
  await expect(page.locator('#valid')).toContainText(/render ok · \d+ resources/, { timeout: 90000 })
})

test('a template that dies under missingkey=error surfaces the render error verbatim', async ({ page, request }) => {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  doc.spec.resources.find(r => r.name === 'dead-letter').fields.tags =
    { raw: '{purpose: {{ $spec.doesNotExist | quote }}}' }
  const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(put.ok()).toBeTruthy()
  await page.goto('/')
  await page.click('#validateBtn')
  // the engine's render failure, verbatim — missingkey=error names the map key
  await expect(page.locator('#valid, #region-output .warnbar').last())
    .toContainText(/doesNotExist|map has no entry/, { timeout: 90000 })
})
