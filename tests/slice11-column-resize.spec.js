// Slice 11 — resizable side columns: drag handles on the palette and
// inspector edges, widths clamped and persisted per browser.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

async function paletteWidth(page) {
  return page.evaluate(() => document.getElementById('region-palette').getBoundingClientRect().width)
}

test('dragging the palette handle resizes the column and it persists across reload', async ({ page }) => {
  await page.goto('/')
  const before = await paletteWidth(page)
  const handle = page.locator('#col-resize-l')
  await expect(handle).toBeVisible()
  const hb = await handle.boundingBox()
  await page.mouse.move(hb.x + hb.width / 2, hb.y + 200)
  await page.mouse.down()
  await page.mouse.move(hb.x + hb.width / 2 + 90, hb.y + 200, { steps: 5 })
  await page.mouse.up()
  const after = await paletteWidth(page)
  expect(after - before).toBeGreaterThan(60)
  await page.reload()
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  expect(Math.abs(await paletteWidth(page) - after)).toBeLessThan(4)
})

test('the inspector handle resizes too, and widths clamp to sane bounds', async ({ page }) => {
  await page.goto('/')
  const handle = page.locator('#col-resize-r')
  const hb = await handle.boundingBox()
  await page.mouse.move(hb.x + hb.width / 2, hb.y + 200)
  await page.mouse.down()
  await page.mouse.move(hb.x + hb.width / 2 + 900, hb.y + 200, { steps: 5 })   // absurd drag
  await page.mouse.up()
  const w = await page.evaluate(() => document.getElementById('region-inspector').getBoundingClientRect().width)
  expect(w).toBeGreaterThanOrEqual(180)   // clamped, never crushed to nothing
  const canvasW = await page.evaluate(() => document.getElementById('cw').getBoundingClientRect().width)
  expect(canvasW).toBeGreaterThan(300)    // canvas never swallowed
})
