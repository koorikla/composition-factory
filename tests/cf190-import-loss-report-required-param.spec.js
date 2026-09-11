const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  test.setTimeout(90000)
  await resetDoc(request)
})

const COMPOSITION_WORKLOAD = `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xworkloads.workloads.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: workloads.sparky.ee/v1alpha1
    kind: XWorkload
  mode: Pipeline
  pipeline:
    - step: render-resources
      functionRef:
        name: function-go-templating
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        options: ["missingkey=error"]
        inline:
          template: |
            {{- $spec := .observed.composite.resource.spec -}}
            {{- $xr := .observed.composite.resource.metadata.name -}}
            ---
            apiVersion: apps/v1
            kind: Deployment
            metadata:
              name: {{ $xr }}-app
            spec:
              template:
                spec:
                  containers:
                    - image: {{ $spec.image | quote }}
                      name: 'app'
            {{- if $spec.enableService }}
            ---
            apiVersion: v1
            kind: Service
            metadata:
              name: {{ $xr }}-svc
            spec:
              ports:
                - port: 80
            {{- end }}
    - step: auto-ready
      functionRef:
        name: function-auto-ready
`

test('CF-190: importing a Composition that turns an optional parameter into required names the change in the loss banner', async ({ page, request }) => {
  // Load Cloud-Agnostic Web Workload starter
  const loadRes = await request.post(ENGINE + '/api/examples/k8s-workload/load')
  expect(loadRes.ok()).toBe(true)

  await page.goto('/')
  page.on('dialog', d => d.accept())

  // Verify enableService is initially optional (required: false or undefined)
  const initialDoc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(initialDoc.spec.xrd.parameters.enableService.required).toBeFalsy()

  // Hand the composition to the import input
  await page.setInputFiles('#importFile', {
    name: 'composition.yaml',
    mimeType: 'application/yaml',
    buffer: Buffer.from(COMPOSITION_WORKLOAD),
  })

  // The import warn banner must appear
  const warnBar = page.locator('#import-warn')
  await expect(warnBar).toBeVisible({ timeout: 10000 })
  const warnText = await warnBar.textContent()

  // Must name the parameter enableService (or xrd.parameters.enableService)
  expect(warnText).toContain('enableService')

  // Must name the old and new flag: 'optional' and 'required'
  expect(warnText).toContain('required')
  expect(warnText).toContain('optional')

  // Check persisted document: enableService is now required: true
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.xrd.parameters.enableService.required).toBe(true)
})
