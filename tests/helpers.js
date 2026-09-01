// Every test starts from the pristine xnotify doc — tests may mutate the live
// blueprint freely; isolation is restored here, not by each test's manners.
const pristine = require('./fixtures/pristine-doc.json')
const ENGINE = 'http://127.0.0.1:8080'

async function resetDoc(request) {
  const r = await request.put(ENGINE + '/api/blueprint', { data: pristine })
  if (!r.ok()) throw new Error('resetDoc failed: ' + (await r.text()))
}

module.exports = { resetDoc, ENGINE }
