const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
const fs = require('fs')
const path = require('path')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  test.setTimeout(90000)
  await resetDoc(request)
})

const COMPOSITION = fs.readFileSync(
  path.join(__dirname, '../testdata/xqueue-pipeline.composition.golden.yaml'),
  'utf8'
)

const XRD = `apiVersion: apiextensions.crossplane.io/v2
kind: CompositeResourceDefinition
metadata:
  name: xqueues.platform.sparky.ee
spec:
  group: platform.sparky.ee
  names:
    kind: XQueue
    plural: xqueues
  scope: Namespaced
  versions:
  - name: v1alpha1
    served: true
    referenceable: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              location:
                type: string
                enum:
                - 'EU'
                - 'US'
              maxMessageSize:
                type: integer
                default: 2048
              providerName:
                type: string
            required: [location, providerName]
`

test('CF-217: importing an XRD complements an adopted composition losslessly and clears the loss banner', async ({ page, request }) => {
  await page.goto('/')
  page.on('dialog', d => d.accept())

  // Verify Import button tooltip explains lossless path
  const importBtn = page.locator('#importBtn')
  const title = await importBtn.getAttribute('title')
  expect(title).toContain('Composition & XRD')
  expect(title).toContain('lossless adoption')

  // Step 1: Import composition alone
  await page.setInputFiles('#importFile', {
    name: 'xqueue-pipeline.composition.yaml',
    mimeType: 'application/yaml',
    buffer: Buffer.from(COMPOSITION),
  })

  // Loss banner appears
  const warnBar = page.locator('#import-warn')
  await expect(warnBar).toBeVisible({ timeout: 10000 })
  const warnText = await warnBar.textContent()
  expect(warnText).toContain('maxMessageSize')
  expect(warnText).toContain('without the XRD')
  expect(warnText).toMatch(/combine XRD and Composition|supply XRD and Composition/)

  // Step 2: Import the XRD that the banner asked for
  await page.setInputFiles('#importFile', {
    name: 'xqueues.platform.sparky.ee.yaml',
    mimeType: 'application/yaml',
    buffer: Buffer.from(XRD),
  })

  // Toast must announce adoption / parameter change, not a failure toast
  const toast = page.locator('#import-toast')
  await expect(toast).toBeVisible({ timeout: 10000 })
  await expect(toast).toContainText('parameter $maxMessageSize type changed')

  // Warn banner must now be hidden / cleared
  await expect(warnBar).toBeHidden()

  // Document on server has updated parameter type integer and default 2048
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.xrd.parameters.maxMessageSize.type).toBe('integer')
  expect(doc.spec.xrd.parameters.maxMessageSize.default).toBe('2048')
  expect(doc.spec.xrd.parameters.location.type).toBe('string')
})

test('CF-217: selecting both Composition and XRD files together adopts losslessly in one go', async ({ page, request }) => {
  await page.goto('/')
  page.on('dialog', d => d.accept())

  // Pass both files simultaneously to #importFile
  await page.setInputFiles('#importFile', [
    {
      name: 'xqueues.platform.sparky.ee.yaml',
      mimeType: 'application/yaml',
      buffer: Buffer.from(XRD),
    },
    {
      name: 'xqueue-pipeline.composition.yaml',
      mimeType: 'application/yaml',
      buffer: Buffer.from(COMPOSITION),
    },
  ])

  // Toast confirms adoption
  const toast = page.locator('#import-toast')
  await expect(toast).toBeVisible({ timeout: 10000 })

  // No loss banner should appear because adoption with XRD is lossless
  const warnBar = page.locator('#import-warn')
  await expect(warnBar).toBeHidden()

  // Persisted doc has integer type and location
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.xrd.parameters.maxMessageSize.type).toBe('integer')
  expect(doc.spec.xrd.parameters.maxMessageSize.default).toBe('2048')
  expect(doc.spec.xrd.parameters.location.type).toBe('string')
})
