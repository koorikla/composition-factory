const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  test.setTimeout(90000)
  await resetDoc(request)
})

test('validating a blueprint with an optional parameter wired to a required field displays hint', async ({ page, request }) => {

  // Update blueprint so that parameter "region" is optional (required: false, no default)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  doc.spec.xrd.parameters.region.required = false
  doc.spec.xrd.parameters.region.default = ""
  const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(put.ok()).toBeTruthy()

  await page.goto('/')
  await page.click('#validateBtn')

  // Expect warning banner or drawer to contain the optional parameter guidance
  const warnText = page.locator('#render-warn, #render-warn-text').first()
  await expect(warnText).toBeVisible({ timeout: 90000 })
  await expect(warnText).toContainText('params.region')
  await expect(warnText).toContainText('optional parameter')
  await expect(warnText).toContainText('mark parameter required in the XRD or provide a default')
})
