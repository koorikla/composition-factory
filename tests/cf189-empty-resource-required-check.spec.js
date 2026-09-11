const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')

guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('unconfigured resource stays green in preview but generate write refuses missing required field', async ({ page, request }) => {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  doc.spec.resources.push({
    name: 'untouched-queue',
    kind: 'Queue',
    provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
    fields: {}
  })
  const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(putRes.status()).toBe(200)

  await page.goto('/')

  const validChip = page.locator('#valid')
  await expect(validChip).toBeVisible()
  // Preview mode stays green
  await expect(validChip).toContainText('preview ·')

  // Attempt write generate
  page.on('dialog', dialog => dialog.accept())
  await page.click('#generateBtn')

  // Must fail and show error, NOT written
  await expect(validChip).toHaveText('error', { timeout: 10000 })
  await expect(validChip).not.toContainText('written')
})
