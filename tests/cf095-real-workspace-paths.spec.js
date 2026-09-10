const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('topbar crumb and drawer breadcrumb display real workspace and output paths', async ({ page }) => {
  await page.goto('/')

  // 1. Top bar crumb reflects the real served blueprint path (not blueprints/...)
  const crumb = page.locator('#crumb')
  await expect(crumb).toBeVisible()
  await expect(crumb).not.toContainText('blueprints/')
  await expect(crumb).toContainText('doc.cf.yaml')

  // 2. Select composition in the drawer tree; verify breadcrumb displays compositions/<plural>.<group>.yaml
  const compItem = page.locator('#tree-root .tree-item[data-t="comp"]')
  await expect(compItem).toBeVisible()
  await compItem.click()
  const ebPath = page.locator('#eb-path')
  await expect(ebPath).toContainText('compositions/xnotifies.platform.sparky.ee.yaml')

  // 3. Select definition in the drawer tree; verify breadcrumb displays xrds/<plural>.<group>.yaml
  const xrdItem = page.locator('#tree-root .tree-item[data-t="xrd"]')
  await expect(xrdItem).toBeVisible()
  await xrdItem.click()
  await expect(ebPath).toContainText('xrds/xnotifies.platform.sparky.ee.yaml')
})
