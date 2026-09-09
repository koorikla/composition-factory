// Slice 91 — output drawer vertical space at 1280x720 (CF-064).
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
const fs = require('fs')

guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('output drawer is compact (<= 210px) and canvas retains vertical authoring space (>= 460px) at 1280x720', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 })
  await page.goto('/')

  const drawer = page.locator('#region-output')
  await expect(drawer).toBeVisible()

  const drawerBox = await drawer.boundingBox()
  expect(drawerBox.height).toBeLessThanOrEqual(210)

  const canvas = page.locator('#cw')
  await expect(canvas).toBeVisible()

  const canvasBox = await canvas.boundingBox()
  expect(canvasBox.height).toBeGreaterThanOrEqual(460)

  const palette = page.locator('#region-palette')
  await expect(palette).toBeVisible()

  const paletteBox = await palette.boundingBox()
  expect(paletteBox.height).toBeGreaterThanOrEqual(460)

  // Verify code area inside drawer is visible and properly sized
  const code = page.locator('#code')
  await expect(code).toBeVisible()
  const codeBox = await code.boundingBox()
  expect(codeBox.height).toBeGreaterThan(100)
})

test('drawer expands to compact height (<= 210px) when re-expanded after collapse', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 })
  await page.goto('/')

  const drawer = page.locator('#region-output')
  const minBtn = page.locator('#drawer-min-btn')
  await expect(minBtn).toBeVisible()

  // Collapse drawer
  await minBtn.click()
  await expect(drawer).toHaveAttribute('data-collapsed', '')

  // Re-expand drawer
  await minBtn.click()
  await expect(drawer).not.toHaveAttribute('data-collapsed', '')

  const drawerBox = await drawer.boundingBox()
  expect(drawerBox.height).toBeLessThanOrEqual(210)

  const canvas = page.locator('#cw')
  const canvasBox = await canvas.boundingBox()
  expect(canvasBox.height).toBeGreaterThanOrEqual(460)
})

test('token definitions in proto.css and canvas-prototype.html remain in sync (zero token drift)', async () => {
  function extractTokens(content) {
    const vars = []
    const re = /(--[\w-]+)\s*:\s*([^;}\n]+)/g
    let m
    while ((m = re.exec(content)) !== null) {
      vars.push(`${m[1]}:${m[2].trim()}`)
    }
    return vars.slice(0, 75)
  }
  const protoTokens = extractTokens(fs.readFileSync('web-proto/css/proto.css', 'utf8'))
  const canvasTokens = extractTokens(fs.readFileSync('docs/design/canvas-prototype.html', 'utf8'))
  expect(protoTokens).toEqual(canvasTokens)
})
