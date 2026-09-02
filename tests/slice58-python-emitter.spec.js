// Slice 58 — Alternative composition emission engine: Python (function-python)
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request, page }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running')
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
})

test('switching engine to python emits function-python composition and updates functions.yaml', async ({ page }) => {
  const engineSel = page.locator('#engineSel')
  await expect(engineSel).toBeVisible()
  await expect(engineSel).toHaveValue('go-templating')

  // Composition initially has function-go-templating
  await page.click('#tabs button[data-t="comp"]')
  await expect(page.locator('#code')).toContainText('function-go-templating')

  // Switch to Python
  await engineSel.selectOption('python')

  // Composition now has function-python with Script and python code
  await expect(page.locator('#code')).toContainText('function-python')
  await expect(page.locator('#code')).toContainText('apiVersion: python.fn.crossplane.io/v1beta1')
  await expect(page.locator('#code')).toContainText('kind: Script')
  await expect(page.locator('#code')).toContainText('def compose(req: fnv1.RunFunctionRequest, rsp: fnv1.RunFunctionResponse):')
  await expect(page.locator('#code')).toContainText('rsp.desired.resources["work-queue"].resource.update')

  // Functions tab now pins function-python
  await page.click('#tabs button[data-t="fns"]')
  await expect(page.locator('#code')).toContainText('name: function-python')
  await expect(page.locator('#code')).toContainText('xpkg.upbound.io/crossplane-contrib/function-python')

  // Blueprint tab shows spec.emit.engine: python
  await page.click('#tabs button[data-t="bp"]')
  await expect(page.locator('#code')).toContainText('engine: python')

  // Undo restores go-templating
  await page.keyboard.press('ControlOrMeta+z')
  await expect(engineSel).toHaveValue('go-templating')
  await page.click('#tabs button[data-t="comp"]')
  await expect(page.locator('#code')).toContainText('function-go-templating')
})

test('a reload keeps python engine select in sync with the persisted document', async ({ page }) => {
  await page.click('#tabs button[data-t="comp"]')
  const engineSel = page.locator('#engineSel')
  await engineSel.selectOption('python')
  await expect(page.locator('#code')).toContainText('function-python')

  await page.reload()
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  await expect(page.locator('#engineSel')).toHaveValue('python')
  await page.click('#tabs button[data-t="comp"]')
  await expect(page.locator('#code')).toContainText('function-python')
})
