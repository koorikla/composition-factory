// tests/cf465-palette-background-providers.spec.js
// CF-465 — Canvas UI polls and reflects background provider downloads,
// dynamically populating kinds palette without manual reload.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

const DOWNLOADING_REF = 'xpkg.upbound.io/crossplane-contrib/provider-aws-sqs:v1.16.0'

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('canvas visually displays provider download progress and reactively polls until kinds populate', async ({ page }) => {
  let providerPollCount = 0
  let kindsPollCount = 0

  await page.route('**/api/providers', async (route) => {
    providerPollCount++
    if (providerPollCount < 3) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          providers: [
            { ref: DOWNLOADING_REF, status: 'loading' }
          ]
        })
      })
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          providers: [
            { ref: DOWNLOADING_REF, digest: 'sha256:ea55e99387b7', kinds: 1 }
          ]
        })
      })
    }
  })

  await page.route('**/api/kinds*', async (route) => {
    kindsPollCount++
    if (providerPollCount < 3) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ kinds: [] })
      })
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          kinds: [
            {
              kind: 'Queue',
              apiVersion: 'sqs.aws.upbound.io/v1beta1',
              group: 'sqs.aws.upbound.io',
              provider: DOWNLOADING_REF,
              scope: 'Namespaced',
              namespaced: true,
              required: 0
            }
          ]
        })
      })
    }
  })

  await page.goto('/')

  // Initial page loads immediately without blocking
  await expect(page.locator('#region-palette')).toBeVisible()

  // Visually displays download progress instead of "No kinds available"
  const rail = page.locator('#lrail')
  await expect(rail).not.toContainText('No kinds available')
  await expect(rail).toContainText(/downloading/i)

  // Reactively polls and populates kinds dynamically without manual reload
  const kindRow = page.locator('.kind[data-kind="Queue"]')
  await expect(kindRow).toBeVisible({ timeout: 10000 })
  expect(providerPollCount).toBeGreaterThanOrEqual(3)

  // Once download completes, download progress clears and kinds remain visible
  await expect(rail).not.toContainText(/downloading/i)
  await expect(kindRow).toBeVisible()
})

test('sources tab visually indicates downloading status for loading providers', async ({ page }) => {
  await page.route('**/api/providers', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        providers: [
          { ref: DOWNLOADING_REF, status: 'loading' }
        ]
      })
    })
  })

  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const row = page.locator('#lrail .src-row[data-ref="' + DOWNLOADING_REF + '"]')
  await expect(row).toBeVisible()
  await expect(row).toHaveAttribute('data-state', 'loading')
  await expect(row).toContainText(/downloading/i)
})

test('unreferenced providers like S3 are strictly deferred and not loaded at startup', async ({ request }) => {
  const r = await request.get(ENGINE + '/api/providers')
  expect(r.ok()).toBeTruthy()
  const providers = (await r.json()).providers
  const s3 = providers.find((p) => (p.ref || '').includes('provider-aws-s3'))
  expect(s3).toBeUndefined()
})
