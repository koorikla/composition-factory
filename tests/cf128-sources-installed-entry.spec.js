// CF-128 — In the SOURCES catalogue an installed entry's name truncates to provider-a…,
// its ref wraps onto four lines under the INSTALLED · 44 KINDS badge, and the search-term
// highlight from an earlier query is painted inside the installed list's name.
// Contract:
// 1. In the SOURCES rail, an installed provider entry's name (.nm) does not awkwardly truncate
//    to provider-a… when room exists; style/flex allows proper visibility.
// 2. The reference under the name displays cleanly without wrapping awkwardly onto four lines under badges.
// 3. Stale search-term highlighting from catalogue searches is not painted inside the installed
//    list's names (installed provider names should not highlight based on cat-search queries).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('installed catalogue entry name does not truncate when room exists, ref does not wrap onto 4 lines, and installed list has no search highlight', async ({ page, request }) => {
  // 1. Install a provider present in the offline test fixtures cache
  const pre = await request.post(ENGINE + '/api/providers', { data: { ref: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0' } })
  expect(pre.ok()).toBeTruthy()

  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  // Verify it appears in the Installed Providers section at the top of SOURCES
  const installedRow = page.locator('#lrail .src-row:not(.cat-row)', { hasText: 'provider-aws-s3' }).first()
  await expect(installedRow).toBeVisible({ timeout: 10000 })

  // 2. Search for 's3' in the Providers Catalogue
  const catSearch = page.locator('#cat-search')
  await expect(catSearch).toBeVisible()
  await catSearch.fill('s3')

  // Locate the matching catalogue row for provider-aws-s3
  const catRow = page.locator('#lrail .cat-row', { hasText: 'provider-aws-s3' }).first()
  await expect(catRow).toBeVisible({ timeout: 10000 })

  // Verify the installed badge is present on the catalogue row
  const badge = catRow.locator('.pill')
  await expect(badge).toBeVisible()
  await expect(badge).toHaveText(/Installed \u00b7 \d+ kinds/i)

  // Contract 1: In the SOURCES rail, an installed provider entry's name (.nm) does not
  // awkwardly truncate to provider-a… when room exists; style/flex allows proper visibility.
  const nm = catRow.locator('.nm')
  await expect(nm).toBeVisible()
  await expect(nm).toHaveText('provider-aws-s3')

  const isTruncated = await nm.evaluate(el => el.scrollWidth > el.clientWidth)
  expect(isTruncated, 'catalogue entry name should not be truncated when room exists in rail').toBe(false)

  // Contract 2: The reference under the name displays cleanly without wrapping awkwardly onto four lines under badges.
  const dg = catRow.locator('.dg').first()
  await expect(dg).toBeVisible()
  await expect(dg).toContainText('provider-aws-s3')

  const lineCount = await dg.evaluate(el => {
    const computed = window.getComputedStyle(el)
    const lh = parseFloat(computed.lineHeight) || (parseFloat(computed.fontSize) * 1.3) || 12
    return Math.round(el.clientHeight / lh)
  })
  expect(lineCount, 'reference under name should display cleanly without wrapping awkwardly onto four lines').toBeLessThan(4)

  // Contract 3: Stale search-term highlighting from catalogue searches is not painted
  // inside the installed list's names (installed provider names should not highlight based on cat-search queries).
  const installedNm = installedRow.locator('.nm')
  await expect(installedNm).toBeVisible()
  const installedHtml = await installedNm.innerHTML()
  expect(installedHtml).not.toContain('<mark')
  expect(installedHtml).not.toContain('class="hl"')
  expect(installedHtml).not.toContain('class="cat-')
  expect(installedHtml).not.toContain('style="background')
  expect(await installedNm.textContent()).toBe('provider-aws-s3:v2.7.0')
})
