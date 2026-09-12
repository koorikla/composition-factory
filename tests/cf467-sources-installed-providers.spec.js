// tests/cf467-sources-installed-providers.spec.js
// CF-467 — SOURCES tab lists cached providers as installed, hiding Add button and search for RDS, IAM
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

const emptyDoc = {
  apiVersion: 'factory.crossplane.io/v1alpha1',
  kind: 'Blueprint',
  metadata: { name: 'untitled' },
  spec: {
    sources: [],
    xrd: {
      group: 'platform.example.org',
      kind: 'XApp',
      plural: 'xapps',
      version: 'v1alpha1',
      scope: 'Namespaced',
      parameters: {}
    },
    resources: []
  }
}

const CACHED_PROVIDERS = [
  { ref: 'ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0', digest: 'sha256:rds123', kinds: 44 },
  { ref: 'ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0', digest: 'sha256:iam123', kinds: 30 },
  { ref: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0', digest: 'sha256:s3123', kinds: 15 }
]

test.beforeEach(async ({ page, request }) => {
  await resetDoc(request, emptyDoc)
  await page.addInitScript(() => {
    localStorage.setItem('cf:empty-start-offered', '1')
  })
})

test('cached providers on server not in spec.sources are not shown in Installed Providers and retain Add button in catalogue', async ({ page }) => {
  // Simulate server holding cached/indexed providers (e.g. from local schema cache or server index)
  await page.route('**/api/providers', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ providers: CACHED_PROVIDERS })
      })
    } else {
      await route.continue()
    }
  })

  await page.goto('/')

  // Navigate to SOURCES tab
  await page.click('#rtabs button[data-r="src"]')
  await expect(page.locator('#region-palette')).toContainText('Installed Providers')

  // Unreferenced cached providers (RDS, IAM, S3) must NOT be listed under Installed Providers
  const rdsInstalled = page.locator('#lrail .src-row:not(.cat-row)', { hasText: 'provider-aws-rds' })
  await expect(rdsInstalled).toHaveCount(0)

  const iamInstalled = page.locator('#lrail .src-row:not(.cat-row)', { hasText: 'provider-aws-iam' })
  await expect(iamInstalled).toHaveCount(0)

  const s3Installed = page.locator('#lrail .src-row:not(.cat-row)', { hasText: 'provider-aws-s3' })
  await expect(s3Installed).toHaveCount(0)

  // Installed Providers header should only count native k8s provider (1)
  const countBadge = page.locator('#lrail .grp:has-text("Installed Providers") .n')
  await expect(countBadge).toHaveText('1')

  // No non-native sources declared: "No sources declared." must be visible
  await expect(page.locator('#lrail .empty', { hasText: 'No sources declared.' })).toBeVisible()

  // In Providers Catalogue (#cat-search), search for "RDS"
  const catSearch = page.locator('#cat-search')
  await expect(catSearch).toBeVisible()
  await catSearch.fill('RDS')

  const rdsCatRow = page.locator('#lrail .cat-row', { hasText: 'provider-aws-rds' }).first()
  await expect(rdsCatRow).toBeVisible({ timeout: 10000 })
  // RDS is cached on the server, but NOT declared in doc.spec.sources:
  // it must NOT be marked as Installed, and its Add button must be visible
  await expect(rdsCatRow.locator('.pill', { hasText: /Installed/i })).toHaveCount(0)
  const rdsAddBtn = rdsCatRow.locator('button.cat-add')
  await expect(rdsAddBtn).toBeVisible()
  await expect(rdsAddBtn).toBeEnabled()

  // In Providers Catalogue, search for "iam"
  await catSearch.fill('iam')
  const iamCatRow = page.locator('#lrail .cat-row', { hasText: 'provider-aws-iam' }).first()
  await expect(iamCatRow).toBeVisible({ timeout: 10000 })
  await expect(iamCatRow.locator('.pill', { hasText: /Installed/i })).toHaveCount(0)
  const iamAddBtn = iamCatRow.locator('button.cat-add')
  await expect(iamAddBtn).toBeVisible()
  await expect(iamAddBtn).toBeEnabled()
})

