const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('providerName parameter row is locked against rename and deletion with clear visual cue', async ({ page }) => {
  await page.goto('/')
  // Select XRD
  await page.click('.node[data-id="xrd"] .node-h')

  // Find providerName name input
  const nameInput = page.locator('#insp input[data-pn="providerName"]')
  await expect(nameInput).toBeVisible()

  // Verify nameInput is readonly or disabled
  const isReadOnly = await nameInput.getAttribute('readonly')
  const isDisabled = await nameInput.getAttribute('disabled')
  expect(isReadOnly !== null || isDisabled !== null).toBe(true)

  // Find providerName delete button
  const delBtn = page.locator('#insp button[data-pd="providerName"]')
  await expect(delBtn).toBeVisible()

  // Verify delBtn is disabled
  const isDelDisabled = await delBtn.getAttribute('disabled')
  expect(isDelDisabled !== null).toBe(true)

  // Verify body never mentions terminal command instruction "run cf serve without --blueprint"
  const bodyText = await page.locator('body').innerText()
  expect(bodyText).not.toMatch(/run cf serve without --blueprint/i)
})
