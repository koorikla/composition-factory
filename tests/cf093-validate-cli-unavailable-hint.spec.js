const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('Validate in container deployment reports unavailable with actionable explanation, never prescribing curl into container', async ({ page }) => {
  // Simulate container environment from /api/version
  await page.route('**/api/version', async route => {
    const response = await route.fetch()
    const json = await response.json()
    json.container = true
    await route.fulfill({ json })
  })

  // Simulate missing crossplane CLI on PATH
  await page.route('**/api/render', async route => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: false,
          resources: 0,
          error: '',
          unavailable: 'crossplane CLI not found on PATH: exec: "crossplane": executable file not found in $PATH'
        })
      })
    } else {
      await route.continue()
    }
  })

  await page.goto('/')

  // Click Validate
  const valBtn = page.locator('#validateBtn')
  await expect(valBtn).toBeEnabled()
  await valBtn.click()

  // Status chip should indicate unavailable
  const chip = page.locator('#valid')
  await expect(chip).toHaveText('validation check unavailable')

  // The warning alert/drawer should be displayed
  const warn = page.locator('#render-warn, #render-warn-banner, .warnbar').first()
  await expect(warn).toBeVisible()
  const warnText = await warn.textContent()

  // Must NOT prescribe raw curl | sh inside the container
  expect(warnText).not.toMatch(/curl -sL https:\/\/raw\.githubusercontent\.com\/crossplane\/crossplane\/master\/install\.sh/i)

  // Must explain that validation is unavailable in container deployments and why
  expect(warnText).toMatch(/unavailable in container deployments/i)
  expect(warnText).toMatch(/does not bundle (the )?Crossplane CLI/i)
  expect(warnText).toMatch(/host/i)
})

test('Validate in host deployment provides actionable host installation commands', async ({ page }) => {
  // Simulate non-container (host) environment
  await page.route('**/api/version', async route => {
    const response = await route.fetch()
    const json = await response.json()
    json.container = false
    await route.fulfill({ json })
  })

  await page.route('**/api/render', async route => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ok: false,
          resources: 0,
          error: '',
          unavailable: 'crossplane CLI not found on PATH: exec: "crossplane": executable file not found in $PATH'
        })
      })
    } else {
      await route.continue()
    }
  })

  await page.goto('/')

  const valBtn = page.locator('#validateBtn')
  await expect(valBtn).toBeEnabled()
  await valBtn.click()

  const warn = page.locator('#render-warn, #render-warn-banner, .warnbar').first()
  await expect(warn).toBeVisible()
  const warnText = await warn.textContent()

  // Must NOT prescribe the old unpinned master script
  expect(warnText).not.toMatch(/raw\.githubusercontent\.com\/crossplane\/crossplane\/master\/install\.sh/i)

  // Must provide proper host installation guidance
  expect(warnText).toMatch(/crossplane CLI on host/i)
  expect(warnText).toMatch(/brew install crossplane-cli|cli\.crossplane\.io/i)
})
