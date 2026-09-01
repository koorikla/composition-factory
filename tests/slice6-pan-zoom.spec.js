// Slice 6 — canvas pan/zoom: ctrl/cmd+wheel zooms to the cursor, plain wheel
// pans, +/- and reset controls, wires and drops track the transform.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

function transformOf(page) {
  return page.evaluate(() => {
    const t = getComputedStyle(document.getElementById('canvas')).transform
    if (t === 'none') return { scale: 1, x: 0, y: 0 }
    const m = new DOMMatrixReadOnly(t)
    return { scale: m.a, x: m.e, y: m.f }
  })
}

test('wheel zooms to the cursor, shift+wheel pans, reset restores', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  const cw = page.locator('#cw')
  await cw.hover()
  await page.mouse.wheel(0, -240)                 // plain wheel: zoom in
  let t = await transformOf(page)
  expect(t.scale).toBeGreaterThan(1.05)
  await page.keyboard.down('Shift')
  await page.mouse.wheel(0, 120)                  // shift+wheel: pan
  await page.keyboard.up('Shift')
  const t2 = await transformOf(page)
  expect(t2.scale).toBeCloseTo(t.scale, 2)
  expect(Math.abs(t2.x - t.x) + Math.abs(t2.y - t.y)).toBeGreaterThan(0)
  await page.click('#zoom-reset')
  t = await transformOf(page)
  expect(t.scale).toBeCloseTo(1, 2)
  expect(t.x).toBeCloseTo(0, 1)
  expect(t.y).toBeCloseTo(0, 1)
})

test('zoom buttons work and wires stay glued to their ports', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  await page.click('#zoom-in')
  await page.click('#zoom-in')
  const t = await transformOf(page)
  expect(t.scale).toBeGreaterThan(1.1)
  // the first wire's start must sit on a port dot (screen-space, ±3px)
  const glue = await page.evaluate(() => {
    const cw = document.getElementById('cw').getBoundingClientRect()
    const path = document.querySelector('#wires path')
    if (!path) return 'no wire'
    const m = /M([\d.]+),([\d.]+)/.exec(path.getAttribute('d'))
    const dots = [...document.querySelectorAll('.port .d')].map(el => {
      const r = el.getBoundingClientRect()
      return { x: r.left - cw.left + r.width / 2, y: r.top - cw.top + r.height / 2 }
    })
    const px = +m[1], py = +m[2]
    return Math.min(...dots.map(d => Math.hypot(d.x - px, d.y - py)))
  })
  expect(glue).toBeLessThan(3)
})

test('a drop while zoomed lands where the cursor points', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#zoom-out')                    // 0.8-ish: transform != identity
  const at = { x: 420, y: 300 }
  await page.evaluate(({ at }) => {
    const cw = document.getElementById('cw')
    const r = cw.getBoundingClientRect()
    const row = document.querySelector('#lrail .kind[data-kind="QueuePolicy"]')
    const dt = new DataTransfer()
    row.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }))
    const ev = { clientX: r.left + at.x, clientY: r.top + at.y }
    cw.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt, ...ev }))
    cw.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt, ...ev }))
  }, { at })
  const node = page.locator('.node[data-id="queue-policy"]')
  await expect(node).toBeVisible()
  const near = await page.evaluate(({ at }) => {
    const cw = document.getElementById('cw').getBoundingClientRect()
    const r = document.querySelector('.node[data-id="queue-policy"]').getBoundingClientRect()
    return Math.hypot((r.left - cw.left) - at.x + 90 * 0.8, (r.top - cw.top) - at.y + 16 * 0.8)
  }, { at })
  expect(near).toBeLessThan(40)
})
