// tests/cf152-failed-source-repair.spec.js
// CF-152 — A declared source that failed to load must be repairable from the
// canvas: it appears in SOURCES in a failed state with the reason and a
// remove/replace action; removing or replacing it updates spec.sources,
// regenerates and clears the chip.
const { test, expect } = require('@playwright/test')
const fs = require('fs')
const path = require('path')
const crypto = require('crypto')
const { execSync } = require('child_process')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
const pristine = require('./fixtures/pristine-doc.json')
guardPageErrors()

const GOOD = 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0'
const BAD = 'offline.invalid/crossplane-contrib/provider-aws-sqs:v9.9.9'
const REPLACEMENT = 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0'

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

// The engine's scratch blueprint — the same per-worktree path
// playwright.config.js starts `cf serve` on.
function scratchDocPath() {
  let toplevel = process.cwd()
  try {
    toplevel = execSync('git rev-parse --show-toplevel', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'ignore'] }).trim()
  } catch (_) {}
  const hash = crypto.createHash('sha256').update(toplevel).digest('hex').slice(0, 8)
  return path.join(toplevel, `.testrun-${hash}`, 'doc.cf.yaml')
}

// Every API write that declares an unfetchable source is refused (CF-087),
// so the failed-source state can only arise the way it does in the field:
// the blueprint on disk already declares it when the engine reads it. The
// engine reads the file on every request, so writing it is exactly the
// `cf serve --blueprint <file>` start the issue reproduces.
async function declareFailedSource(request) {
  const doc = JSON.parse(JSON.stringify(pristine))
  doc.spec.sources.push({ provider: BAD })
  fs.writeFileSync(scratchDocPath(), JSON.stringify(doc))
  const r = await request.get(ENGINE + '/api/blueprint')
  expect(r.ok()).toBeTruthy()
  const sources = (await r.json()).spec.sources.map((s) => s.provider)
  expect(sources).toContain(BAD)
}

async function docSources(request) {
  const r = await request.get(ENGINE + '/api/blueprint')
  expect(r.ok()).toBeTruthy()
  return (await r.json()).spec.sources.map((s) => s.provider || s.crds)
}

test('a declared source that failed to load is listed in SOURCES as failed with its reason and can be removed, which regenerates and clears the chip', async ({ page, request }) => {
  await declareFailedSource(request)
  await page.goto('/')

  const chip = page.locator('#valid')
  await expect(chip).toContainText('error', { timeout: 20000 })

  await page.click('#rtabs button[data-r="src"]')
  const failedRow = page.locator('#lrail .src-row[data-ref="' + BAD + '"]')
  await expect(failedRow).toBeVisible({ timeout: 20000 })
  await expect(failedRow).toHaveAttribute('data-state', 'failed')
  await expect(failedRow).toContainText(/failed/i)
  const reason = failedRow.locator('.src-fail-reason')
  await expect(reason).toBeVisible()
  expect((await reason.textContent()).trim().length).toBeGreaterThan(0)

  // The healthy sibling is still listed as before.
  await expect(page.locator('#lrail .src-row[data-ref="' + GOOD + '"]')).toBeVisible()

  page.on('dialog', (d) => d.accept())
  await failedRow.locator('[data-remove-ref]').click()

  await expect(page.locator('#lrail .src-row[data-ref="' + BAD + '"]')).toHaveCount(0, { timeout: 20000 })
  const sources = await docSources(request)
  expect(sources).not.toContain(BAD)
  expect(sources).toContain(GOOD)

  await expect(chip).not.toContainText('error', { timeout: 20000 })
  await expect(chip).toContainText('preview ·')
})

test('replacing a failed source from SOURCES swaps spec.sources to the new ref, regenerates and clears the chip', async ({ page, request }) => {
  await declareFailedSource(request)
  await page.goto('/')

  const chip = page.locator('#valid')
  await expect(chip).toContainText('error', { timeout: 20000 })

  await page.click('#rtabs button[data-r="src"]')
  const failedRow = page.locator('#lrail .src-row[data-ref="' + BAD + '"]')
  await expect(failedRow).toBeVisible({ timeout: 20000 })

  await failedRow.locator('[data-replace-ref]').click()
  const input = page.locator('#src-add-ref')
  await expect(input).toHaveValue(BAD)
  await input.fill(REPLACEMENT)
  await page.click('#src-add-btn')

  await expect(page.locator('#lrail .src-row[data-ref="' + BAD + '"]')).toHaveCount(0, { timeout: 30000 })
  await expect(page.locator('#lrail .src-row[data-ref="' + REPLACEMENT + '"]')).toBeVisible({ timeout: 30000 })
  await expect(page.locator('#lrail .src-row[data-ref="' + REPLACEMENT + '"]')).not.toHaveAttribute('data-state', 'failed')

  const sources = await docSources(request)
  expect(sources).not.toContain(BAD)
  expect(sources).toContain(REPLACEMENT)
  expect(sources).toContain(GOOD)

  await expect(chip).not.toContainText('error', { timeout: 20000 })
  await expect(chip).toContainText('preview ·')
})

test('the API lists a failed declared source with its reason and DELETE removes it from spec.sources', async ({ request }) => {
  await declareFailedSource(request)

  // Generate is what the canvas does first; it is where the fetch fails.
  const gen = await request.post(ENGINE + '/api/generate', { data: { write: false } })
  expect(gen.status()).toBe(400)

  const list = await request.get(ENGINE + '/api/providers')
  expect(list.ok()).toBeTruthy()
  const providers = (await list.json()).providers
  const failed = providers.find((p) => p.ref === BAD)
  expect(failed).toBeTruthy()
  expect(typeof failed.error).toBe('string')
  expect(failed.error.length).toBeGreaterThan(0)
  expect(providers.find((p) => p.ref === GOOD)).toBeTruthy()
  expect(providers.find((p) => p.ref === GOOD).error).toBeFalsy()

  const del = await request.delete(ENGINE + '/api/providers/' + encodeURIComponent(BAD))
  expect(del.status()).toBe(200)
  const remaining = (await del.json()).providers.map((p) => p.ref)
  expect(remaining).not.toContain(BAD)
  expect(remaining).toContain(GOOD)

  expect(await docSources(request)).not.toContain(BAD)

  const gen2 = await request.post(ENGINE + '/api/generate', { data: { write: false } })
  expect(gen2.ok()).toBeTruthy()
})
