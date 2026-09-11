// Slice 31 — the opening example demonstrates reusable tags: a cf.tags
// template applied by convention to every resource that doesn't set tags,
// with dead-letter's explicit raw tags overriding it.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors, dropKind } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
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
  // a Queue has a top-level tags leaf, so the convention applies to it
  await dropKind(page, 'Queue', '*=.m.', 180, 300)
  await expect(page.locator('.node[data-id="queue"]')).toBeVisible()
  // the convention reaches the new resource with zero configuration: the
  // define holds the tag literal ONCE; each covered resource emits a CALL
  const code = page.locator('#code')
  await expect(code).toContainText('setResourceNameAnnotation "queue"', { timeout: 8000 })
  const body = await code.textContent()
  const calls = (body.match(/include "cf\.tags"/g) || []).length
  expect(calls).toBeGreaterThanOrEqual(2)  // work-queue + the dropped queue
})
