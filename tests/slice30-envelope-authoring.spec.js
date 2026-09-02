// Slice 30 — envelope authoring: the inspector exposes the kind's real
// Crossplane envelope (writeConnectionSecretToRef, managementPolicies) with
// the same V/W forms as forProvider fields. providerConfigRef used to be
// derived-only; since per-resource overrides landed, its rows are offered
// like any other envelope field (the derived default still applies when
// unset).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('the envelope section lists real fields, providerConfigRef now overridable', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  const sec = page.locator('#insp .insp-sec', { hasText: 'Crossplane Envelope' })
  await expect(sec).toBeVisible()
  await expect(sec).toContainText('writeConnectionSecretToRef.name')
  await expect(sec).toContainText('managementPolicies')
  // per-resource providerConfigRef overrides: the rows are offered; unset
  // they keep the derived {kind, name: providerName} default
  await expect(sec).toContainText('providerConfigRef.name')
})

test('wiring the secret name from a parameter persists and renders', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.locator('#insp button[data-m="w"][data-env][data-path="writeConnectionSecretToRef.name"]').click()
  await page.locator('#insp select[data-env-wire="writeConnectionSecretToRef.name"]').selectOption('params.providerName')
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = doc.spec.resources.find(x => x.name === 'work-queue')
    return r.envelope && r.envelope['writeConnectionSecretToRef.name']
      ? r.envelope['writeConnectionSecretToRef.name'].from : undefined
  }).toBe('params.providerName')
  await expect(page.locator('#code')).toContainText('writeConnectionSecretToRef', { timeout: 8000 })
})

test('a literal envelope value round-trips and clears', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  const v = page.locator('#insp input[data-env-v="writeConnectionSecretToRef.name"]')
  await v.fill('queue-conn')
  await v.press('Tab')
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = doc.spec.resources.find(x => x.name === 'work-queue')
    return r.envelope && r.envelope['writeConnectionSecretToRef.name']
      ? r.envelope['writeConnectionSecretToRef.name'].value : undefined
  }).toBe('queue-conn')
  await v.fill('')
  await v.press('Tab')
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = doc.spec.resources.find(x => x.name === 'work-queue')
    return r.envelope && r.envelope['writeConnectionSecretToRef.name'] ? 'set' : 'gone'
  }).toBe('gone')
})
