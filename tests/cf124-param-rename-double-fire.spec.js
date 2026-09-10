const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('adding and renaming a parameter with Enter does not show false error toast or inspector error banner', async ({ page }) => {
  await page.goto('/')
  // Select XRD
  await page.click('.node[data-id="xrd"] .node-h')

  // Click Add parameter
  await page.click('#addParamBtn')

  // Find the newly added parameter input (default "newParam")
  const input = page.locator('#insp input[data-pn="newParam"]')
  await expect(input).toBeVisible()

  // Track rename requests to assert exactly one request is sent
  const renameRequests = []
  page.on('request', req => {
    if (req.url().includes('/api/blueprint/parameters/') && req.url().includes('/rename')) {
      renameRequests.push(req.url())
    }
  })

  // Type new name and press Enter
  await input.fill('envRegion')
  await input.press('Enter')

  // Wait a short duration for network round-trips
  await page.waitForTimeout(600)

  // Verify exactly one rename request was sent
  expect(renameRequests).toHaveLength(1)

  // Verify toast does not show error
  const toast = page.locator('#toast')
  if (await toast.isVisible()) {
    const toastText = await toast.textContent()
    expect(toastText).not.toMatch(/is not declared/i)
  }

  // Verify inspector error banner does not show error
  const banner = page.locator('#insp .warnbar, #insp [role="alert"]')
  await expect(banner).toHaveCount(0)

  // Verify the parameter has been renamed to "envRegion"
  const renamedInput = page.locator('#insp input[data-pn="envRegion"]')
  await expect(renamedInput).toBeVisible()
  await expect(renamedInput).toHaveValue('envRegion')
})

test('renaming an existing parameter with Enter fires exactly one rename request and shows no error', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="xrd"] .node-h')

  const input = page.locator('#insp input[data-pn="retention"]')
  await expect(input).toBeVisible()

  const renameRequests = []
  page.on('request', req => {
    if (req.url().includes('/api/blueprint/parameters/') && req.url().includes('/rename')) {
      renameRequests.push(req.url())
    }
  })

  await input.fill('retentionPeriod')
  await input.press('Enter')
  await page.waitForTimeout(600)

  expect(renameRequests).toHaveLength(1)

  const toast = page.locator('#toast')
  if (await toast.isVisible()) {
    const toastText = await toast.textContent()
    expect(toastText).not.toMatch(/is not declared/i)
  }

  const banner = page.locator('#insp .warnbar, #insp [role="alert"]')
  await expect(banner).toHaveCount(0)

  const renamedInput = page.locator('#insp input[data-pn="retentionPeriod"]')
  await expect(renamedInput).toBeVisible()
  await expect(renamedInput).toHaveValue('retentionPeriod')
})

test('renaming a parameter and blurring does not trigger duplicate rename requests', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="xrd"] .node-h')

  const input = page.locator('#insp input[data-pn="retention"]')
  await expect(input).toBeVisible()

  const renameRequests = []
  page.on('request', req => {
    if (req.url().includes('/api/blueprint/parameters/') && req.url().includes('/rename')) {
      renameRequests.push(req.url())
    }
  })

  await input.fill('retentionSecs')
  // Blur by calling blur()
  await input.blur()
  await page.waitForTimeout(600)

  expect(renameRequests).toHaveLength(1)

  const toast = page.locator('#toast')
  if (await toast.isVisible()) {
    const toastText = await toast.textContent()
    expect(toastText).not.toMatch(/is not declared/i)
  }

  const banner = page.locator('#insp .warnbar, #insp [role="alert"]')
  await expect(banner).toHaveCount(0)

  const renamedInput = page.locator('#insp input[data-pn="retentionSecs"]')
  await expect(renamedInput).toBeVisible()
  await expect(renamedInput).toHaveValue('retentionSecs')
})

