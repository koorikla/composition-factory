// Slice 21 — shared parameters: removable from the SHARED rail, and the add
// form's controls follow the chosen type (object params are free-form string
// maps — no default/enum to fill).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('an unused parameter can be removed from the SHARED rail', async ({ page, request }) => {
  await request.post(ENGINE + '/api/blueprint/parameters', {
    data: { name: 'scratch', parameter: { type: 'string' } } })
  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')
  page.on('dialog', d => d.accept())
  await page.click('.card:has-text("$scratch") [data-param-del]')
  await expect(page.locator('#lrail').getByText('$scratch')).toHaveCount(0)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.xrd.parameters.scratch).toBeUndefined()
})

test('removing a wired parameter shows the referencers verbatim and keeps it', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')
  page.on('dialog', d => d.accept())
  await page.click('.card:has-text("$region") [data-param-del]')
  await expect(page.locator('#region-palette [role="alert"]').first()).toContainText(/work-queue|dead-letter/)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.xrd.parameters.region).toBeTruthy()
})

test('the add form adapts to the type: object hides default/enum and explains itself', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')
  await page.click('#param-add-btn')
  await expect(page.locator('#param-add-default')).toBeVisible()
  await page.selectOption('#param-add-type', 'object')
  await expect(page.locator('#param-add-default')).toBeHidden()
  await expect(page.locator('#param-add-enum')).toBeHidden()
  await expect(page.locator('#lrail')).toContainText(/free-form string map/i)
  await page.selectOption('#param-add-type', 'boolean')
  await expect(page.locator('#param-add-default')).toBeVisible()
  await expect(page.locator('#param-add-enum')).toBeHidden()
})
