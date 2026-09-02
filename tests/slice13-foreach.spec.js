// Slice 13 — forEach: a resource repeated N times, N from an integer
// parameter (RDS cluster + N instances pattern), proven through a real render.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const add = await request.post(ENGINE + '/api/blueprint/parameters', {
    data: { name: 'instanceCount', parameter: { type: 'integer', default: '2' } },
  })
  expect(add.ok()).toBeTruthy()
})

test('setting for-each on a resource persists, badges the card and loops the template', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h')
  const sel = page.locator('#insp select[data-foreach]')
  await expect(sel).toBeVisible()
  await sel.selectOption('params.instanceCount')
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    return doc.spec.resources.find(r => r.name === 'dead-letter').forEach
  }).toBe('params.instanceCount')
  await expect(page.locator('.node[data-id="dead-letter"]')).toContainText(/for each/i)
  await expect(page.locator('#code')).toContainText('range', { timeout: 8000 })
})

test('Validate proves the loop: render ok with one extra instance', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h')
  const sel = page.locator('#insp select[data-foreach]')
  await expect(sel).toBeVisible()
  await sel.selectOption('params.instanceCount')
  await expect(page.locator('#code')).toContainText('range', { timeout: 8000 })
  await page.click('#validateBtn')
  // work-queue + 2x dead-letter (instanceCount defaults to 2)
  await expect(page.locator('#valid')).toContainText('render ok · 3 resources', { timeout: 90000 })
})

test('removing the loop returns the card and render count to normal', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h')
  await page.locator('#insp select[data-foreach]').selectOption('params.instanceCount')
  await expect(page.locator('.node[data-id="dead-letter"]')).toContainText(/for each/i)
  await page.locator('#insp select[data-foreach]').selectOption('')
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    return doc.spec.resources.find(r => r.name === 'dead-letter').forEach || 'gone'
  }).toBe('gone')
  await expect(page.locator('.node[data-id="dead-letter"]')).not.toContainText(/for each/i)
})
