// The canvas had two front doors for existing YAML and only exposed one: the
// Import button posted everything to /api/blueprint/import, so a real
// Crossplane Composition — the thing most people already have — was rejected
// as an invalid blueprint. Import now routes on the manifest's own kind and
// adopts a Composition through /api/blueprint/adopt, reporting what adoption
// had to drop rather than silently keeping a partial document.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

const COMPOSITION = `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: adopted-from-ui
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
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
              name: adopted-queue
            spec:
              forProvider:
                region: {{ $spec.region }}
`

/** Hand the hidden file input a file, exactly as the Import button does. */
async function importFile(page, name, body) {
  await page.setInputFiles('#importFile', {
    name, mimeType: 'application/yaml', buffer: Buffer.from(body),
  })
}

test('importing a Crossplane Composition adopts it onto the canvas', async ({ page, request }) => {
  await page.goto('/')
  await expect(page.locator('.node')).toHaveCount(3)

  await importFile(page, 'composition.yaml', COMPOSITION)

  // the adopted document replaces the pristine one, and lands on the canvas
  await expect(page.locator('.node[data-id="adopted-queue"]')).toBeVisible()
  await expect(page.locator('.node[data-id="dead-letter"]')).toHaveCount(0)

  // and it is persisted, not just drawn
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    return (doc.spec.resources || []).map((r) => r.name)
  }).toEqual(['adopted-queue'])
})

test('adopting is undoable, like any other import', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.node')).toHaveCount(3)

  await importFile(page, 'composition.yaml', COMPOSITION)
  await expect(page.locator('.node[data-id="adopted-queue"]')).toBeVisible()

  await page.keyboard.press('ControlOrMeta+z')
  await expect(page.locator('.node[data-id="dead-letter"]')).toBeVisible()
  await expect(page.locator('.node[data-id="adopted-queue"]')).toHaveCount(0)
})

test('a cf blueprint still goes through the import gate, not adopt', async ({ page, request }) => {
  await page.goto('/')
  await expect(page.locator('.node')).toHaveCount(3)

  const blueprint = `apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: imported
spec:
  sources: []
  xrd:
    group: platform.example.org
    kind: XThing
    plural: xthings
    version: v1alpha1
    scope: Namespaced
    parameters:
      size: {type: string, required: true}
  resources: []
`
  await importFile(page, 'thing.cf.yaml', blueprint)

  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    return doc.metadata.name
  }).toBe('imported')
  // the XR card is the only node a resource-less blueprint draws
  await expect(page.locator('.node')).toHaveCount(1)
})

test('what adoption had to drop is reported, not swallowed', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.node')).toHaveCount(3)

  // claimNames and connectionSecretKeys have no blueprint equivalent, so an
  // XRD carrying them adopts lossily — the user has to be told which parts of
  // their Composition did not survive the trip.
  const lossy = `apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xqueues.example.org
spec:
  group: example.org
  names: {kind: XQueue, plural: xqueues}
  claimNames: {kind: QueueClaim, plural: queueclaims}
  connectionSecretKeys: [endpoint]
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
                region: {type: string}
---
` + COMPOSITION

  await importFile(page, 'xrd-and-composition.yaml', lossy)

  const warn = page.locator('#import-warn')
  await expect(warn).toBeVisible()
  await expect(warn).toContainText('adopted with 2 dropped items')
  await expect(warn).toContainText('xrd.claimNames')
  await expect(warn).toContainText('xrd.connectionSecretKeys')

  // and the adoption still landed
  await expect(page.locator('.node[data-id="adopted-queue"]')).toBeVisible()
})

// cf's own package.yaml is a Configuration stream that also contains an XRD and
// a Composition, so routing purely on "does this contain a Composition" would
// send cf's own export to adopt and approximate a blueprint it could have
// recovered exactly. The embedded-blueprint annotation outranks the kinds.
test('cf\'s own package.yaml round-trips through import, not adopt', async ({ page, request }) => {
  await page.goto('/')
  await expect(page.locator('.node')).toHaveCount(3)

  const pkg = await (await request.get(ENGINE + '/api/package?format=yaml')).text()
  expect(pkg).toContain('kind: Configuration')
  expect(pkg).toContain('kind: Composition')
  expect(pkg).toContain('factory.crossplane.io/blueprint')

  // move off the pristine doc so recovering it proves something
  await importFile(page, 'composition.yaml', COMPOSITION)
  await expect(page.locator('.node[data-id="adopted-queue"]')).toBeVisible()

  // the package recovers the original blueprint exactly, wires and all
  await importFile(page, 'package.yaml', pkg)
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  await expect(page.locator('.node[data-id="dead-letter"]')).toBeVisible()
  await expect(page.locator('svg.wires path.wire-path')).toHaveCount(3)

  // adopt would have reported drops; import reports nothing
  await expect(page.locator('#import-warn')).toBeHidden()
})

test('a manifest that is neither reports the server error verbatim', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.node')).toHaveCount(3)

  await importFile(page, 'junk.yaml', 'kind: NotAThingWeKnow\nnope: [')

  const warn = page.locator('#import-warn')
  await expect(warn).toBeVisible()
  await expect(warn).toContainText('import failed')
})
