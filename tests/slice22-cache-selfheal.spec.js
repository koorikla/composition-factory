// Slice 22 — self-healing cache: the document load clears the origin's HTTP
// cache so no browser can keep running ghost modules (the recurring
// stale-page bug class: phantom cards, dead buttons).
const { test, expect } = require('@playwright/test')
const { ENGINE } = require('./helpers')

test('the UI server sends Clear-Site-Data on the document only', async ({ request }) => {
  const doc = await request.get(ENGINE + '/')
  expect(doc.headers()['clear-site-data']).toBe('"cache"')
  const mod = await request.get(ENGINE + '/js/main.js')
  expect(mod.headers()['clear-site-data']).toBeUndefined()
  expect(mod.headers()['cache-control']).toBe('no-store')
})

