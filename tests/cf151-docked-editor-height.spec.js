const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('docked editor fills drawer available height and opens at top', async ({ page }) => {
  await page.goto('/')
  // Select blueprint tab in output drawer
  await page.click('#tabs button[data-t="bp"]')

  // Click edit button
  await page.click('#code-edit')
  const editor = page.locator('#code-editor')
  await expect(editor).toBeVisible()

  // Docked editor must fill available drawer height (comfortably > 115px in 166px drawer body, not 40px)
  const box = await editor.boundingBox()
  expect(box).not.toBeNull()
  expect(box.height).toBeGreaterThan(115)

  // Subbar should be hidden while editing so editor has full height
  await expect(page.locator('#editor-subbar')).toBeHidden()

  // Editor must open at the top of the document (scrollTop 0, not scrolled to the end)
  const scrollTop = await editor.evaluate((el) => el.scrollTop)
  expect(scrollTop).toBe(0)

  // Canceling hides the editor and restores subbar
  await page.click('#code-cancel')
  await expect(editor).toBeHidden()
  await expect(page.locator('#editor-subbar')).toBeVisible()
})
