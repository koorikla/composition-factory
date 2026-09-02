// Slice 32 — status.atProvider outputs display on cards like inputs do, and
// other objects can wire from them (object-depends-on-object).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('a card shows status outputs (atProvider) alongside its inputs', async ({ page }) => {
  await page.goto('/')
  const card = page.locator('.node[data-id="work-queue"]')
  await expect(card).toBeVisible()
  // status rows: at least the queue url/arn outputs, marked as outputs
  const out = card.locator('.port.status').first()
  await expect(out).toBeVisible()
  const label = await out.locator('.nm').textContent()
  expect(label).not.toMatch(/atProvider|^status\./)   // short labels, prefix in the title only
  const align = await out.evaluate(el => getComputedStyle(el).justifyContent)
  expect(align).toBe('flex-end')                       // outputs read on the right
})

test('the inspector can wire a field FROM another object status output', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h')
  await page.click('#fseg button[data-f="all"]')
  // switch a string field to wire mode and pick a status source from work-queue
  const row = page.locator('#insp .fld', { hasText: 'deduplicationScope' }).first()
  await row.locator('button[data-m="w"]').click()
  const sel = page.locator('#insp select[data-wire="deduplicationScope"]')
  const options = await sel.locator('option').allTextContents()
  expect(options.join('|')).toMatch(/work-queue.*status|status.*work-queue/i)
  const val = await sel.locator('option', { hasText: /work-queue/ }).first().getAttribute('value')
  await sel.selectOption(val)
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = doc.spec.resources.find(x => x.name === 'dead-letter')
    const f = r.fields.deduplicationScope
    return f && f.from ? f.from : 'unset'
  }).toMatch(/^resources\.work-queue\.status\./)
  // teal status wire renders on the canvas
  await expect(page.locator('#wires path.wire-status').first()).toBeVisible()
})
