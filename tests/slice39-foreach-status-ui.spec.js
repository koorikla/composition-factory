// Slice 39 — the for-each control offers observed counts: an unlooped
// sibling's integer/number status leaf can drive the fan-out.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('for-each can bind to a sibling status number and persists', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h')
  const sel = page.locator('#insp select[data-foreach]')
  await expect(sel.locator('option', { hasText: /work-queue\.status\./ }).first()).toBeAttached()
  await sel.selectOption('resources.work-queue.status.atProvider.maxMessageSize')
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    return doc.spec.resources.find(r => r.name === 'dead-letter').forEach || 'unset'
  }).toBe('resources.work-queue.status.atProvider.maxMessageSize')
  await expect(page.locator('.node[data-id="dead-letter"]')).toContainText(/for each/i)
})
