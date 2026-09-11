// tests/cf183-playwright-seeded-cache.spec.js
// CF-183 — Playwright e2e harness boots with pre-seeded cache fixtures,
// preventing live OCI pulls and missing-provider startup warnings.
const { test, expect } = require('@playwright/test')
const fs = require('fs')
const path = require('path')
const crypto = require('crypto')
const { execSync } = require('child_process')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

function scratchDir() {
  let toplevel = process.cwd()
  try { toplevel = execSync('git rev-parse --show-toplevel', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'ignore'] }).trim() } catch (_) {}
  const hash = crypto.createHash('sha256').update(toplevel).digest('hex').slice(0, 8)
  return path.join(toplevel, `.testrun-${hash}`)
}

test.beforeEach(async ({ request }) => { await resetDoc(request) })

test('e2e scratch cache is pre-seeded with required provider fixtures on startup', async () => {
  const cache = path.join(scratchDir(), 'cache')
  expect(fs.existsSync(cache), `expected the engine's cache under ${cache}`).toBe(true)

  // Pristine doc provider (AWS SQS) must be pre-seeded
  const sqsEntries = fs.readdirSync(cache).filter(d => d.startsWith('provider-aws-sqs-'))
  expect(sqsEntries.length, `expected provider-aws-sqs entry in ${cache}`).toBe(1)
  const sqsCRD = path.join(cache, sqsEntries[0], 'crds.json')
  expect(fs.existsSync(sqsCRD)).toBe(true)

  // S3 provider fixture must be pre-seeded for subsequent add-provider tests
  const s3Entries = fs.readdirSync(cache).filter(d => d.startsWith('provider-aws-s3-'))
  expect(s3Entries.length, `expected provider-aws-s3 entry in ${cache}`).toBe(1)
  const s3CRD = path.join(cache, s3Entries[0], 'crds.json')
  expect(fs.existsSync(s3CRD)).toBe(true)
})

test('resetDoc restores seeded provider fixtures if deleted by previous test', async ({ request }) => {
  const cache = path.join(scratchDir(), 'cache')
  const s3Dir = path.join(cache, 'provider-aws-s3-e5c6930648d0')

  // Simulate provider eviction/removal
  if (fs.existsSync(s3Dir)) {
    fs.rmSync(s3Dir, { recursive: true, force: true })
  }
  expect(fs.existsSync(s3Dir)).toBe(false)

  // resetDoc must restore the seeded fixture
  await resetDoc(request)
  expect(fs.existsSync(s3Dir)).toBe(true)
  expect(fs.existsSync(path.join(s3Dir, 'crds.json'))).toBe(true)
})
