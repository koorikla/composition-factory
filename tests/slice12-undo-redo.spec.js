// Slice 12 — undo/redo: topbar buttons + Cmd/Ctrl+Z / Shift+Cmd/Ctrl+Z over a
// server-backed doc history; text inputs keep their native undo.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

async function serverValue(request, res, field) {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const r = doc.spec.resources.find(x => x.name === res)
  return r && r.fields[field] ? r.fields[field].value : undefined
}

test('undo and redo round-trip an edit through the server', async ({ page, request }) => {
  await page.goto('/')
  await expect(page.locator('#undoBtn')).toBeDisabled()
  await expect(page.locator('#redoBtn')).toBeDisabled()
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.click('#fseg button[data-f="all"]')
  const input = page.locator('#insp input[data-v="maxMessageSize"]')
  await input.fill('4096')
  await input.press('Tab')
  await expect(page.locator('#undoBtn')).toBeEnabled()
  await expect.poll(() => serverValue(request, 'work-queue', 'maxMessageSize')).toBe('4096')
  await page.click('#undoBtn')
  await expect(page.locator('#redoBtn')).toBeEnabled()
  await expect.poll(() => serverValue(request, 'work-queue', 'maxMessageSize')).toBe(undefined)
  await page.click('#redoBtn')
  await expect.poll(() => serverValue(request, 'work-queue', 'maxMessageSize')).toBe('4096')
})

test('Cmd/Ctrl+Z undoes on the canvas but never steals undo from a text field', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.click('#fseg button[data-f="all"]')
  const input = page.locator('#insp input[data-v="maxMessageSize"]')
  await input.fill('1234')
  await input.press('Tab')
  await expect.poll(() => serverValue(request, 'work-queue', 'maxMessageSize')).toBe('1234')
  await input.click()                       // focus back in the text field
  await page.keyboard.press('ControlOrMeta+z')
  await page.waitForTimeout(400)            // give a wrongly-triggered undo time to land
  expect(await serverValue(request, 'work-queue', 'maxMessageSize')).toBe('1234') // untouched
  await page.click('#cw')                   // canvas focus
  await page.keyboard.press('ControlOrMeta+z')
  await expect.poll(() => serverValue(request, 'work-queue', 'maxMessageSize')).toBe(undefined)
  await page.keyboard.press('Shift+ControlOrMeta+z')
  await expect.poll(() => serverValue(request, 'work-queue', 'maxMessageSize')).toBe('1234')
})
