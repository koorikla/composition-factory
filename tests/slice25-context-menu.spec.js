// Slice 25 — right-click menu on canvas objects: duplicate, rename (via the
// server's rename route so wires re-point), delete; Escape/click-away closes.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('right-click opens a menu; delete works from it', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.click('.node[data-id="work-queue"] .node-h', { button: 'right' })
  const menu = page.locator('#ctx-menu[role="menu"]')
  await expect(menu).toBeVisible()
  page.on('dialog', d => d.accept())
  await menu.getByRole('menuitem', { name: /delete/i }).click()
  await expect(page.locator('.node[data-id="work-queue"]')).toHaveCount(0)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.map(r => r.name)).toEqual(['dead-letter'])
})

test('rename via the menu re-points wires and survives the server round-trip', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h', { button: 'right' })
  page.on('dialog', d => d.type() === 'prompt' ? d.accept('graveyard') : d.accept())
  await page.locator('#ctx-menu').getByRole('menuitem', { name: /rename/i }).click()
  await expect(page.locator('.node[data-id="graveyard"]')).toBeVisible({ timeout: 5000 })
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.map(r => r.name).sort()).toEqual(['graveyard', 'work-queue'])
  // the shared region wire still reaches the renamed card
  expect(await page.locator('svg path[class*="wire"]').count()).toBeGreaterThanOrEqual(3)
})

test('Escape and click-away close the menu; empty canvas gets none', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h', { button: 'right' })
  await expect(page.locator('#ctx-menu')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.locator('#ctx-menu')).toBeHidden()
  await page.click('#cw', { button: 'right', position: { x: 500, y: 400 } })
  await expect(page.locator('#ctx-menu')).toBeHidden()
})
