// Slice 57 — Alternative composition emission engine: KCL (function-kcl)
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request, page }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running')
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
})

test('switching engine to kcl emits function-kcl composition and updates functions.yaml', async ({ page }) => {
  const engineSel = page.locator('#engineSel')
  await expect(engineSel).toBeVisible()
  await expect(engineSel).toHaveValue('go-templating')

  // Composition initially has function-go-templating
  await page.click('#tabs button[data-t="comp"]')
  await expect(page.locator('#code')).toContainText('function-go-templating')

  // Switch to KCL
  await engineSel.selectOption('kcl')

  // Composition now has function-kcl with KCLInput and KCL syntax
  await expect(page.locator('#code')).toContainText('function-kcl')
  await expect(page.locator('#code')).toContainText('apiVersion: krm.kcl.dev/v1alpha1')
  await expect(page.locator('#code')).toContainText('kind: KCLInput')
  await expect(page.locator('#code')).toContainText('oxr = option("params")?.oxr or {}')
  await expect(page.locator('#code')).toContainText('"krm.kcl.dev/composition-resource-name" = "work-queue"')
  await expect(page.locator('#code')).toContainText('region = _spec?.region')

  // Functions tab now pins function-kcl
  await page.click('#tabs button[data-t="fns"]')
  await expect(page.locator('#code')).toContainText('name: function-kcl')
  await expect(page.locator('#code')).toContainText('xpkg.upbound.io/crossplane-contrib/function-kcl')

  // Blueprint tab shows spec.emit.engine: kcl
  await page.click('#tabs button[data-t="bp"]')
  await expect(page.locator('#code')).toContainText('engine: kcl')

  // Undo restores go-templating
  await page.keyboard.press('ControlOrMeta+z')
  await expect(engineSel).toHaveValue('go-templating')
  await page.click('#tabs button[data-t="comp"]')
  await expect(page.locator('#code')).toContainText('function-go-templating')
})

test('a reload keeps engine select in sync with the persisted document', async ({ page }) => {
  await page.locator('#engineSel').selectOption('kcl')
  await expect(page.locator('#code')).toContainText('function-kcl')

  await page.reload()
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  await expect(page.locator('#engineSel')).toHaveValue('kcl')
  await page.click('#tabs button[data-t="comp"]')
  await expect(page.locator('#code')).toContainText('function-kcl')
})
