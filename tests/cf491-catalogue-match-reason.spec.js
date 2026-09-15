// CF-491 — Catalogue match reason caption must name the matching field
// Contract: A match-reason caption must name the field the match actually came from,
// or not be shown. A row must never be captioned as matching a string the row's
// own quoted text does not contain.
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('catalogue search captions kind matches with kind name and never attributes kind matches to description', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const searchInput = page.locator('#cat-search')
  await expect(searchInput).toBeVisible()
  await searchInput.fill('rds')

  // Wait for catalogue results to render
  const rows = page.locator('#lrail .cat-row')
  await expect(rows.first()).toBeVisible()

  const awsRow = page.locator('#lrail .cat-row', { hasText: 'provider-aws-rds' }).first()
  await expect(awsRow).toBeVisible()
  // Name match should not show any match reason
  await expect(awsRow.locator('.cat-match-reason')).toHaveCount(0)

  // provider-gcp-dns matched because packageKinds["provider-gcp-dns"] contains RecordSet
  const dnsRow = page.locator('#lrail .cat-row', { hasText: 'provider-gcp-dns' }).first()
  await expect(dnsRow).toBeVisible()

  const dnsReason = dnsRow.locator('.cat-match-reason')
  await expect(dnsReason).toBeVisible()

  // Must name the kind field and the matching kind "RecordSet"
  await expect(dnsReason).toHaveText('Matches kind: RecordSet')

  // Must NOT claim to match description
  await expect(dnsReason).not.toContainText(/description/i)
})

test('every match-reason caption names the field it came from and contains the query', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const searchInput = page.locator('#cat-search')
  await expect(searchInput).toBeVisible()
  await searchInput.fill('rds')

  const rows = page.locator('#lrail .cat-row')
  await expect(rows.first()).toBeVisible()

  const reasonLocators = await page.locator('#lrail .cat-row .cat-match-reason').all()
  expect(reasonLocators.length).toBeGreaterThan(0)

  for (const reasonEl of reasonLocators) {
    const text = await reasonEl.innerText()
    // Reason must either match kind or description, and must contain 'rds'
    const isKind = text.startsWith('Matches kind: ')
    const isDesc = text.startsWith('Matches description: ')
    expect(isKind || isDesc).toBe(true)
    expect(text.toLowerCase()).toContain('rds')
  }
})

test('description-only match in catalogue shows Matches description with quoted text containing query', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const searchInput = page.locator('#cat-search')
  await expect(searchInput).toBeVisible()
  await searchInput.fill('custom status')

  const rows = page.locator('#lrail .cat-row')
  await expect(rows.first()).toBeVisible()

  const statusRow = page.locator('#lrail .cat-row', { hasText: 'function-status-transformer' }).first()
  await expect(statusRow).toBeVisible()

  const reason = statusRow.locator('.cat-match-reason')
  await expect(reason).toBeVisible()
  await expect(reason).toHaveText(/Matches description: .*/)
  const text = await reason.innerText()
  expect(text.toLowerCase()).toContain('custom status')
})

test('kinds tab search explains catalogue provider match with kind name', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="kinds"]')

  const searchInput = page.locator('#psearch')
  await expect(searchInput).toBeVisible()
  await searchInput.fill('rds')

  // In the matching catalogue providers section
  const dnsRow = page.locator('#lrail .cat-row', { hasText: 'provider-gcp-dns' }).first()
  await expect(dnsRow).toBeVisible()

  const dnsReason = dnsRow.locator('.cat-match-reason')
  await expect(dnsReason).toBeVisible()
  await expect(dnsReason).toHaveText('Matches kind: RecordSet')
  await expect(dnsReason).not.toContainText(/description/i)
})
