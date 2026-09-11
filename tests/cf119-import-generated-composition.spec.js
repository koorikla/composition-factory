const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  test.setTimeout(90000)
  await resetDoc(request)
})

const COMPOSITION_WITH_AUTOREADY = `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        inline:
          template: |
            apiVersion: sqs.aws.upbound.io/v1beta1
            kind: Queue
            metadata:
              name: main-queue
            spec:
              forProvider:
                region: {{ $spec.region | quote }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

test('importing a generated Composition does not materialize auto-ready as custom step or 404', async ({ page, request }) => {
  // Capture console errors
  const consoleErrors = []
  page.on('console', msg => {
    if (msg.type() === 'error') {
      consoleErrors.push(msg.text())
    }
  })

  await page.goto('/')
  await expect(page.locator('.node')).toHaveCount(3)

  // Hand the generated composition to the import input
  await page.setInputFiles('#importFile', {
    name: 'composition.yaml',
    mimeType: 'application/yaml',
    buffer: Buffer.from(COMPOSITION_WITH_AUTOREADY),
  })

  // The imported document lands on the canvas
  await expect(page.locator('.node[data-id="main-queue"]')).toBeVisible()

  // Select XRD node to inspect pipeline and parameters
  await page.click('.node[data-id="xrd"] .node-h')

  // Inspector should show Pipeline (default), NOT PIPELINE (1 CUSTOM)
  await expect(page.locator('#insp')).toContainText('Pipeline (default)')
  await expect(page.locator('#insp')).not.toContainText('PIPELINE (1 CUSTOM)')

  // Check that console does not log 404 for AutoReady fields
  const autoReady404s = consoleErrors.filter(err => err.includes('AutoReady') || err.includes('autoready'))
  expect(autoReady404s).toHaveLength(0)

  // Check persisted doc: pipeline must remain default/empty, region required: true
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.pipeline || []).toHaveLength(0)
  expect(doc.spec.xrd.parameters.region.required).toBe(true)
})
