// tests/cf157-kinds-search-empty-state-sources-hint.spec.js
// CF-157 — Kinds search dead-ends with 'No kinds match search query.' when the answer is 'add a provider in SOURCES'.
// Contract: the empty state names SOURCES (and, when the catalogue has a name match, offers it).
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('kinds search empty state names SOURCES and offers catalogue matches', async ({ page }) => {
  await page.goto('/')

  // Switch to KINDS tab
  await page.click('#rtabs button[data-r="kinds"]')

  // Search for 's3' which has no installed kinds initially (only native k8s and sqs from pristine doc)
  const search = page.locator('#psearch')
  await search.fill('s3')

  const emptyEl = page.locator('#lrail .empty')
  await expect(emptyEl).toBeVisible()

  // Must name SOURCES
  await expect(emptyEl).toContainText('SOURCES')

  // When the catalogue has a name match for 's3', offers it
  await expect(emptyEl).toContainText('provider-aws-s3')
  const addBtn = emptyEl.locator('button[data-cat-ref="ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0"]')
  await expect(addBtn).toBeVisible()

  // Clicking the offered provider installs it directly
  await addBtn.click()

  // After installing, kinds appear or toast opens KINDS
  await expect(page.locator('#palette-toast')).toBeVisible({ timeout: 30000 })
})

test('kinds search empty state names SOURCES even when catalogue has no match', async ({ page }) => {
  await page.goto('/')

  await page.click('#rtabs button[data-r="kinds"]')

  const search = page.locator('#psearch')
  await search.fill('zzzznonexistentanything')

  const emptyEl = page.locator('#lrail .empty')
  await expect(emptyEl).toBeVisible()

  // Must name SOURCES
  await expect(emptyEl).toContainText('SOURCES')

  // Clicking SOURCES switches to the SOURCES tab
  const srcLink = emptyEl.locator('button[data-tab-switch="src"]')
  await expect(srcLink).toBeVisible()
  await srcLink.click()
  await expect(page.locator('#rtabs button[data-r="src"]')).toHaveAttribute('aria-pressed', 'true')
})
