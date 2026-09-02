// Slice 47 — an Import button loads a dsl .yaml from disk: through the
// server's YAML gate, doc replaced, undoable; bad files show verbatim errors.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()
const path = require('path')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('importing a blueprint yaml replaces the doc and is undoable', async ({ page, request }) => {
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  const [chooser] = await Promise.all([
    page.waitForEvent('filechooser'),
    page.click('#importBtn'),
  ])
  await chooser.setFiles(path.resolve('testdata/xqueue.cf.yaml'))
  await expect(page.locator('.node[data-id="main-queue"]')).toBeVisible({ timeout: 8000 })
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.metadata.name).toBe('xqueue')
  await page.click('#undoBtn')                       // import is one undo step
  await expect.poll(async () => {
    const d = await (await request.get(ENGINE + '/api/blueprint')).json()
    return d.metadata.name
  }).toBe('xnotify')
})

test('an invalid file surfaces the server error verbatim; doc untouched', async ({ page, request }) => {
  await page.goto('/')
  const bad = path.resolve('.testrun/bad-import.yaml')
  require('fs').mkdirSync(path.dirname(bad), { recursive: true })
  require('fs').writeFileSync(bad, 'apiVersion: factory.crossplane.io/v1alpha1\nkind: Blueprint\nmetadata: {name: x}\nspec:\n  xrd: {group: g, kind: bad!, plural: bads, version: v1, scope: Namespaced}\n')
  const [chooser] = await Promise.all([
    page.waitForEvent('filechooser'),
    page.click('#importBtn'),
  ])
  await chooser.setFiles(bad)
  await expect(page.locator('#render-warn, [role="alert"]').first()).toContainText(/Kind|kind/, { timeout: 8000 })
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.metadata.name).toBe('xnotify')
})
