const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('renaming XRD kind re-derives plural and updates subtitle', async ({ page, request }) => {
  await page.goto('/')
  // Select XRD card
  await page.click('.node[data-id="xrd"] .node-h')
  const kindInput = page.locator('#xk')
  await expect(kindInput).toBeVisible()

  await kindInput.fill('XPostgres')
  await kindInput.press('Enter')

  // Inspector subtitle should update from xnotifies.platform.sparky.ee to xpostgreses.platform.sparky.ee
  const subtitle = page.locator('.insp-t .g')
  await expect(subtitle).not.toContainText('xnotifies.')

  const res = await request.get(ENGINE + '/api/blueprint')
  const bp = await res.json()
  expect(bp.spec.xrd.kind).toBe('XPostgres')
  expect(bp.spec.xrd.plural).not.toBe('xnotifies')
})
