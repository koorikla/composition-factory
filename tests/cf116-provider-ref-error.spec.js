const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('mistyped provider ref in SOURCES shows package not found guidance, not backend server restarting', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await page.fill('#src-add-ref', 'ghcr.io/nope/does-not-exist:v0.0.1')
  await page.click('#src-add-btn')

  const alert = page.locator('#region-palette [role="alert"], #region-palette .warnbar, #toast').first()
  await expect(alert).toBeVisible({ timeout: 15000 })
  const text = await alert.textContent()
  expect(text).not.toMatch(/backend server may be restarting/i)
  expect(text).toMatch(/not found|cannot find package|package.*not found|manifest unknown|access denied/i)
})

test('upstream registry connection refused shows registry unreachable guidance, not backend server restarting', async ({ page }) => {
  await page.goto('/')

  await page.route('**/api/providers', async route => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 502,
        statusText: 'Bad Gateway',
        contentType: 'application/json',
        body: JSON.stringify({
          error: 'fetch "ghcr.io/x/provider-aws:v1.0.0": GET https://ghcr.io/v2/: dial tcp: connection refused'
        })
      })
    } else {
      await route.continue()
    }
  })

  await page.click('#rtabs button[data-r="src"]')
  await page.fill('#src-add-ref', 'ghcr.io/x/provider-aws:v1.0.0')
  await page.click('#src-add-btn')

  const alert = page.locator('#region-palette [role="alert"], #region-palette .warnbar, #toast').first()
  await expect(alert).toBeVisible({ timeout: 15000 })
  const text = await alert.textContent()
  expect(text).not.toMatch(/backend server may be restarting/i)
  expect(text).toMatch(/could not connect to registry|failed to fetch package/i)
})

test('upstream 404 from registry shows package not found guidance, not backend server restarting', async ({ page }) => {
  await page.goto('/')

  await page.route('**/api/providers', async route => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 502,
        statusText: 'Bad Gateway',
        contentType: 'application/json',
        body: JSON.stringify({
          error: 'fetch "ghcr.io/x/provider-aws:v9.9.9": unexpected status code 404 Not Found'
        })
      })
    } else {
      await route.continue()
    }
  })

  await page.click('#rtabs button[data-r="src"]')
  await page.fill('#src-add-ref', 'ghcr.io/x/provider-aws:v9.9.9')
  await page.click('#src-add-btn')

  const alert = page.locator('#region-palette [role="alert"], #region-palette .warnbar, #toast').first()
  await expect(alert).toBeVisible({ timeout: 15000 })
  const text = await alert.textContent()
  expect(text).not.toMatch(/backend server may be restarting/i)
  expect(text).toMatch(/package not found|not found/i)
})
