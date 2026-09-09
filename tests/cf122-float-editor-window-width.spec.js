const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('floating the editor drawer maintains full width and fills viewport', async ({ page }) => {
  await page.goto('/')
  // Select blueprint tab in output drawer
  await page.click('#tabs button[data-t="bp"]')
  // Click edit button
  await page.click('#code-edit')
  const editor = page.locator('#code-editor')
  await expect(editor).toBeVisible()

  // Float the drawer
  await page.click('#drawer-float-btn')

  // Bounding box of editor must be comfortably wide (>= 400px), not 24px
  const box = await editor.boundingBox()
  expect(box).not.toBeNull()
  expect(box.width).toBeGreaterThan(400)
  expect(box.height).toBeGreaterThan(150)
})
