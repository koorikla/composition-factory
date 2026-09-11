const { test, expect } = require('@playwright/test')
const { ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

const scaffoldDoc = {
  apiVersion: 'factory.crossplane.io/v1alpha1',
  kind: 'Blueprint',
  metadata: { name: 'untitled' },
  spec: {
    sources: [],
    xrd: {
      group: 'platform.example.org',
      kind: 'XApp',
      plural: 'xapps',
      version: 'v1alpha1',
      scope: 'Namespaced',
      parameters: {
        providerName: {
          type: 'string',
          required: true,
          description: 'ProviderConfig to reconcile the composed resources against.'
        }
      }
    },
    resources: []
  }
}

test.beforeEach(async ({ request }) => {
  const r = await request.put(ENGINE + '/api/blueprint', { data: scaffoldDoc })
  if (!r.ok()) throw new Error('reset scaffoldDoc failed: ' + (await r.text()))
})

test('providerName parameter row is locked from first paint on a scaffolded document', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('cf:empty-start-offered', 'true')
  })

  await page.goto('/')

  // Select XRD
  await page.click('.node[data-id="xrd"] .node-h')

  // Find providerName name input
  const nameInput = page.locator('#insp input[data-pn="providerName"]')
  await expect(nameInput).toBeVisible()

  // Verify nameInput is readonly or disabled on first paint
  const isReadOnly = await nameInput.getAttribute('readonly')
  const isDisabled = await nameInput.getAttribute('disabled')
  expect(isReadOnly !== null || isDisabled !== null).toBe(true)

  // Find providerName delete button
  const delBtn = page.locator('#insp button[data-pd="providerName"]')
  await expect(delBtn).toBeVisible()

  // Verify delBtn is disabled on first paint
  const isDelDisabled = await delBtn.getAttribute('disabled')
  expect(isDelDisabled !== null).toBe(true)
  expect(await delBtn.getAttribute('title')).toBe('providerName is required for managed resources in Namespaced XRD')

  // Verify type select is disabled
  const typeSelect = page.locator('#insp select[data-pt="providerName"]')
  await expect(typeSelect).toBeDisabled()

  // Verify req checkbox is disabled
  const reqCheck = page.locator('#insp input[data-pr="providerName"]')
  await expect(reqCheck).toBeDisabled()

  // Attempting to click delete button does not delete providerName
  await delBtn.click({ force: true })
  await expect(page.locator('#insp input[data-pn="providerName"]')).toBeVisible()
  const paramsHeader = page.locator('#insp .lbl', { hasText: 'Parameters' })
  await expect(paramsHeader).toContainText('Parameters (1)')
})

test('providerName is unlocked when all composed resources are native k8s', async ({ page, request }) => {
  await page.addInitScript(() => {
    localStorage.setItem('cf:empty-start-offered', 'true')
  })

  // Namespaced scope with native k8s-only resource
  const k8sDoc = JSON.parse(JSON.stringify(scaffoldDoc))
  k8sDoc.spec.resources = [
    {
      name: 'config',
      kind: 'ConfigMap',
      provider: 'k8s',
      fields: {}
    }
  ]
  const r = await request.put(ENGINE + '/api/blueprint', { data: k8sDoc })
  if (!r.ok()) throw new Error('put k8sDoc failed: ' + (await r.text()))

  await page.goto('/')
  await page.click('.node[data-id="xrd"] .node-h')

  const nameInput = page.locator('#insp input[data-pn="providerName"]')
  const delBtn = page.locator('#insp button[data-pd="providerName"]')
  await expect(nameInput).toBeVisible()
  expect(await nameInput.getAttribute('readonly')).toBeNull()
  expect(await delBtn.getAttribute('disabled')).toBeNull()
})
