// tests/cf101-e2e-engine-uses-scratch-cache.spec.js
// CF-101 — The e2e engine must run against a scratch schema cache, not the
// developer's ~/Library/Caches/compositionfactory: host state must not decide
// whether a spec passes, and the suite must not write into the host cache.
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

test('a provider added during the suite lands in the scratch cache, not the host cache', async ({ request }) => {
  const ref = 'ghcr.io/crossplane-contrib/provider-nop:v0.5.0'
  const r = await request.post(ENGINE + '/api/providers', { data: { ref } })
  expect(r.ok(), await r.text()).toBeTruthy()

  const cache = path.join(scratchDir(), 'cache')
  expect(fs.existsSync(cache), `expected the engine's cache under ${cache}`).toBe(true)
  const entries = fs.readdirSync(cache).filter(d => d.startsWith('provider-nop-'))
  expect(entries.length, `provider-nop entry in ${cache}`).toBe(1)
})
