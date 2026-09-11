// tests/cf145-empty-canvas-tab-links.spec.js
// CF-145 — The empty-canvas hint renders SOURCES and KINDS in bold like links, and clicking them does nothing.
// Contract: clicking "KINDS" and "SOURCES" (both in the title and in the numbered steps)
// switches the palette tab to 'kinds' and 'sources' respectively, with interactive pointer/hover styling.

const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')

guardPageErrors()

const EMPTY_BLUEPRINT = {
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
}

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const res = await request.put(ENGINE + '/api/blueprint', {
    data: EMPTY_BLUEPRINT,
  })
  expect(res.ok()).toBeTruthy()
})

test('clicking KINDS and SOURCES in empty canvas hint switches active palette tab', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('cf:empty-start-offered', '1')
  })
  await page.goto('/')

  // Dismiss examples modal if shown
  const overlay = page.locator('#examplesOverlay')
  if (await overlay.isVisible()) {
    await page.keyboard.press('Escape')
    await expect(overlay).toBeHidden()
  }

  const emptyState = page.locator('#canvas-empty-state')
  await expect(emptyState).toBeVisible()

  const kindsTab = page.locator('#rtabs button[data-r="kinds"]')
  const srcTab = page.locator('#rtabs button[data-r="src"]')

  // Initially Kinds tab is active
  await expect(kindsTab).toHaveAttribute('aria-pressed', 'true')
  await expect(srcTab).toHaveAttribute('aria-pressed', 'false')

  // 1. Click SOURCES in title switches to Sources tab
  const titleSources = emptyState.locator('.canvas-empty-title [data-tab-switch="sources"]')
  await expect(titleSources).toBeVisible()
  await titleSources.click()
  await expect(srcTab).toHaveAttribute('aria-pressed', 'true')
  await expect(kindsTab).toHaveAttribute('aria-pressed', 'false')

  // 2. Click KINDS in title switches to Kinds tab
  const titleKinds = emptyState.locator('.canvas-empty-title [data-tab-switch="kinds"]')
  await expect(titleKinds).toBeVisible()
  await titleKinds.click()
  await expect(kindsTab).toHaveAttribute('aria-pressed', 'true')
  await expect(srcTab).toHaveAttribute('aria-pressed', 'false')

  // 3. Click SOURCES in step 2 switches to Sources tab
  const stepSources = emptyState.locator('.canvas-empty-step [data-tab-switch="sources"]')
  await expect(stepSources).toBeVisible()
  await stepSources.click()
  await expect(srcTab).toHaveAttribute('aria-pressed', 'true')
  await expect(kindsTab).toHaveAttribute('aria-pressed', 'false')

  // 4. Click KINDS in step 1 switches to Kinds tab
  const stepKinds = emptyState.locator('.canvas-empty-step [data-tab-switch="kinds"]')
  await expect(stepKinds).toBeVisible()
  await stepKinds.click()
  await expect(kindsTab).toHaveAttribute('aria-pressed', 'true')
  await expect(srcTab).toHaveAttribute('aria-pressed', 'false')
})

test('empty-canvas tab links have cursor pointer and role button', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('cf:empty-start-offered', '1')
  })
  await page.goto('/')

  const overlay = page.locator('#examplesOverlay')
  if (await overlay.isVisible()) {
    await page.keyboard.press('Escape')
    await expect(overlay).toBeHidden()
  }

  const emptyState = page.locator('#canvas-empty-state')
  await expect(emptyState).toBeVisible()

  const links = emptyState.locator('.canvas-empty-tab-link')
  await expect(links).toHaveCount(4)

  for (let i = 0; i < 4; i++) {
    const link = links.nth(i)
    await expect(link).toHaveAttribute('role', 'button')
    await expect(link).toHaveAttribute('tabindex', '0')
    const cursor = await link.evaluate((el) => window.getComputedStyle(el).cursor)
    expect(cursor).toBe('pointer')
  }
})

test('keyboard Enter on empty-canvas tab link switches active tab', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('cf:empty-start-offered', '1')
  })
  await page.goto('/')

  const overlay = page.locator('#examplesOverlay')
  if (await overlay.isVisible()) {
    await page.keyboard.press('Escape')
    await expect(overlay).toBeHidden()
  }

  const emptyState = page.locator('#canvas-empty-state')
  await expect(emptyState).toBeVisible()

  const kindsTab = page.locator('#rtabs button[data-r="kinds"]')
  const srcTab = page.locator('#rtabs button[data-r="src"]')

  await expect(kindsTab).toHaveAttribute('aria-pressed', 'true')

  const stepSources = emptyState.locator('.canvas-empty-step [data-tab-switch="sources"]')
  await stepSources.focus()
  await page.keyboard.press('Enter')
  await expect(srcTab).toHaveAttribute('aria-pressed', 'true')
  await expect(kindsTab).toHaveAttribute('aria-pressed', 'false')

  const stepKinds = emptyState.locator('.canvas-empty-step [data-tab-switch="kinds"]')
  await stepKinds.focus()
  await page.keyboard.press('Enter')
  await expect(kindsTab).toHaveAttribute('aria-pressed', 'true')
  await expect(srcTab).toHaveAttribute('aria-pressed', 'false')
})

