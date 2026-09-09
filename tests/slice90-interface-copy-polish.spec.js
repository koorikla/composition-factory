const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('CF-072: empty canvas copy states 16 native Kubernetes kinds', async ({ request, page }) => {
  const res = await request.put('/api/blueprint', {
    data: {
      apiVersion: 'factory.crossplane.io/v1alpha1',
      kind: 'Blueprint',
      metadata: { name: 'blank' },
      spec: {
        xrd: {
          group: 'platform.sparky.ee',
          kind: 'XBlank',
          plural: 'xblanks',
          version: 'v1alpha1',
          scope: 'Namespaced',
          parameters: {},
        },
        resources: [],
      },
    },
  })
  expect(res.ok()).toBeTruthy()

  await page.goto('/')

  const emptyState = page.locator('#canvas-empty-state')
  await expect(emptyState).toBeVisible()
  await expect(emptyState).toContainText('16 native Kubernetes kinds ready without providers')
  await expect(emptyState).not.toContainText('14 native Kubernetes kinds')
})

test('CF-072: palette empty states distinguish between search query and empty catalogue', async ({ page }) => {
  await page.goto('/')

  // Switch to KINDS rail
  await page.click('#rtabs button[data-r="kinds"]')

  // Search for a non-existent kind
  const search = page.locator('#psearch')
  await search.fill('zzzznonexistentkind')

  // Empty state should indicate no search matches
  const emptyEl = page.locator('#lrail .empty')
  await expect(emptyEl).toBeVisible()
  await expect(emptyEl).toHaveText('No kinds match search query.')

  // When kinds list is empty without a search query, it should say "No kinds available."
  await page.evaluate(() => {
    const searchInput = document.getElementById('psearch')
    if (searchInput) searchInput.value = ''
  })
  // Intercept /api/kinds with empty kinds list and reload kinds
  await page.route('**/api/kinds*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ kinds: [] }),
    })
  })
  await page.evaluate(async () => {
    // trigger reload
    const searchInput = document.getElementById('psearch')
    if (searchInput) {
      searchInput.dispatchEvent(new Event('input', { bubbles: true }))
    }
  })

  await expect(emptyEl).toBeVisible()
  await expect(emptyEl).toContainText('No kinds available.')
  await expect(emptyEl).toContainText('cf provider add')
})

test('CF-072: missing schema provides actionable provider add guidance', async ({ page }) => {
  await page.goto('/')

  // Intercept /api/kinds to simulate missing schema for Queue
  await page.route('**/api/kinds*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ kinds: [] }),
    })
  })

  // Click the work-queue card on canvas to open inspector
  const card = page.locator('.node[data-id="work-queue"]')
  await card.click()

  // Inspector should show missing schema empty state with actionable advice
  const emptyState = page.locator('#insp .empty')
  await expect(emptyState).toBeVisible()
  await expect(emptyState).toContainText('No schema found for kind Queue')
  await expect(emptyState).toContainText('cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0')
})
