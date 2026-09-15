// CF-492 — Failed provider add error message must show its cause where the user is looking,
// without horizontal clipping or the signed URL query string.
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

const MOCK_REGISTRY_ERROR =
  'ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0: read image config: Get "https://pkg-containers.githubusercontent.com/ghcrblobs11/blobs/sha256:604c2759f2c96c4293998fcf9a1ef5d470ee649ac65ee2e652a2656feec37e3d?hmac=REDACTED&se=REDACTED&sig=REDACTED&ske=REDACTED&skoid=REDACTED&sks=REDACTED&skt=REDACTED&sktid=REDACTED&skv=REDACTED&sp=REDACTED&spr=REDACTED&sr=REDACTED&sv=REDACTED": Forbidden'

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('catalogue provider add failure strips signed URL query string and wraps cause within panel width', async ({ page }) => {
  await page.goto('/')

  await page.route('**/api/providers', async route => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 502,
        statusText: 'Bad Gateway',
        contentType: 'application/json',
        body: JSON.stringify({ error: MOCK_REGISTRY_ERROR })
      })
    } else {
      await route.continue()
    }
  })

  // SOURCES → search provider-aws-rds → Add
  await page.click('#rtabs button[data-r="src"]')
  const searchInput = page.locator('#cat-search')
  await expect(searchInput).toBeVisible()
  await searchInput.fill('provider-aws-rds')

  const catRow = page.locator('#lrail .cat-row', { hasText: 'provider-aws-rds' }).first()
  await expect(catRow).toBeVisible()

  const addBtn = catRow.locator('button.cat-add')
  await expect(addBtn).toBeVisible()
  await addBtn.click()

  const alert = page.locator('#region-palette .warnbar[role="alert"]').first()
  await expect(alert).toBeVisible({ timeout: 15000 })

  const text = await alert.textContent()
  // Signed URL query string carries no information for the reader and must not be shown
  expect(text).not.toContain('hmac=')
  expect(text).not.toContain('REDACTED')
  // The actionable cause token must be present
  expect(text).toContain('Forbidden')

  // The error element must wrap to the panel's width and not overflow horizontally
  const { clientWidth, scrollWidth } = await alert.evaluate(el => ({
    clientWidth: el.clientWidth,
    scrollWidth: el.scrollWidth
  }))
  expect(clientWidth).toBeGreaterThan(0)
  expect(scrollWidth).toBeLessThanOrEqual(clientWidth)
})

test('manual provider add failure strips signed URL query string and wraps cause within panel width', async ({ page }) => {
  await page.goto('/')

  await page.route('**/api/providers', async route => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 502,
        statusText: 'Bad Gateway',
        contentType: 'application/json',
        body: JSON.stringify({ error: MOCK_REGISTRY_ERROR })
      })
    } else {
      await route.continue()
    }
  })

  await page.click('#rtabs button[data-r="src"]')
  await page.fill('#src-add-ref', 'ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0')
  await page.click('#src-add-btn')

  const alert = page.locator('#region-palette .warnbar[role="alert"]').first()
  await expect(alert).toBeVisible({ timeout: 15000 })

  const text = await alert.textContent()
  expect(text).not.toContain('hmac=')
  expect(text).not.toContain('REDACTED')
  expect(text).toContain('Forbidden')

  const { clientWidth, scrollWidth } = await alert.evaluate(el => ({
    clientWidth: el.clientWidth,
    scrollWidth: el.scrollWidth
  }))
  expect(clientWidth).toBeGreaterThan(0)
  expect(scrollWidth).toBeLessThanOrEqual(clientWidth)
})