test('clicking Add on a catalogue provider adds it to spec.sources and updates the UI', async ({ page, request }) => {
  await page.goto('/')

  await page.click('#rtabs button[data-r="src"]')
  await expect(page.locator('#region-palette')).toContainText('Installed Providers')

  const catSearch = page.locator('#cat-search')
  await expect(catSearch).toBeVisible()
  await catSearch.fill('s3')

  const s3CatRow = page.locator('#lrail .cat-row', { hasText: 'provider-aws-s3' }).first()
  await expect(s3CatRow).toBeVisible({ timeout: 10000 })
  const s3AddBtn = s3CatRow.locator('button.cat-add')
  await expect(s3AddBtn).toBeVisible()

  // Click Add
  await s3AddBtn.click()

  // Catalogue row updates to show Installed pill
  await expect(s3CatRow.locator('.pill', { hasText: /Installed/i })).toBeVisible({ timeout: 15000 })
  await expect(s3CatRow.locator('button.cat-add')).toHaveCount(0)

  // Installed Providers list now contains provider-aws-s3
  const s3Installed = page.locator('#lrail .src-row:not(.cat-row)', { hasText: 'provider-aws-s3' })
  await expect(s3Installed).toBeVisible({ timeout: 10000 })

  // "No sources declared." is no longer visible
  await expect(page.locator('#lrail .empty', { hasText: 'No sources declared.' })).toHaveCount(0)

  // Verify spec.sources on the server contains the added provider
  const bp = await (await request.get(ENGINE + '/api/blueprint')).json()
  const sources = (bp.spec && bp.spec.sources || []).map(s => s.provider)
  expect(sources).toContain('ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0')
})

test('declared source in blueprint is marked installed, while undeclared cached provider remains addable', async ({ page, request }) => {
  // Reset to pristine doc which declares provider-aws-sqs
  await resetDoc(request)
  await page.goto('/')

  await page.click('#rtabs button[data-r="src"]')
  await expect(page.locator('#region-palette')).toContainText('Installed Providers')

  // Declared provider SQS is listed under Installed Providers
  const sqsInstalled = page.locator('#lrail .src-row:not(.cat-row)', { hasText: 'provider-aws-sqs' })
  await expect(sqsInstalled).toBeVisible({ timeout: 10000 })

  // Undeclared provider S3 is NOT listed under Installed Providers
  const s3Installed = page.locator('#lrail .src-row:not(.cat-row)', { hasText: 'provider-aws-s3' })
  await expect(s3Installed).toHaveCount(0)

  // Because a non-native source is declared, "No sources declared." is NOT visible
  await expect(page.locator('#lrail .empty', { hasText: 'No sources declared.' })).toHaveCount(0)

  // In Providers Catalogue, search for SQS
  const catSearch = page.locator('#cat-search')
  await expect(catSearch).toBeVisible()
  await catSearch.fill('sqs')

  const sqsCatRow = page.locator('#lrail .cat-row', { hasText: 'provider-aws-sqs' }).first()
  await expect(sqsCatRow).toBeVisible({ timeout: 10000 })
  // SQS is declared, so it must be marked as Installed
  await expect(sqsCatRow.locator('.pill', { hasText: /Installed/i })).toBeVisible()
  await expect(sqsCatRow.locator('button.cat-add')).toHaveCount(0)

  // In Providers Catalogue, search for S3 (which is cached on the server, but undeclared in the doc)
  await catSearch.fill('s3')
  const s3CatRow = page.locator('#lrail .cat-row', { hasText: 'provider-aws-s3' }).first()
  await expect(s3CatRow).toBeVisible({ timeout: 10000 })
  // S3 is NOT declared, so it must have an Add button and NOT be marked as Installed
  await expect(s3CatRow.locator('.pill', { hasText: /Installed/i })).toHaveCount(0)
  await expect(s3CatRow.locator('button.cat-add')).toBeVisible()
})
