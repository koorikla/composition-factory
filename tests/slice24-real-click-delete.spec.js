// Slice 24 — a real mouse press on a card action must work: pointerdown used
// to enter the drag/select path, re-render the card mid-press, and destroy
// the very button being clicked (click never fired — "still cannot delete").
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('a single real press-and-release on the delete button deletes', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')   // select first (buttons appear)
  const del = page.locator('.node[data-id="work-queue"] [data-act="delete"]')
  await expect(del).toBeVisible()
  page.on('dialog', d => d.accept())
  const box = await del.boundingBox()
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await page.mouse.down()                                    // real gesture: down…
  await page.waitForTimeout(120)                             // …a human-length press…
  await page.mouse.up()                                      // …up on whatever is there now
  await expect(page.locator('.node[data-id="work-queue"]')).toHaveCount(0, { timeout: 5000 })
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.map(r => r.name)).toEqual(['dead-letter'])
})

test('pressing an action button never starts a card drag', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  const before = await page.locator('.node[data-id="work-queue"]').boundingBox()
  const dup = page.locator('.node[data-id="work-queue"] [data-act="duplicate"]')
  const box = await dup.boundingBox()
  await page.mouse.move(box.x + 3, box.y + 3)
  await page.mouse.down()
  await page.mouse.move(box.x + 40, box.y + 40, { steps: 4 })  // drag attempt from the button
  await page.mouse.up()
  const after = await page.locator('.node[data-id="work-queue"]').boundingBox()
  expect(Math.abs(after.x - before.x)).toBeLessThan(2)
  expect(Math.abs(after.y - before.y)).toBeLessThan(2)
})
