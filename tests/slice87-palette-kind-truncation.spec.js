// Slice 87 — prevent palette kind name truncation at 1280x720 and make kinds easily distinguishable.
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('palette width is at least 220px at 1280x720 and canvas retains ample workspace', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 })
  await page.goto('/')

  const palette = page.locator('#region-palette')
  await expect(palette).toBeVisible()
  const pBox = await palette.boundingBox()
  expect(pBox.width).toBeGreaterThanOrEqual(220)

  const canvas = page.locator('#cw')
  await expect(canvas).toBeVisible()
  const cBox = await canvas.boundingBox()
  expect(cBox.width).toBeGreaterThan(650)
})

test('kind names carry rich tooltip title with <kind> · <apiVersion> (<provider>)', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 })
  await page.goto('/')

  const row = page.locator('#lrail .kind[data-kind="QueueRedriveAllowPolicy"]').first()
  await expect(row).toBeVisible()

  const nameEl = row.locator('.nm')
  await expect(nameEl).toBeVisible()

  const title = await nameEl.getAttribute('title')
  expect(title).toMatch(/^QueueRedriveAllowPolicy\s*·\s*\S+\s*\(\S+\)$/)

  const rowTitle = await row.getAttribute('title')
  expect(rowTitle).toMatch(/^QueueRedriveAllowPolicy\s*·\s*\S+\s*\(\S+\)$/)
})

test('kinds sharing a long common prefix are distinguishable at 1280x720', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 })
  await page.goto('/')

  const redrivePolicy = page.locator('#lrail .kind[data-kind="QueueRedrivePolicy"]').first()
  const redriveAllow = page.locator('#lrail .kind[data-kind="QueueRedriveAllowPolicy"]').first()

  await expect(redrivePolicy).toBeVisible()
  await expect(redriveAllow).toBeVisible()

  const title1 = await redrivePolicy.locator('.nm').getAttribute('title')
  const title2 = await redriveAllow.locator('.nm').getAttribute('title')

  expect(title1).not.toEqual(title2)
  expect(title1).toContain('QueueRedrivePolicy')
  expect(title2).toContain('QueueRedriveAllowPolicy')

  // Hovering shows the distinct full kind names
  await redriveAllow.hover()
  const prev = page.locator('#kind-preview')
  await expect(prev).toBeVisible({ timeout: 4000 })
  await expect(prev).toContainText('QueueRedriveAllowPolicy')
})
