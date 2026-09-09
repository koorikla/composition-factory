const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors, canvasSettled } = require('./helpers')
guardPageErrors()

test.describe('Wire and parameter interactive hit targets (CF-068)', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request)
  })

  test('wire-hit path exists in #wires SVG with stroke-width >= 12', async ({ page }) => {
    await page.goto('/')
    await canvasSettled(page)

    const wires = page.locator('#wires path.wire-path')
    const count = await wires.count()
    expect(count).toBeGreaterThanOrEqual(1)

    const wireHits = page.locator('#wires path.wire-hit')
    await expect(wireHits).toHaveCount(count)

    for (let i = 0; i < count; i++) {
      const hit = wireHits.nth(i)
      const strokeWidthAttr = await hit.getAttribute('stroke-width')
      const widthVal = parseFloat(strokeWidthAttr || '0')
      expect(widthVal).toBeGreaterThanOrEqual(12)

      const pointerEvents = await hit.evaluate(el => window.getComputedStyle(el).pointerEvents)
      expect(pointerEvents).toBe('stroke')

      const dataIdx = await hit.getAttribute('data-wire-idx')
      expect(dataIdx).toBe(String(i))
    }
  })

  test('clicking slightly off the center of the drawn wire selects the wire', async ({ page }) => {
    await page.goto('/')
    await canvasSettled(page)

    // Find a coordinate 5px off from the wire path where .wire-hit is hit
    const point = await page.evaluate(() => {
      const p = document.querySelector('svg.wires path.wire-path')
      if (!p) return null
      const ctm = p.getScreenCTM()
      const len = p.getTotalLength()
      if (!ctm || !len) return null

      // Sample midpoint
      const pt = p.getPointAtLength(len * 0.5).matrixTransform(ctm)
      const x = Math.round(pt.x)
      const y = Math.round(pt.y)

      // Test 5px offset above or below
      for (const dy of [5, -5, 6, -6]) {
        const el = document.elementFromPoint(x, y + dy)
        if (el && el.classList && el.classList.contains('wire-hit')) {
          return { x, y: y + dy }
        }
      }
      // Fallback to 5px offset to test click selection failure
      return { x, y: y + 5 }
    })

    expect(point).not.toBeNull()

    // Initially no wire selected
    await expect(page.locator('svg.wires path.wire-path.wire-selected')).toHaveCount(0)

    // Click at the offset point (5px away from stroke center)
    await page.mouse.click(point.x, point.y)

    // The wire must be selected
    await expect(page.locator('svg.wires path.wire-path.wire-selected')).toHaveCount(1)
    await expect(page.locator('svg.wires .wire-del-btn')).toBeVisible()
  })

  test('port dot has extended hit target (pseudo-element >= 16px) while preserving 7x7 dot size', async ({ page }) => {
    await page.goto('/')
    await canvasSettled(page)

    const dot = page.locator('.port .d').first()
    await expect(dot).toBeVisible()

    // Dot visual size is 7x7 (plus borders <= 10px)
    const dotBox = await dot.boundingBox()
    expect(dotBox).not.toBeNull()
    expect(dotBox.width).toBeLessThanOrEqual(10)
    expect(dotBox.height).toBeLessThanOrEqual(10)

    // Pseudo-element ::before provides >= 16px hit target
    const pseudoMetrics = await dot.evaluate(el => {
      const before = window.getComputedStyle(el, '::before')
      const w = parseFloat(before.width) || 0
      const h = parseFloat(before.height) || 0
      return { width: w, height: h, content: before.content }
    })

    expect(pseudoMetrics.content).not.toBe('none')
    expect(pseudoMetrics.width).toBeGreaterThanOrEqual(16)
    expect(pseudoMetrics.height).toBeGreaterThanOrEqual(16)
  })

  test('right-clicking slightly off the center opens wire context menu', async ({ page }) => {
    await page.goto('/')
    await canvasSettled(page)

    const point = await page.evaluate(() => {
      const p = document.querySelector('svg.wires path.wire-path')
      if (!p) return null
      const ctm = p.getScreenCTM()
      const len = p.getTotalLength()
      if (!ctm || !len) return null

      const pt = p.getPointAtLength(len * 0.5).matrixTransform(ctm)
      const x = Math.round(pt.x)
      const y = Math.round(pt.y)

      for (const dy of [5, -5, 6, -6]) {
        const el = document.elementFromPoint(x, y + dy)
        if (el && el.classList && el.classList.contains('wire-hit')) {
          return { x, y: y + dy }
        }
      }
      return { x, y: y + 5 }
    })

    expect(point).not.toBeNull()

    // Right-click on the wide hit target
    await page.mouse.click(point.x, point.y, { button: 'right' })

    const ctxMenu = page.locator('#ctx-menu')
    await expect(ctxMenu).toBeVisible()
    await expect(ctxMenu).toContainText('Delete wire')
  })
})
