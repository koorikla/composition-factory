const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('adding a provider from sources is not dropped by subsequent doc writes', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await expect(page.locator('#region-palette')).toContainText('Installed Providers')

  const input = page.locator('#src-add-ref')
  await input.fill('ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0')
  await page.click('#src-add-btn')

  await expect(page.locator('#region-palette')).toContainText('provider-aws-s3', { timeout: 10000 })

  // Open drawer and click Apply in the blueprint editor
  await page.click('#tabs button[data-t="bp"]')
  await page.click('#code-edit')
  await page.click('#code-apply')

  const res = await request.get(ENGINE + '/api/providers')
  const json = await res.json()
  const refs = json.providers.map(p => p.ref)
  expect(refs).toContain('ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0')
})
