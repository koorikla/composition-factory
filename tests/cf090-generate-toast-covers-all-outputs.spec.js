const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('generate banner and toast instruct an apply command that covers all written files', async ({ page }) => {
  await page.goto('/')
  // Accept the generate confirmation dialog
  page.on('dialog', dialog => dialog.accept())

  // Click topbar Generate button
  await page.click('#generateBtn')

  // The output next-steps banner must be visible
  const banner = page.locator('#out-next-steps')
  await expect(banner).toBeVisible()

  // Must not claim output is only written to "compositions"
  await expect(banner).not.toContainText('Output written to compositions')
  // The apply instruction must cover all written manifests recursively or at root
  await expect(banner).toContainText('kubectl apply -R -f')
})
