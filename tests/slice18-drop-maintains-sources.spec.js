// Slice 18 — dropping a kind keeps spec.sources true: a resource from a
// provider not yet declared adds that provider to sources, so generate never
// sees a kind whose CRDs it can't load (the BucketVersioning red-chip bug).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request) // pristine doc: sources = [provider-aws-sqs] only
  // load s3 into the running engine (runtime add; sources persistence is
  // exactly what this slice tests). 200 = newly added, 409 = already loaded.
  const add = await request.post(ENGINE + '/api/providers', {
    data: { ref: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0' } })
  expect([200, 409]).toContain(add.status())
})

test('dropping an s3 kind declares the s3 source and generate stays green', async ({ page, request }) => {
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  await page.evaluate(() => {
    const row = document.querySelector('#lrail .kind[data-kind="Bucket"]')
    const cw = document.getElementById('cw')
    const r = cw.getBoundingClientRect()
    const dt = new DataTransfer()
    row.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }))
    const at = { clientX: r.left + 200, clientY: r.top + 320 }
    cw.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt, ...at }))
    cw.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt, ...at }))
  })
  await expect(page.locator('.node[data-id="bucket"]')).toBeVisible()
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    return (doc.spec.sources || []).map(s => s.provider).join(',')
  }).toContain('provider-aws-s3')
  const gen = await request.post(ENGINE + '/api/generate', { data: { write: false } })
  expect(gen.status()).toBe(200)
})
