// Slice 37 — annotation wires render and are authorable: the IRSA arn →
// ServiceAccount annotation must be VISIBLE (teal, like status wires) and
// editable from the inspector's new annotations section.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')



test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('an annotation wire from a status output renders on the canvas', async ({ page, request }) => {
  // wire dead-letter's arn into work-queue's annotation (same mechanism as IRSA)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const wq = doc.spec.resources.find(r => r.name === 'work-queue')
  wq.annotations = { 'sparky.ee/dlq-arn': { from: 'resources.dead-letter.status.atProvider.arn' } }
  const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(put.ok()).toBeTruthy()
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toContainText('sparky.ee/dlq-arn')
  await expect(page.locator('#wires path.wire-status')).toHaveCount(1)
})

test('the inspector lists annotations and adds one', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
  await expect(sec).toBeVisible()
  await sec.locator('input[data-ann-key]').fill('sparky.ee/team')
  await sec.locator('input[data-ann-value]').fill('platform')
  await sec.locator('button[data-ann-add]').click()
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = doc.spec.resources.find(x => x.name === 'work-queue')
    return r.annotations && r.annotations['sparky.ee/team']
      ? r.annotations['sparky.ee/team'].value : 'missing'
  }).toBe('platform')
})
