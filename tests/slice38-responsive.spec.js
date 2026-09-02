// Slice 38 — narrow-screen layout: the side panes become slide-over drawers
// instead of vanishing (the "inspector disappears" bug was a media query
// hiding both panes below 900px).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('selecting a card at narrow width opens the inspector as an overlay', async ({ page }) => {
  await page.setViewportSize({ width: 800, height: 700 })
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  const insp = page.locator('.pane.r')
  await expect(insp).toBeVisible()
  await expect(page.locator('#insp .insp-t .k')).toContainText('Queue')
  // it overlays rather than squeezing the canvas
  const pos = await insp.evaluate(el => getComputedStyle(el).position)
  expect(pos).toBe('fixed')
  // and closes with its own button
  await page.click('#pane-close-r')
  await expect(insp).toBeHidden()
})

test('a kinds toggle opens the palette drawer at narrow width', async ({ page }) => {
  await page.setViewportSize({ width: 800, height: 700 })
  await page.goto('/')
  await expect(page.locator('.pane.l')).toBeHidden()
  await page.click('#pane-toggle-l')
  await expect(page.locator('.pane.l')).toBeVisible()
  await expect(page.locator('#lrail .kind[data-kind="Queue"]').first()).toBeVisible()
  await page.click('#pane-toggle-l')
  await expect(page.locator('.pane.l')).toBeHidden()
})

test('desktop width keeps the three-column layout untouched', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 900 })
  await page.goto('/')
  await expect(page.locator('.pane.l')).toBeVisible()
  await expect(page.locator('.pane.r')).toBeVisible()
  const pos = await page.locator('.pane.r').evaluate(el => getComputedStyle(el).position)
  expect(pos).not.toBe('fixed')
  await expect(page.locator('#pane-toggle-l')).toBeHidden()
})
