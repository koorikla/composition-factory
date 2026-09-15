// CF-497 — Catalogue "Matches kind:" captions clip before the matched substring
// Contract: For every row in MATCHING CATALOGUE PROVIDERS, the rendered caption
// makes the matched substring visible without hovering (not clipped horizontally).
const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('matching catalogue providers captions do not clip matched kind names for "iam"', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="kinds"]')

  const searchInput = page.locator('#psearch')
  await expect(searchInput).toBeVisible()
  await searchInput.fill('iam')

  // Wait for matching catalogue providers list
  const rows = page.locator('#lrail .cat-row')
  await expect(rows.first()).toBeVisible()

  const reasonLocators = await page.locator('#lrail .cat-row .cat-match-reason').all()
  expect(reasonLocators.length).toBeGreaterThan(0)

  for (const reason of reasonLocators) {
    await expect(reason).toBeVisible()
    const { clientWidth, scrollWidth, text } = await reason.evaluate(el => ({
      clientWidth: el.clientWidth,
      scrollWidth: el.scrollWidth,
      text: el.innerText || el.textContent || ''
    }))
    expect(text.toLowerCase()).toContain('iam')
    expect(clientWidth).toBeGreaterThan(0)
    expect(scrollWidth, `Caption "${text}" was clipped (scrollWidth ${scrollWidth} > clientWidth ${clientWidth})`).toBeLessThanOrEqual(clientWidth)
  }
})

test('matching catalogue providers captions do not clip matched kind names for "subscription"', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="kinds"]')

  const searchInput = page.locator('#psearch')
  await expect(searchInput).toBeVisible()
  await searchInput.fill('subscription')

  const rows = page.locator('#lrail .cat-row')
  await expect(rows.first()).toBeVisible()

  const topicSubRow = page.locator('#lrail .cat-row', { hasText: 'TopicSubscription' }).first()
  await expect(topicSubRow).toBeVisible()

  const reasonLocators = await page.locator('#lrail .cat-row .cat-match-reason').all()
  expect(reasonLocators.length).toBeGreaterThan(0)

  for (const reason of reasonLocators) {
    await expect(reason).toBeVisible()
    const { clientWidth, scrollWidth, text } = await reason.evaluate(el => ({
      clientWidth: el.clientWidth,
      scrollWidth: el.scrollWidth,
      text: el.innerText || el.textContent || ''
    }))
    expect(text.toLowerCase()).toContain('subscription')
    expect(clientWidth).toBeGreaterThan(0)
    expect(scrollWidth, `Caption "${text}" was clipped (scrollWidth ${scrollWidth} > clientWidth ${clientWidth})`).toBeLessThanOrEqual(clientWidth)
  }
})

test('sources tab catalogue captions do not clip kind match reasons', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const searchInput = page.locator('#cat-search')
  await expect(searchInput).toBeVisible()
  await searchInput.fill('subscription')

  const rows = page.locator('#lrail .cat-row')
  await expect(rows.first()).toBeVisible()

  const reasonLocators = await page.locator('#lrail .cat-row .cat-match-reason').all()
  expect(reasonLocators.length).toBeGreaterThan(0)

  for (const reason of reasonLocators) {
    await expect(reason).toBeVisible()
    const { clientWidth, scrollWidth, text } = await reason.evaluate(el => ({
      clientWidth: el.clientWidth,
      scrollWidth: el.scrollWidth,
      text: el.innerText || el.textContent || ''
    }))
    expect(clientWidth).toBeGreaterThan(0)
    expect(scrollWidth, `Caption "${text}" was clipped in sources catalogue (scrollWidth ${scrollWidth} > clientWidth ${clientWidth})`).toBeLessThanOrEqual(clientWidth)
  }
})

test('matching catalogue providers caption with long kind name wraps cleanly without horizontal overflow', async ({ page }) => {
  await page.goto('/')

  await page.route('**/api/catalogue?*', async route => {
    const json = {
      providers: [
        {
          name: 'provider-aws-s3-extra',
          ref: 'xpkg.upbound.io/upbound/provider-aws-s3-extra:v1.0.0',
          matchedKind: 'BucketIntelligentTieringConfiguration'
        }
      ]
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(json)
    })
  })

  await page.click('#rtabs button[data-r="kinds"]')
  const searchInput = page.locator('#psearch')
  await searchInput.fill('tiering')

  const reason = page.locator('#lrail .cat-row .cat-match-reason').first()
  await expect(reason).toBeVisible()
  const { clientWidth, scrollWidth, text } = await reason.evaluate(el => ({
    clientWidth: el.clientWidth,
    scrollWidth: el.scrollWidth,
    text: el.innerText || el.textContent || ''
  }))
  expect(text).toContain('BucketIntelligentTieringConfiguration')
  expect(clientWidth).toBeGreaterThan(0)
  expect(scrollWidth, `Long caption "${text}" was clipped (scrollWidth ${scrollWidth} > clientWidth ${clientWidth})`).toBeLessThanOrEqual(clientWidth)
})
