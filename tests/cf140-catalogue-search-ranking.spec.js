// CF-140 — Catalogue search ranking and description match explanation
// Contract: rank name matches above description matches and show why a description-only match is listed.
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('catalogue search ranks name matches above description matches and explains description matches', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const searchInput = page.locator('#cat-search')
  await expect(searchInput).toBeVisible()
  await searchInput.fill('iam')

  // Wait for catalogue results to render
  const rows = page.locator('#lrail .cat-row')
  await expect(rows.first()).toBeVisible()
  await expect(page.locator('#lrail .cat-row', { hasText: 'provider-aws-iam' }).first()).toBeVisible()
  await expect(page.locator('#lrail .cat-row', { hasText: 'provider-gcp-iam' }).first()).toBeVisible()
  await expect(page.locator('#lrail .cat-row', { hasText: 'provider-gcp-artifact' }).first()).toBeVisible()

  const names = await page.locator('#lrail .cat-row .nm').allTextContents()

  // 1. Ranking contract: all name matches must appear before any description-only matches.
  // In particular, provider-gcp-iam (name match) must rank before provider-gcp-artifact (description-only match).
  const gcpIamIdx = names.indexOf('provider-gcp-iam')
  const artifactIdx = names.indexOf('provider-gcp-artifact')
  expect(gcpIamIdx).toBeGreaterThanOrEqual(0)
  expect(artifactIdx).toBeGreaterThanOrEqual(0)
  expect(gcpIamIdx).toBeLessThan(artifactIdx)

  const firstDescIdx = names.findIndex(name => !name.toLowerCase().includes('iam'))
  const lastNameIdx = names.reduce((max, name, idx) => name.toLowerCase().includes('iam') ? Math.max(max, idx) : max, -1)
  expect(firstDescIdx).toBeGreaterThanOrEqual(0)
  expect(lastNameIdx).toBeGreaterThanOrEqual(0)
  expect(lastNameIdx).toBeLessThan(firstDescIdx)

  // 2. Explanation contract: description-only matches must show why they are listed.
  const artifactRow = page.locator('#lrail .cat-row', { hasText: 'provider-gcp-artifact' }).first()
  const matchReason = artifactRow.locator('.cat-match-reason')
  await expect(matchReason).toBeVisible()
  await expect(matchReason).toContainText(/description/i)

  // Name matches should not show a description-match reason badge/line.
  const awsRow = page.locator('#lrail .cat-row', { hasText: 'provider-aws-iam' }).first()
  await expect(awsRow.locator('.cat-match-reason')).toHaveCount(0)
})
