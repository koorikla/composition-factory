// Every test starts from the pristine xnotify doc — and the suite runs
// against ITS OWN engine started by playwright.config's
// webServer with a scratch blueprint, never the one a human is using on
// 8080. Isolation is structural, not manners.
const { test } = require('@playwright/test')
const pristine = require('./fixtures/pristine-doc.json')
const crypto = require('crypto')
const fs = require('fs')
const { execSync } = require('child_process')

if (!fs.existsSync('.testrun')) {
  fs.mkdirSync('.testrun', { recursive: true })
}

function getEngineURL() {
  if (process.env.CF_E2E_PORT) {
    return `http://127.0.0.1:${process.env.CF_E2E_PORT}`
  }
  let toplevel = process.cwd()
  try {
    toplevel = execSync('git rev-parse --show-toplevel', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'ignore'] }).trim()
  } catch (_) {}
  const hash = crypto.createHash('sha256').update(toplevel).digest('hex').slice(0, 8)
  const port = 18000 + (parseInt(hash, 16) % 10000)
  return `http://127.0.0.1:${port}`
}

const ENGINE = getEngineURL()

async function resetDoc(request) {
  let api
  try { api = await request.get(ENGINE + '/api/kinds') } catch (e) { api = null }
  test.skip(!api || !api.ok(), `cf serve is not running on ${ENGINE}`)

  const r = await request.put(ENGINE + '/api/blueprint', { data: pristine })
  if (!r.ok()) throw new Error('resetDoc failed: ' + (await r.text()))
}

module.exports = { resetDoc, ENGINE }

// Any uncaught error in the page fails the test that produced it. Without
// this, a broken module import or a ReferenceError inside an event handler
// leaves the suite green while the canvas is dead — which is exactly how a
// duplicate `import { esc }` and a missing `startDrag` import shipped.
function guardPageErrors() {
  const errors = []
  test.beforeEach(async ({ page }) => {
    errors.length = 0
    page.on('pageerror', (e) => errors.push(e))
  })
  test.afterEach(async () => {
    if (errors.length) {
      throw new Error('uncaught page error(s):\n' + errors.map((e) => '  ' + (e.message || String(e))).join('\n'))
    }
  })
}

module.exports.guardPageErrors = guardPageErrors
