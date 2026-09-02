// Slice 7 — add a shared parameter from the SHARED rail: name/type/required
// form, persisted to the XRD, visible as $param and on the XR card.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('the SHARED rail adds a parameter that lands in the XRD and on the XR card', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')
  await page.click('#param-add-btn')
  await page.fill('#param-add-name', 'environment')
  await page.selectOption('#param-add-type', 'string')
  await page.check('#param-add-req')
  await page.click('#param-add-submit')
  await expect(page.locator('#lrail').getByText('$environment')).toBeVisible()
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.xrd.parameters.environment).toMatchObject({ type: 'string', required: true })
  await expect(page.locator('.node[data-id="xrd"] .port[data-path="environment"]')).toBeVisible()
})

test('an invalid parameter name shows the server error verbatim and changes nothing', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')
  await page.click('#param-add-btn')
  await page.fill('#param-add-name', 'not valid!')
  await page.click('#param-add-submit')
  await expect(page.locator('#region-palette [role="alert"]').first()).toContainText(/not valid!|name/i)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(Object.keys(doc.spec.xrd.parameters).sort()).toEqual(['providerName', 'region', 'retention'])
  // what was typed survives the error re-render
  await expect(page.locator('#param-add-name')).toHaveValue('not valid!')
})
