// tests/cf465-palette-background-providers.spec.js
// CF-465 — Canvas UI polls and reflects background provider downloads,
// dynamically populating kinds palette without manual reload.
const { test, expect } = require('@playwright/test')
const fs = require('fs')
const path = require('path')
const crypto = require('crypto')
const { spawn, execSync } = require('child_process')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

function scratchDir() {
  let toplevel = process.cwd()
  try {
    toplevel = execSync('git rev-parse --show-toplevel', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'ignore'] }).trim()
  } catch (_) {}
  const hash = crypto.createHash('sha256').update(toplevel).digest('hex').slice(0, 8)
  return path.join(toplevel, `.testrun-${hash}`)
}

const DOWNLOADING_REF = 'xpkg.upbound.io/crossplane-contrib/provider-aws-sqs:v1.16.0'

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('canvas visually displays provider download progress and reactively polls until kinds populate', async ({ page }) => {
  let providerPollCount = 0
  let kindsPollCount = 0

  await page.route('**/api/providers', async (route) => {
    providerPollCount++
    if (providerPollCount < 3) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          providers: [
            { ref: DOWNLOADING_REF, status: 'loading' }
          ]
        })
      })
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          providers: [
            { ref: DOWNLOADING_REF, digest: 'sha256:ea55e99387b7', kinds: 1 }
          ]
        })
      })
    }
  })

  await page.route('**/api/kinds*', async (route) => {
    kindsPollCount++
    if (providerPollCount < 3) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ kinds: [] })
      })
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          kinds: [
            {
              kind: 'Queue',
              apiVersion: 'sqs.aws.upbound.io/v1beta1',
              group: 'sqs.aws.upbound.io',
              provider: DOWNLOADING_REF,
              scope: 'Namespaced',
              namespaced: true,
              required: 0
            }
          ]
        })
      })
    }
  })

  await page.goto('/')

  // Initial page loads immediately without blocking
  await expect(page.locator('#region-palette')).toBeVisible()

  // Visually displays download progress instead of "No kinds available"
  const rail = page.locator('#lrail')
  await expect(rail).not.toContainText('No kinds available')
  await expect(rail).toContainText(/downloading/i)

  // Reactively polls and populates kinds dynamically without manual reload
  const kindRow = page.locator('.kind[data-kind="Queue"]')
  await expect(kindRow).toBeVisible({ timeout: 10000 })
  expect(providerPollCount).toBeGreaterThanOrEqual(3)

  // Once download completes, download progress clears and kinds remain visible
  await expect(rail).not.toContainText(/downloading/i)
  await expect(kindRow).toBeVisible()
})

test('sources tab visually indicates downloading status for loading providers', async ({ page }) => {
  await page.route('**/api/providers', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        providers: [
          { ref: DOWNLOADING_REF, status: 'loading' }
        ]
      })
    })
  })

  await page.goto('/')
  await page.click('#rtabs button[data-r="src"]')

  const row = page.locator('#lrail .src-row[data-ref="' + DOWNLOADING_REF + '"]')
  await expect(row).toBeVisible()
  await expect(row).toHaveAttribute('data-state', 'loading')
  await expect(row).toContainText(/downloading/i)
})

test('unreferenced providers like S3 are strictly deferred and not loaded at startup', async ({ request }) => {
  const r = await request.get(ENGINE + '/api/providers')
  expect(r.ok()).toBeTruthy()
  const providers = (await r.json()).providers
  const s3 = providers.find((p) => (p.ref || '').includes('provider-aws-s3'))
  expect(s3).toBeUndefined()
})

test('unreferenced providers are strictly deferred and not loaded at startup on a blank blueprint (CF-485)', async ({ page }) => {
  const scratch = scratchDir()
  const blankDoc = path.join(scratch, 'blank-doc-cf485.cf.yaml')
  fs.writeFileSync(
    blankDoc,
    'apiVersion: factory.crossplane.io/v1alpha1\nkind: Blueprint\nmetadata:\n  name: untitled\nspec:\n  sources: []\n  xrd:\n    group: platform.example.org\n    kind: XApp\n    plural: xapps\n    version: v1alpha1\n    scope: Namespaced\n    parameters: {}\n  resources: []\n'
  )

  const child = spawn('./bin/cf', [
    'serve',
    '--addr', '127.0.0.1:0',
    '--blueprint', blankDoc,
    '--out', path.join(scratch, 'out'),
    '--cache-dir', path.join(scratch, 'cache'),
    '--lock', path.join(scratch, '.cf.lock'),
  ])

  let serverURL = ''
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('timeout waiting for cf serve')), 10000)
    child.stdout.on('data', (data) => {
      const match = data.toString().match(/listening on (http:\/\/[^\s]+)/)
      if (match) {
        serverURL = match[1]
        clearTimeout(timer)
        resolve()
      }
    })
    child.on('error', (err) => {
      clearTimeout(timer)
      reject(err)
    })
    child.on('exit', (code) => {
      clearTimeout(timer)
      reject(new Error(`cf serve exited with code ${code}`))
    })
  })

  try {
    const res = await fetch(serverURL + '/api/providers')
    expect(res.ok).toBeTruthy()
    const data = await res.json()
    const providers = data.providers || []
    const s3 = providers.find((p) => (p.ref || '').includes('provider-aws-s3'))
    expect(s3).toBeUndefined()
    const sqs = providers.find((p) => (p.ref || '').includes('provider-aws-sqs'))
    expect(sqs).toBeUndefined()
    expect(providers.length).toBe(0)

    // Canvas UI: SOURCES and KINDS must agree
    await page.addInitScript(() => {
      localStorage.setItem('cf:empty-start-offered', '1')
    })
    await page.goto(serverURL + '/')

    const overlay = page.locator('#examplesOverlay')
    if (await overlay.isVisible()) {
      await page.keyboard.press('Escape')
      await expect(overlay).toBeHidden()
    }

    await expect(page.locator('#region-palette')).toBeVisible()

    await page.click('#rtabs button[data-r="src"]')
    await expect(page.locator('#lrail')).toContainText('No sources declared')

    await page.click('#rtabs button[data-r="kinds"]')
    await expect(page.locator('.kind[data-kind="Bucket"]')).toHaveCount(0)
    await expect(page.locator('.kind[data-kind="Queue"]')).toHaveCount(0)
  } finally {
    child.kill('SIGTERM')
  }
})

