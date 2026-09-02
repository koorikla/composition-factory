// Slice 36 — the wordmark shows the real build version, not schema versions.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test('the topbar wordmark shows the server build version', async ({ page, request }) => {
  await resetDoc(request)
  const v = (await (await request.get(ENGINE + '/api/version')).json()).version
  expect(v.length).toBeGreaterThan(2)
  await page.goto('/')
  await expect(page.locator('#ver')).toHaveText(v)
  await expect(page.locator('#ver')).not.toHaveText(/v1alpha1|v0\.1\.0/)
})
