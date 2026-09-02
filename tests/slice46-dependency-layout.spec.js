// Slice 46 — the canvas lays out the dependency tree: a resource that
// consumes another's status sits to its right (it cannot exist first), the
// XR leftmost, and no cards overlap.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  // dead-letter depends on work-queue's observed arn — the IRSA shape
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const dl = doc.spec.resources.find(r => r.name === 'dead-letter')
  dl.annotations = { 'sparky.ee/src-arn': { from: 'resources.work-queue.status.atProvider.arn' } }
  await request.put(ENGINE + '/api/blueprint', { data: doc })
})

async function rects(page) {
  return page.evaluate(() =>
    Object.fromEntries([...document.querySelectorAll('.node')].map(n => {
      const r = n.getBoundingClientRect()
      return [n.getAttribute('data-id'), { l: r.left, r: r.right, t: r.top, b: r.bottom }]
    })))
}

function overlap(a, b) {
  return a.l < b.r && b.l < a.r && a.t < b.b && b.t < a.b
}

test('fresh layout: dependents sit right of their sources, nothing overlaps', async ({ page }) => {
  await page.goto('/')
  await page.evaluate(() => localStorage.clear())
  await page.reload()
  await expect(page.locator('.node[data-id="dead-letter"]')).toBeVisible()
  const r = await rects(page)
  expect(r['xrd'].r).toBeLessThanOrEqual(r['work-queue'].l + 1)      // XR leftmost
  expect(r['work-queue'].r).toBeLessThanOrEqual(r['dead-letter'].l + 1) // dependent right of source
  const ids = Object.keys(r)
  for (let i = 0; i < ids.length; i++)
    for (let j = i + 1; j < ids.length; j++)
      expect(overlap(r[ids[i]], r[ids[j]]), ids[i] + ' overlaps ' + ids[j]).toBe(false)
})

test('reversed dependency reverses the order (not just array order)', async ({ page, request }) => {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  // flip: work-queue now depends on dead-letter
  doc.spec.resources.find(r => r.name === 'dead-letter').annotations = {}
  doc.spec.resources.find(r => r.name === 'work-queue').annotations =
    { 'sparky.ee/src-arn': { from: 'resources.dead-letter.status.atProvider.arn' } }
  const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(put.ok(), await put.text()).toBeTruthy()
  await page.goto('/')
  await page.evaluate(() => localStorage.clear())
  await page.reload()
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  const r = await rects(page)
  expect(r['dead-letter'].r).toBeLessThanOrEqual(r['work-queue'].l + 1)
})

test('the tidy control restores the dependency layout after manual mess', async ({ page }) => {
  await page.goto('/')
  // pile both queues onto the same spot
  await page.evaluate(async () => {
    const { store } = await import('/js/store.js')
    store.setPosition('work-queue', { x: 300, y: 100 })
    store.setPosition('dead-letter', { x: 305, y: 104 })
  })
  await page.click('#layout-btn')
  const r = await rects(page)
  expect(overlap(r['work-queue'], r['dead-letter'])).toBe(false)
  expect(r['work-queue'].r).toBeLessThanOrEqual(r['dead-letter'].l + 1)
})
