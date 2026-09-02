// Slice 20 — provider detail: clicking a SOURCES row reveals the full
// registry ref and per-kind checkboxes that filter the KINDS rail,
// persisted across reloads.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request, page }) => {
  await resetDoc(request)
  await page.goto('/')
  await page.evaluate(() => localStorage.removeItem('cf-hidden-kinds'))
})

test('a provider row expands to full ref and kind checkboxes', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await page.click('#lrail .src-row', { position: { x: 40, y: 10 } })
  const detail = page.locator('#lrail .src-detail')
  await expect(detail).toContainText('ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0')
  await expect(detail.locator('input[data-pick-kind="Queue"][data-av*=".m."]')).toBeChecked()
  await expect(detail.locator('input[data-pick-kind="Queue"]')).toHaveCount(2) // both scope variants listed
})

test('unchecking a kind hides it from KINDS and survives reload', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  await page.click('#lrail .src-row', { position: { x: 40, y: 10 } })
  await page.uncheck('#lrail .src-detail input[data-pick-kind="QueuePolicy"][data-av*=".m."]')
  await page.click('#rtabs button[data-r="kinds"]')
  const nsGroup = page.locator('#lrail')
  await expect(nsGroup.locator('.kind[data-kind="Queue"]').first()).toBeVisible()
  await expect(nsGroup.locator('.kind[data-kind="QueuePolicy"][data-av*=".m."]')).toHaveCount(0)
  await page.click('#lrail [data-grp-toggle="sqs.aws.upbound.io"]')
  await expect(nsGroup.locator('.kind[data-kind="QueuePolicy"]:not([data-av*=".m."])').first()).toBeVisible() // cluster variant untouched
  await page.reload()
  await expect(page.locator('#lrail .kind[data-kind="Queue"]').first()).toBeVisible()
  await expect(page.locator('#lrail .kind[data-kind="QueuePolicy"][data-av*=".m."]')).toHaveCount(0)
  // restore for later tests
  await page.click('#rtabs button[data-r="src"]')
  await page.click('#lrail .src-row', { position: { x: 40, y: 10 } })
  await page.check('#lrail .src-detail input[data-pick-kind="QueuePolicy"][data-av*=".m."]')
})

test('the native k8s group has a row and a hide-all toggle', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')
  const k8sRow = page.locator('#lrail .src-row', { hasText: 'k8s' }).first()
  await expect(k8sRow).toBeVisible()
  await k8sRow.click({ position: { x: 40, y: 10 } })
  await page.uncheck('#lrail .src-detail input[data-pick-all]')
  await page.click('#rtabs button[data-r="kinds"]')
  await expect(page.locator('#lrail .kind[data-provider="k8s"]')).toHaveCount(0)
  await expect(page.locator('#lrail .kind[data-kind="Queue"]').first()).toBeVisible()
  await page.reload()
  await expect(page.locator('#lrail .kind[data-provider="k8s"]')).toHaveCount(0)
  await page.click('#rtabs button[data-r="src"]')
  await page.locator('#lrail .src-row', { hasText: 'k8s' }).first().click({ position: { x: 40, y: 10 } })
  await page.check('#lrail .src-detail input[data-pick-all]')
})
