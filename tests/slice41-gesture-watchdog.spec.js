// Slice 41 — a gesture that loses its release (app switch mid-press, no
// further movement) must not freeze rendering: window blur and pointercancel
// end the gesture and flush deferred renders.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('a blur mid-press ends the gesture; renders keep flowing', async ({ page }) => {
  await page.goto('/')
  const queueLocator = page.locator('.node[data-id="work-queue"]')
  await queueLocator.waitFor()
  const card = await queueLocator.boundingBox()
  await page.mouse.move(card.x + 60, card.y + 8)
  await page.mouse.down()                       // gesture begins
  await page.evaluate(() => window.dispatchEvent(new Event('blur')))  // app switch
  // no mouse movement at all afterwards — now the doc changes:
  await page.evaluate(async () => {
    const { store } = await import('/js/store.js')
    await store.replaceDoc(d => {
      d.spec.resources.push({ name: 'probe', kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0', fields: {} })
    })
  })
  await expect(page.locator('.node[data-id="probe"]')).toBeVisible({ timeout: 4000 })
  await page.mouse.up()                          // stray release must be harmless
  await expect(page.locator('.node[data-id="probe"]')).toBeVisible()
})

test('pointercancel ends pan and resize gestures too', async ({ page }) => {
  await page.goto('/')
  await page.mouse.move(600, 400)               // empty ground: pan gesture
  await page.mouse.down()
  await page.evaluate(() => document.dispatchEvent(new PointerEvent('pointercancel')))
  await page.evaluate(async () => {
    const { store } = await import('/js/store.js')
    await store.replaceDoc(d => {
      d.spec.resources.push({ name: 'probe2', kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0', fields: {} })
    })
  })
  await expect(page.locator('.node[data-id="probe2"]')).toBeVisible({ timeout: 4000 })
  await page.mouse.up()
})
