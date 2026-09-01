// Slice 26 — manual card resize: a grip on the selected card widens it past
// the auto cap, size sticks like positions do, double-click resets.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('dragging the grip widens the card beyond the auto cap and persists', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  const grip = page.locator('.node[data-id="work-queue"] [data-resize]')
  await expect(grip).toBeVisible()
  const before = await page.locator('.node[data-id="work-queue"]').boundingBox()
  const g = await grip.boundingBox()
  await page.mouse.move(g.x + g.width / 2, g.y + g.height / 2)
  await page.mouse.down()
  await page.mouse.move(g.x + 260, g.y + 10, { steps: 5 })
  await page.mouse.up()
  const after = await page.locator('.node[data-id="work-queue"]').boundingBox()
  expect(after.width).toBeGreaterThan(before.width + 200)   // beyond the 340px cap
  expect(Math.abs(after.x - before.x)).toBeLessThan(2)      // resized, not dragged
  await page.click('.node[data-id="dead-letter"] .node-h')  // re-render via selection
  const kept = await page.locator('.node[data-id="work-queue"]').boundingBox()
  expect(Math.abs(kept.width - after.width)).toBeLessThan(2)
})

test('double-clicking the grip resets to automatic sizing', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  const auto = await page.locator('.node[data-id="work-queue"]').boundingBox()
  const grip = page.locator('.node[data-id="work-queue"] [data-resize]')
  const g = await grip.boundingBox()
  await page.mouse.move(g.x + 4, g.y + 4)
  await page.mouse.down(); await page.mouse.move(g.x + 200, g.y, { steps: 4 }); await page.mouse.up()
  await grip.dblclick()
  const reset = await page.locator('.node[data-id="work-queue"]').boundingBox()
  expect(Math.abs(reset.width - auto.width)).toBeLessThan(3)
})
