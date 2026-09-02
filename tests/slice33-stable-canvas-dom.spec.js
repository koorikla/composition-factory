// Slice 33 — selection must not rebuild the canvas DOM: a click that lands
// right after another click used to hit a destroyed element (innerHTML
// rebuild), which read as "can't click anything" at human speed.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('selecting one card leaves every other card element connected', async ({ page }) => {
  await page.goto('/')
  const dl = await page.locator('.node[data-id="dead-letter"]').elementHandle()
  await page.click('.node[data-id="work-queue"] .node-h')
  await expect(page.locator('.node.sel')).toHaveAttribute('data-id', 'work-queue')
  expect(await dl.evaluate(el => el.isConnected)).toBe(true)  // not rebuilt
  const wq = await page.locator('.node[data-id="work-queue"]').elementHandle()
  await page.click('.node[data-id="dead-letter"] .node-h')
  await expect(page.locator('.node.sel')).toHaveAttribute('data-id', 'dead-letter')
  expect(await wq.evaluate(el => el.isConnected)).toBe(true)
})

test('rapid alternating clicks always land (no dead clicks)', async ({ page }) => {
  await page.goto('/')
  for (let i = 0; i < 6; i++) {
    const id = i % 2 ? 'work-queue' : 'dead-letter'
    await page.click('.node[data-id="' + id + '"] .node-h')
    await expect(page.locator('.node.sel')).toHaveAttribute('data-id', id)
  }
})
