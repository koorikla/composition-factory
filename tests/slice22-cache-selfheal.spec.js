// Slice 22 — self-healing cache: the document load clears the origin's HTTP
// cache so no browser can keep running ghost modules (the recurring
// stale-page bug class: phantom cards, dead buttons).
const { test, expect } = require('@playwright/test')

test('serve.py sends Clear-Site-Data on the document only', async ({ request }) => {
  const doc = await request.get('http://127.0.0.1:5180/')
  expect(doc.headers()['clear-site-data']).toBe('"cache"')
  const mod = await request.get('http://127.0.0.1:5180/js/main.js')
  expect(mod.headers()['clear-site-data']).toBeUndefined()
  expect(mod.headers()['cache-control']).toBe('no-store')
})

test('the embedded UI sends it too', async ({ request }) => {
  const doc = await request.get('http://127.0.0.1:8080/')
  expect(doc.headers()['clear-site-data']).toBe('"cache"')
  const mod = await request.get('http://127.0.0.1:8080/js/main.js')
  expect(mod.headers()['clear-site-data']).toBeUndefined()
})
