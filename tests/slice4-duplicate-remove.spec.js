// Slice 4 — duplicate and remove canvas objects: Cmd/Ctrl+C/V deep-copies a
// resource (unique name, values and wires carried), Delete removes it with a
// confirm that lists wired fields; text editing is never hijacked.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('copy/paste duplicates a queue with its fields and wires, persisted', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.keyboard.press('ControlOrMeta+c')
  await page.keyboard.press('ControlOrMeta+v')
  await expect(page.locator('.node[data-id="work-queue-2"]')).toBeVisible()
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const dupe = doc.spec.resources.find(r => r.name === 'work-queue-2')
  expect(dupe).toBeTruthy()
  expect(dupe.kind).toBe('Queue')
  expect(dupe.fields.region).toMatchObject({ from: 'params.region' })
  expect(dupe.fields.messageRetentionSeconds).toMatchObject({ from: 'params.retention' })
  // the duplicated wire renders: region now fans out to 3 rows
  expect(await page.locator('svg path[class*="wire"]').count()).toBeGreaterThanOrEqual(3)
})

test('Delete removes a resource after a confirm that names its wired fields', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="dead-letter"] .node-h')
  let confirmText = ''
  page.on('dialog', d => { confirmText = d.message(); d.accept() })
  await page.keyboard.press('Delete')
  await expect(page.locator('.node[data-id="dead-letter"]')).toHaveCount(0)
  expect(confirmText).toContain('region')  // the wired field, named in the confirm
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.map(r => r.name)).toEqual(['work-queue'])
})

test('Delete and copy keys inside a text field never touch the canvas', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.click('#fseg button[data-f="all"]')
  const input = page.locator('#insp input').first()
  await input.click()
  await input.type('x')
  await page.keyboard.press('Backspace')
  await page.keyboard.press('ControlOrMeta+c')
  await page.keyboard.press('ControlOrMeta+v')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  await expect(page.locator('.node[data-id="work-queue-2"]')).toHaveCount(0)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.length).toBe(2)
})
