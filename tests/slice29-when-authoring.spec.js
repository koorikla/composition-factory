// Slice 29 — author `when:` from the inspector: a condition builder writing
// the engine's exact grammar, badge on the card, loop proven by render count.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('building a condition persists the canonical grammar and badges the card', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h')
  await page.selectOption('#insp select[data-when-param]', 'region')
  await page.selectOption('#insp select[data-when-op]', '==')
  await page.selectOption('#insp select[data-when-val]', 'eu-north-1')
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    return doc.spec.resources.find(r => r.name === 'dead-letter').when
  }).toBe('params.region == "eu-north-1"')
  await expect(page.locator('.node[data-id="dead-letter"]')).toContainText(/when/i)
  await expect(page.locator('#code')).toContainText('{{- if', { timeout: 8000 })
})

test('the render check counts the conditional resource in and out', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h')
  await page.selectOption('#insp select[data-when-param]', 'region')
  await page.selectOption('#insp select[data-when-op]', '==')
  await page.selectOption('#insp select[data-when-val]', 'eu-north-1')
  await expect(page.locator('#code')).toContainText('{{- if', { timeout: 8000 })
  await page.click('#validateBtn')
  // the sample XR takes the first enum value (eu-north-1): condition true
  await expect(page.locator('#valid')).toContainText('render ok · 2 resources', { timeout: 90000 })
  await page.selectOption('#insp select[data-when-op]', '!=')
  await page.waitForTimeout(600)   // let the doc PUT + debounced regenerate settle
  await page.click('#validateBtn')
  await expect(page.locator('#valid')).toContainText('render ok · 1 resource', { timeout: 90000 })
})

test('clearing the condition removes the key and the badge', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h')
  await page.selectOption('#insp select[data-when-param]', 'region')
  await page.selectOption('#insp select[data-when-op]', '==')
  await page.selectOption('#insp select[data-when-val]', 'eu-north-1')
  await expect(page.locator('.node[data-id="dead-letter"]')).toContainText(/when/i)
  await page.selectOption('#insp select[data-when-param]', '')
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    return doc.spec.resources.find(r => r.name === 'dead-letter').when || 'gone'
  }).toBe('gone')
  await expect(page.locator('.node[data-id="dead-letter"]')).not.toContainText(/when/i)
})
