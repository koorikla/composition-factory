// Slice 31 — the opening example demonstrates reusable tags: a cf.tags
// template applied by convention to every resource that doesn't set tags,
// with dead-letter's explicit raw tags overriding it.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('the composition carries the convention define and both tag behaviors', async ({ page }) => {
  await page.goto('/')
  const code = page.locator('#code')
  await expect(code).toContainText('define "cf.tags"', { timeout: 8000 })
  await expect(code).toContainText('managed-by: crossplane')   // work-queue inherits
  await expect(code).toContainText('purpose')                  // dead-letter overrides (raw)
})

test('a freshly dropped resource inherits the reusable tags too', async ({ page }) => {
  await page.goto('/')
  await page.evaluate(() => {
    // a Queue has a top-level tags leaf, so the convention applies to it
    const row = document.querySelector('#lrail .kind[data-kind="Queue"][data-av*=".m."]')
    const cw = document.getElementById('cw')
    const r = cw.getBoundingClientRect()
    const dt = new DataTransfer()
    row.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }))
    const at = { clientX: r.left + 180, clientY: r.top + 300 }
    cw.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt, ...at }))
    cw.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt, ...at }))
  })
  await expect(page.locator('.node[data-id="queue"]')).toBeVisible()
  // the convention reaches the new resource with zero configuration: the
  // define holds the tag literal ONCE; each covered resource emits a CALL
  const code = page.locator('#code')
  await expect(code).toContainText('setResourceNameAnnotation "queue"', { timeout: 8000 })
  const body = await code.textContent()
  const calls = (body.match(/include "cf\.tags"/g) || []).length
  expect(calls).toBeGreaterThanOrEqual(2)  // work-queue + the dropped queue
})
