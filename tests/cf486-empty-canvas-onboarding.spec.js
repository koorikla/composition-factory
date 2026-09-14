// tests/cf486-empty-canvas-onboarding.spec.js
// CF-486 — The empty-canvas onboarding card runs under the Inspector rail and its instructions are clipped
// Contract: the empty-canvas hint must lay out inside the canvas region at every viewport width
// the app supports, so that no part of its text is covered by the Inspector or the palette rail.

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

const VIEWPORTS = [
  { width: 1600, height: 1000 },
  { width: 1440, height: 900 },
  { width: 1280, height: 800 },
  { width: 1024, height: 768 },
]

for (const vp of VIEWPORTS) {
  test(`empty-canvas hint lays out fully inside canvas region at ${vp.width}x${vp.height}`, async ({ page }) => {
    await page.setViewportSize(vp)
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

    const inspector = page.locator('#region-inspector')
    await expect(inspector).toBeVisible()

    const palette = page.locator('#region-palette')
    await expect(palette).toBeVisible()

    const cw = page.locator('#cw')
    await expect(cw).toBeVisible()

    const cardBox = await emptyState.boundingBox()
    const inspBox = await inspector.boundingBox()
    const palBox = await palette.boundingBox()
    const cwBox = await cw.boundingBox()

    expect(cardBox).not.toBeNull()
    expect(inspBox).not.toBeNull()
    expect(palBox).not.toBeNull()
    expect(cwBox).not.toBeNull()

    // Bounding assertions:
    // 1. Right edge of the card must not go under the inspector rail
    expect(cardBox.x + cardBox.width).toBeLessThanOrEqual(inspBox.x)

    // 2. Left edge of the card must be at or to the right of the palette rail's right edge
    expect(cardBox.x).toBeGreaterThanOrEqual(palBox.x + palBox.width)

    // 3. Card must stay inside canvas wrap (#cw) boundaries horizontally
    expect(cardBox.x).toBeGreaterThanOrEqual(cwBox.x)
    expect(cardBox.x + cardBox.width).toBeLessThanOrEqual(cwBox.x + cwBox.width)
  })
}
