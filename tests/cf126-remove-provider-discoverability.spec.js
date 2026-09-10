const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('"Remove provider" button is immediately visible at top of expanded detail without scrolling and uses sources terminology', async ({ page, request }) => {
  // Install an unused provider with kinds (e.g. provider-aws-s3)
  const pre = await request.post(ENGINE + '/api/providers', { data: { ref: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0' } })
  expect(pre.ok()).toBeTruthy()

  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const s3row = page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' }).first()
  await expect(s3row).toBeVisible({ timeout: 10000 })

  // Expand provider details
  await s3row.click()

  // Verify remove button is visible in the viewport immediately without scrolling
  const removeBtn = page.locator('#src-remove-btn')
  await expect(removeBtn).toBeVisible()

  // Verify button title speaks of sources, not cache
  const title = await removeBtn.getAttribute('title')
  expect(title).toMatch(/sources/i)
  expect(title).not.toMatch(/cache/i)

  // Verify button position is above kind checkboxes (top of detail)
  const firstKind = page.locator('#lrail .src-detail label input[data-pick-kind]').first()
  if (await firstKind.count() > 0) {
    const isAbove = await page.evaluate(() => {
      const btn = document.querySelector('#src-remove-btn')
      const kind = document.querySelector('#lrail .src-detail label input[data-pick-kind]')
      if (!btn || !kind) return true
      return Boolean(btn.compareDocumentPosition(kind) & Node.DOCUMENT_POSITION_FOLLOWING)
    })
    expect(isAbove).toBeTruthy()
  }

  // Verify confirmation dialog text speaks of sources, not cache
  let dialogMessage = ''
  page.on('dialog', d => {
    dialogMessage = d.message()
    d.accept()
  })

  await removeBtn.click()
  expect(dialogMessage).toMatch(/sources/i)
  expect(dialogMessage).not.toMatch(/cache/i)

  // Verify provider was removed
  await expect(page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' })).toHaveCount(0, { timeout: 10000 })
})

test('provider row offers direct remove action without requiring expansion', async ({ page, request }) => {
  const pre = await request.post(ENGINE + '/api/providers', { data: { ref: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0' } })
  expect(pre.ok()).toBeTruthy()

  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const s3row = page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' }).first()
  await expect(s3row).toBeVisible({ timeout: 10000 })

  // Provider row should have a remove button/affordance
  const rowRemove = s3row.locator('.src-row-remove, button[data-remove-ref]')
  await expect(rowRemove).toBeVisible()

  page.on('dialog', d => d.accept())
  await rowRemove.click()

  // Verify provider was removed without needing to expand full kinds list
  await expect(page.locator('#lrail .src-row', { hasText: 'provider-aws-s3' })).toHaveCount(0, { timeout: 10000 })
})
