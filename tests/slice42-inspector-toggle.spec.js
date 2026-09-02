// Slice 42 — the inspector is always summonable: at narrow width a toggle
// button opens it even with nothing selected (there was no affordance at
// all — "still no inspector").
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('narrow width with no selection: the toggle opens the inspector', async ({ page }) => {
  await page.setViewportSize({ width: 820, height: 700 })
  await page.goto('/')
  await expect(page.locator('.pane.r')).toBeHidden()
  const toggle = page.locator('#pane-toggle-r')
  await expect(toggle).toBeVisible()
  await toggle.click()
  await expect(page.locator('.pane.r')).toBeVisible()
  await expect(page.locator('#insp')).toContainText(/Parameters/) // the XRD editor is the empty-selection view
  await page.click('#pane-close-r')
  await expect(page.locator('.pane.r')).toBeHidden()
})

test('desktop width hides the toggle (inspector is a column there)', async ({ page }) => {
  await page.setViewportSize({ width: 1300, height: 800 })
  await page.goto('/')
  await expect(page.locator('.pane.r')).toBeVisible()
  await expect(page.locator('#pane-toggle-r')).toBeHidden()
})
