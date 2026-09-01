// Slice 8 — orientation UX: a GUIDE rail tab and informative mouseover text
// on fields, wires and topbar actions.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('a GUIDE tab explains the loop, the shortcuts and the wire colors', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="guide"]')
  const rail = page.locator('#lrail')
  await expect(rail).toContainText(/drag a kind/i)
  await expect(rail).toContainText(/duplicate/i)
  await expect(rail).toContainText(/zoom/i)
  await expect(rail).toContainText(/shared/i)   // wire-color legend explained
  await expect(rail).toContainText(/validate/i)
})

test('field rows and wires carry informative mouseover text', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  const portTitle = await page
    .locator('.node[data-id="work-queue"] .port[data-path="region"]')
    .getAttribute('title')
  expect(portTitle).toMatch(/region/)
  expect(portTitle).toMatch(/string/)
  const wireTip = await page.evaluate(() => {
    const t = document.querySelector('#wires path title')
    return t ? t.textContent : null
  })
  expect(wireTip).toMatch(/\$\w+ → [\w-]+\.\w+/)
  const vTitle = await page.locator('#validateBtn').getAttribute('title')
  expect((vTitle || '').length).toBeGreaterThan(10)
})
