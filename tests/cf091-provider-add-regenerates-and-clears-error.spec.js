const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('adding a missing provider from sources regenerates and clears error status', async ({ page }) => {
  await page.goto('/')
  // Open sources
  await page.click('#rtabs button[data-r="src"]')
  await expect(page.locator('#region-palette')).toContainText('Installed Providers')

  // Add provider-aws-s3
  const input = page.locator('#src-add-ref')
  await input.fill('ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0')
  await page.click('#src-add-btn')

  // Wait for installed
  await expect(page.locator('#region-palette')).toContainText('provider-aws-s3', { timeout: 10000 })

  // Verify that status chip is not in error state and shows preview/files
  const chip = page.locator('#valid')
  await expect(chip).not.toContainText('error')
  await expect(chip).toContainText('preview ·')
})
