// Every test starts from the pristine xnotify doc — and the suite runs
// against ITS OWN engine (127.0.0.1:8081, started by playwright.config's
// webServer with a scratch blueprint), never the one a human is using on
// 8080. Isolation is structural, not manners.
const { test } = require('@playwright/test')
const pristine = require('./fixtures/pristine-doc.json')
const ENGINE = 'http://127.0.0.1:8081'

async function resetDoc(request) {
  let api
  try { api = await request.get(ENGINE + '/api/kinds') } catch (e) { api = null }
  test.skip(!api || !api.ok(), 'cf serve is not running on 8081')

  const r = await request.put(ENGINE + '/api/blueprint', { data: pristine })
  if (!r.ok()) throw new Error('resetDoc failed: ' + (await r.text()))
}

module.exports = { resetDoc, ENGINE }
