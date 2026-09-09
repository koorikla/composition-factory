const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('when Validate fails while the drawer is collapsed, the failure error message is visible in the viewport', async ({ page, request }) => {
  test.setTimeout(90000)
  // Inject invalid template expression that triggers a render failure
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  doc.spec.resources.find(r => r.name === 'dead-letter').fields.tags =
    { raw: '{purpose: {{ $spec.doesNotExist | quote }}}' }
  const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(put.ok()).toBeTruthy()

  await page.setViewportSize({ width: 1280, height: 720 })
  await page.goto('/')

  // Collapse the output drawer
  await page.click('#drawer-min-btn')
  const drawer = page.locator('#region-output')
  const box = await drawer.boundingBox()
  expect(box.height).toBeLessThanOrEqual(48)

  // Click Validate
  await page.click('#validateBtn')

  // Validation fails: valid chip displays render error
  await expect(page.locator('#valid')).toContainText(/render error|render check unavailable/, { timeout: 90000 })

  // Failure error message must be visible in the viewport!
  const errorBanner = page.locator('.render-warn-banner, #render-warn-banner')
  await expect(errorBanner).toBeVisible({ timeout: 10000 })
  await expect(errorBanner).toBeInViewport()
  await expect(errorBanner).toContainText(/doesNotExist|map has no entry|render check unavailable/)

  // Clicking "Open in Drawer" on the banner expands the drawer and reveals the in-drawer warnbar
  await page.click('#render-warn-open-btn')
  await expect(drawer).not.toHaveAttribute('data-collapsed', '')
  const expandedBox = await drawer.boundingBox()
  expect(expandedBox.height).toBeGreaterThan(100)
  const inDrawerWarn = page.locator('#render-warn')
  await expect(inDrawerWarn).toBeVisible()
  await expect(inDrawerWarn).toBeInViewport()

  // Clicking dismiss button hides the top banner
  await page.click('#render-warn-dismiss')
  await expect(errorBanner).toBeHidden()
})

test('clicking valid chip expands collapsed drawer and reveals error diagnostics', async ({ page, request }) => {
  test.setTimeout(90000)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  doc.spec.resources.find(r => r.name === 'dead-letter').fields.tags =
    { raw: '{purpose: {{ $spec.doesNotExist | quote }}}' }
  const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(put.ok()).toBeTruthy()

  await page.setViewportSize({ width: 1280, height: 720 })
  await page.goto('/')

  // Collapse output drawer
  await page.click('#drawer-min-btn')
  const drawer = page.locator('#region-output')
  expect((await drawer.boundingBox()).height).toBeLessThanOrEqual(48)

  // Validate to trigger error
  await page.click('#validateBtn')
  const validChip = page.locator('#valid')
  await expect(validChip).toContainText(/render error|render check unavailable/, { timeout: 90000 })

  // Dismiss top banner to test valid chip reveal
  await page.click('#render-warn-dismiss')
  await expect(page.locator('#render-warn-banner')).toBeHidden()

  // Clicking valid chip expands drawer and reveals in-drawer error
  await validChip.click()
  await expect(drawer).not.toHaveAttribute('data-collapsed', '')
  expect((await drawer.boundingBox()).height).toBeGreaterThan(100)
  await expect(page.locator('#render-warn')).toBeInViewport()
})
