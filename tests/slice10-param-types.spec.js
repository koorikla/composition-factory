// Slice 10 — parameter controls follow the type: object params are free-form
// string maps (no default/enum, an explanatory hint), booleans get true/false,
// and "array" (engine-rejected) is not offered anywhere.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('object type hides default/enum and explains the map shape; boolean gets true/false', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="xrd"] .node-h')
  await page.click('#addParamBtn')
  const row = page.locator('#insp .fld', { has: page.locator('input[data-pn="newParam"]') })
  await row.locator('select[data-pt]').selectOption('object')
  await expect(row.locator('input[data-pdef]')).toHaveCount(0)
  await expect(row.locator('input[data-pe]')).toHaveCount(0)
  await expect(row).toContainText(/free-form.*map|map.*string/i)
  await row.locator('select[data-pt]').selectOption('boolean')
  await expect(row.locator('select[data-pdef]')).toBeVisible()
  const opts = await row.locator('select[data-pdef] option').allTextContents()
  expect(opts.join(',')).toMatch(/true/)
  await expect(row.locator('input[data-pe]')).toHaveCount(0)
})

test('array is not offered as a parameter type anywhere', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="xrd"] .node-h')
  const inspectorTypes = await page.locator('#insp select[data-pt] option').allTextContents()
  expect(inspectorTypes).not.toContain('array')
  await page.click('#rtabs button[data-r="shared"]')
  await page.click('#param-add-btn')
  const railTypes = await page.locator('#param-add-type option').allTextContents()
  expect(railTypes).not.toContain('array')
})

test('an object parameter persists and offers itself for map fields', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')
  await page.click('#param-add-btn')
  await page.fill('#param-add-name', 'labels')
  await page.selectOption('#param-add-type', 'object')
  await page.click('#param-add-submit')
  await expect(page.locator('#lrail').getByText('$labels')).toBeVisible()
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.xrd.parameters.labels).toMatchObject({ type: 'object' })
  // the map-typed field's bind quick-pick lists it (tags is a map)
  await page.click('.node[data-id="dead-letter"] .node-h')
  await page.click('#fseg button[data-f="all"]')
  await page.click('#insp button[data-m="w"][data-path="tags"]')
  const pick = page.locator('#insp select[data-wire="tags"]')
  await expect(pick).toBeVisible()
  const options = await pick.locator('option').allTextContents()
  expect(options.join(',')).toContain('labels')
})
