// Slice 56 — Floating & movable panels (Inspector, Editor) and mobile support
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('inspector can be floated, dragged around the canvas, and docked back in place', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 900 })
  await page.goto('/')

  const insp = page.locator('#region-inspector')
  const floatBtn = page.locator('#pane-float-r')
  await expect(floatBtn).toBeVisible()

  // Initially docked
  await expect(insp).not.toHaveClass(/floated-panel/)

  // Click float button
  await floatBtn.click()
  await expect(insp).toHaveClass(/floated-panel/)
  await expect(floatBtn).toHaveText('🔒')

  // Drag the inspector header to move it
  const header = insp.locator('.pane-h')
  const boxBefore = await insp.boundingBox()
  const hb = await header.boundingBox()

  await page.mouse.move(hb.x + 40, hb.y + hb.height / 2)
  await page.mouse.down()
  await page.mouse.move(hb.x - 120, hb.y + 80, { steps: 5 })
  await page.mouse.up()

  const boxAfter = await insp.boundingBox()
  expect(boxAfter.x).not.toEqual(boxBefore.x)

  // Dock back in place
  await floatBtn.click()
  await expect(insp).not.toHaveClass(/floated-panel/)
  await expect(floatBtn).toHaveText('⛶')
})

test('code editor / output drawer can be floated, dragged, minimized, and docked', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 900 })
  await page.goto('/')

  const drawer = page.locator('#region-output')
  const floatBtn = page.locator('#drawer-float-btn')
  const minBtn = page.locator('#drawer-min-btn')

  await expect(floatBtn).toBeVisible()
  await expect(drawer).not.toHaveClass(/floated-panel/)

  // Float editor
  await floatBtn.click()
  await expect(drawer).toHaveClass(/floated-panel/)
  await expect(floatBtn).toHaveText('🔒')

  // Drag the drawer header
  const handle = drawer.locator('#drawer-drag-handle')
  const boxBefore = await drawer.boundingBox()
  const hb = await handle.boundingBox()

  await page.mouse.move(hb.x + hb.width / 2, hb.y + hb.height / 2)
  await page.mouse.down()
  await page.mouse.move(hb.x + 80, hb.y - 60, { steps: 5 })
  await page.mouse.up()

  const boxAfter = await drawer.boundingBox()
  expect(boxAfter.y).not.toEqual(boxBefore.y)

  // Minimize floating editor
  await minBtn.click()
  await expect(drawer).toHaveClass(/minimized/)
  await expect(minBtn).toHaveText('▴')

  // Expand
  await minBtn.click()
  await expect(drawer).not.toHaveClass(/minimized/)

  // Dock back in place
  await floatBtn.click()
  await expect(drawer).not.toHaveClass(/floated-panel/)
  await expect(floatBtn).toHaveText('⛶')
})

test('mobile view provides responsive topbar, touch drawers and backdrop closing', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 667 })
  await page.goto('/')

  const toggleDrawer = page.locator('#pane-toggle-drawer')
  const toggleL = page.locator('#pane-toggle-l')
  const drawer = page.locator('#region-output')
  const palette = page.locator('#region-palette')
  const backdrop = page.locator('#drawerBackdrop')

  await expect(toggleDrawer).toBeVisible()
  await expect(toggleL).toBeVisible()
  await expect(backdrop).toBeHidden()

  // Open generated YAML drawer
  await toggleDrawer.click()
  await expect(drawer).toBeVisible()
  await expect(backdrop).toBeVisible()

  // Tapping backdrop at top of screen closes the drawer
  await backdrop.click({ position: { x: 100, y: 100 } })
  await expect(drawer).toBeHidden()
  await expect(backdrop).toBeHidden()

  // Open palette drawer
  await toggleL.click()
  await expect(palette).toBeVisible()
  await expect(backdrop).toBeVisible()

  // Tapping backdrop closes palette
  await backdrop.click({ position: { x: 340, y: 100 } })
  await expect(palette).toBeHidden()
  await expect(backdrop).toBeHidden()
})
