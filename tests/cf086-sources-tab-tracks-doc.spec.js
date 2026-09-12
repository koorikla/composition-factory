// tests/cf086-sources-tab-tracks-doc.spec.js
// CF-086 — The SOURCES tab keeps showing the provider list it fetched first,
// even after the document's sources change (loading a starter example here).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('sources tab lists the providers the server serves after a starter example is loaded', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await expect(page.locator('#region-palette')).toContainText('Installed Providers')

  await page.click('#examplesBtn')
  await page.locator('button[data-load-id="s3-bucket"]').click()
  await expect(page.locator('#examplesOverlay')).toBeHidden()
  await expect(page.locator('.node[data-id="bucket"]')).toBeVisible({ timeout: 15000 })

  const served = (await (await request.get(ENGINE + '/api/providers')).json()).providers.map(p => p.ref)
  expect(served).toContain('ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0')

  const rail = page.locator('#region-palette')
  await expect(rail).toContainText('provider-aws-s3', { timeout: 5000 })
  for (const ref of served) await expect(rail).toContainText(ref.split('/').pop().split(':')[0])
  await expect(rail).not.toContainText('provider-aws-sqs')
})
