const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('network failure provides actionable guidance instead of bare Failed to fetch', async ({ page }) => {
  await page.goto('/')

  // Intercept an API route and abort it to simulate network disconnect
  await page.route('**/api/blueprint', async route => {
    await route.abort('failed')
  })

  const err = await page.evaluate(async () => {
    const api = await import('/js/api.js')
    try {
      await api.getBlueprint()
      return null
    } catch (e) {
      return { message: e.message, status: e.status }
    }
  })

  expect(err).not.toBeNull()
  expect(err.status).toBe(0)
  expect(err.message).toContain("Failed to connect to the Composition Factory server at /api/blueprint")
  expect(err.message).toContain("Ensure 'cf serve' is running and reachable")
  expect(err.message).not.toBe("network error: Failed to fetch")
})

test('502 Bad Gateway provides helpful server status rather than bare status code', async ({ page }) => {
  await page.goto('/')

  await page.route('**/api/blueprint', async route => {
    await route.fulfill({
      status: 502,
      statusText: 'Bad Gateway',
      contentType: 'text/plain',
      body: ''
    })
  })

  const err = await page.evaluate(async () => {
    const api = await import('/js/api.js')
    try {
      await api.getBlueprint()
      return null
    } catch (e) {
      return { message: e.message, status: e.status }
    }
  })

  expect(err).not.toBeNull()
  expect(err.status).toBe(502)
  expect(err.message).toContain("Server unavailable (HTTP 502 Bad Gateway)")
  expect(err.message).toContain("restarting or unreachable")
  expect(err.message).not.toBe("502 Bad Gateway")
})

test('inspector surfaces card readable name when error references spec.resources[i]', async ({ page }) => {
  await page.goto('/')

  await page.click('.node[data-id="work-queue"] .node-h')
  await expect(page.locator('#insp .insp-t .k')).toContainText('Queue')
  await page.click('#fseg button[data-f="all"]')

  // Route PUT /api/blueprint to return an error with coordinate spec.resources[0]
  await page.route('**/api/blueprint', async route => {
    if (route.request().method() === 'PUT') {
      await route.fulfill({
        status: 400,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'spec.resources[0].kind: "invalid" is not a valid Kind' })
      })
    } else {
      await route.continue()
    }
  })

  // Mutate a field in inspector to trigger PUT /api/blueprint
  const input = page.locator('#insp input[data-v="maxMessageSize"]')
  await input.fill('2048')
  await input.press('Tab')

  // Inspector warnbar must be visible and map spec.resources[0] to the card's readable name 'work-queue'
  const warnbar = page.locator('#insp .warnbar')
  await expect(warnbar).toBeVisible()
  await expect(warnbar).toContainText("work-queue")
  await expect(warnbar).toContainText("spec.resources[0]")
  await expect(warnbar).toContainText("not a valid Kind")
})

test('inspector replaces literal operation failed with meaningful explanation when operation fails', async ({ page }) => {
  await page.goto('/')

  await page.click('.node[data-id="work-queue"] .node-h')
  await expect(page.locator('#insp .insp-t .k')).toContainText('Queue')
  await page.click('#fseg button[data-f="all"]')

  // Route PUT /api/blueprint to return empty 500 error
  await page.route('**/api/blueprint', async route => {
    if (route.request().method() === 'PUT') {
      await route.fulfill({
        status: 500,
        statusText: 'Internal Server Error',
        contentType: 'text/plain',
        body: ''
      })
    } else {
      await route.continue()
    }
  })

  const input = page.locator('#insp input[data-v="maxMessageSize"]')
  await input.fill('4096')
  await input.press('Tab')

  const warnbar = page.locator('#insp .warnbar')
  await expect(warnbar).toBeVisible()
  await expect(warnbar).not.toHaveText('operation failed')
  await expect(warnbar).toContainText(/Server returned HTTP 500|Operation failed|unable to update field/)
})

test('inspector op() displays "Operation failed: unable to update field" when operation fails without error detail', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await expect(page.locator('#insp .insp-t .k')).toContainText('Queue')

  await page.evaluate(async () => {
    const insp = await import('./js/regions/inspector.js')
    await insp.op(async () => null, 'unable to update field')
  })

  const warnbar = page.locator('#insp .warnbar')
  await expect(warnbar).toBeVisible()
  await expect(warnbar).toHaveText('Operation failed: unable to update field')
})
