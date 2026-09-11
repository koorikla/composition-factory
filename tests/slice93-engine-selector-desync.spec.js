const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')

guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('switching engine to an unsupported choice displays error toast and does not desync selector', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.node')).toHaveCount(3)

  const engineSel = page.locator('#engineSel')
  await expect(engineSel).toHaveValue('go-templating')

  // The pristine document contains spec.conventions with template cf.tags.
  // Neither kcl nor python support template conventions, so switching engine
  // fails server-side validation with HTTP 400.
  await engineSel.selectOption('kcl')

  // The server error must be visible to the user in the error toast rather than
  // immediately swallowed by an unconditional post-mutation store.generate().
  const toast = page.locator('#canvas-error-toast')
  await expect(toast).toBeVisible()
  await expect(toast).toContainText('current engine is "kcl"')

  // The engine selector in the UI must reflect the actual persisted document state
  // rather than remaining stuck on the rejected engine value.
  await expect(engineSel).toHaveValue('go-templating')
})
