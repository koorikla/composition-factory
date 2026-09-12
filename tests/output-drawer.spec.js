const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')

guardPageErrors()

async function setupCleanDoc(request) {
  await resetDoc(request)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  delete doc.spec.conventions
  delete doc.spec.templates
  const dl = doc.spec.resources?.find(r => r.name === 'dead-letter')
  if (dl?.fields?.tags) delete dl.fields.tags
  await request.put(ENGINE + '/api/blueprint', { data: doc })
}

test('switching engine to non-go engine while on FileSystem clears templateSource and succeeds', async ({ page, request }) => {
  await setupCleanDoc(request)

  await page.goto('/')
  const engineSel = page.locator('#engineSel')
  const tplSource = page.locator('#tplSource')
  const toast = page.locator('#canvas-error-toast')

  await expect(engineSel).toHaveValue('go-templating')
  await tplSource.selectOption('FileSystem')
  await expect(tplSource).toHaveValue('FileSystem')

  // Verify FileSystem is set in the document
  let doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec?.emit?.templateSource).toBe('FileSystem')

  // Switching engine to python must clear templateSource and succeed without error
  await engineSel.selectOption('python')
  await expect(toast).not.toBeVisible()
  await expect(engineSel).toHaveValue('python')
  await expect(tplSource).toHaveValue('Inline')

  // Verify document on server has python engine and cleared templateSource
  doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec?.emit?.engine).toBe('python')
  expect(doc.spec?.emit?.templateSource).toBeUndefined()
})

test('tplSource is disabled with tooltip on non-go engines and re-enables on switch back', async ({ page, request }) => {
  await setupCleanDoc(request)

  await page.goto('/')
  const engineSel = page.locator('#engineSel')
  const tplSource = page.locator('#tplSource')
  const fsOpt = tplSource.locator('option[value="FileSystem"]')

  // Initially on go-templating: enabled
  await expect(engineSel).toHaveValue('go-templating')
  await expect(tplSource).toBeEnabled()
  await expect(fsOpt).toBeEnabled()

  // Switch to python: tplSource and FileSystem option must be disabled with tooltip
  await engineSel.selectOption('python')
  await expect(engineSel).toHaveValue('python')
  await expect(tplSource).toBeDisabled()
  await expect(fsOpt).toBeDisabled()
  await expect(tplSource).toHaveAttribute('title', /FileSystem template emission is only available for go-templating/)

  // Switch back to go-templating: re-enabled
  await engineSel.selectOption('go-templating')
  await expect(engineSel).toHaveValue('go-templating')
  await expect(tplSource).toBeEnabled()
  await expect(fsOpt).toBeEnabled()
  await expect(tplSource).toHaveAttribute('title', '')

  // Switch to kcl: disabled again
  await engineSel.selectOption('kcl')
  await expect(engineSel).toHaveValue('kcl')
  await expect(tplSource).toBeDisabled()
  await expect(fsOpt).toBeDisabled()
  await expect(tplSource).toHaveAttribute('title', /FileSystem template emission is only available for go-templating/)
})

test('tplSource is disabled on first paint when loading a document with non-go engine', async ({ page, request }) => {
  await setupCleanDoc(request)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  doc.spec = doc.spec || {}
  doc.spec.emit = doc.spec.emit || {}
  doc.spec.emit.engine = 'python'
  await request.put(ENGINE + '/api/blueprint', { data: doc })

  await page.goto('/')
  const engineSel = page.locator('#engineSel')
  const tplSource = page.locator('#tplSource')
  const fsOpt = tplSource.locator('option[value="FileSystem"]')

  await expect(engineSel).toHaveValue('python')
  await expect(tplSource).toBeDisabled()
  await expect(fsOpt).toBeDisabled()
  await expect(tplSource).toHaveAttribute('title', /FileSystem template emission is only available for go-templating/)
})

