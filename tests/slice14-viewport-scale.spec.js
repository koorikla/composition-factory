// Slice 14 — the app scales to the window: no dead band under the output
// drawer on tall screens, no overflow on short ones.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('the app fills a tall window and the code pane reaches the bottom', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1100 })
  await page.goto('/')
  await expect(page.locator('#code')).toContainText('kind: Composition')
  const m = await page.evaluate(() => {
    const app = document.querySelector('.app').getBoundingClientRect()
    const code = document.getElementById('code').getBoundingClientRect()
    return { appH: app.height, codeBottom: code.bottom, winH: innerHeight, bodyScroll: document.body.scrollHeight }
  })
  expect(m.appH).toBeGreaterThan(1050)               // fills, not capped at 860
  expect(m.winH - m.codeBottom).toBeLessThan(40)     // code pane reaches the bottom
  expect(m.bodyScroll).toBeLessThanOrEqual(m.winH + 2) // no page scrollbar
})

test('a short window still fits without a page scrollbar', async ({ page }) => {
  await page.setViewportSize({ width: 1100, height: 640 })
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  const m = await page.evaluate(() => ({ winH: innerHeight, bodyScroll: document.body.scrollHeight }))
  expect(m.bodyScroll).toBeLessThanOrEqual(m.winH + 2)
})
