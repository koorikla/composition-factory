// Slice 19 — visible card actions: delete and duplicate must work by mouse
// alone, even while focus sits in an inspector field (the keyboard-only path
// is guarded there by design, which read as "can't delete" — user report).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('a selected card shows delete and duplicate buttons that work', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h')
  const del = page.locator('.node[data-id="dead-letter"] [data-act="delete"]')
  const dup = page.locator('.node[data-id="dead-letter"] [data-act="duplicate"]')
  await expect(del).toBeVisible()
  await expect(dup).toBeVisible()
  page.on('dialog', d => d.accept())
  await del.click()
  await expect(page.locator('.node[data-id="dead-letter"]')).toHaveCount(0)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.map(r => r.name)).toEqual(['work-queue'])
})

test('delete works by mouse even while an inspector field has focus', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.click('#fseg button[data-f="all"]')
  await page.locator('#insp input').first().click()   // focus trap for the key path
  page.on('dialog', d => d.accept())
  await page.click('.node[data-id="work-queue"] [data-act="delete"]')
  await expect(page.locator('.node[data-id="work-queue"]')).toHaveCount(0)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.map(r => r.name)).toEqual(['dead-letter'])
})
