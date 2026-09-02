// Slice 23 — the store must serialize mutations: two rapid operations may
// never lose each other's changes (the ghost-resurrection bug: a delete
// undone by a concurrent edit cloned from the pre-delete doc).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('a delete and an edit fired back-to-back both survive', async ({ page, request }) => {
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  await page.evaluate(async () => {
    const { store } = await import('/js/store.js')
    const del = store.replaceDoc(d => {
      d.spec.resources = d.spec.resources.filter(r => r.name !== 'work-queue')
    })
    const edit = store.replaceDoc(d => {  // no await between: overlapping ops
      const r = d.spec.resources.find(x => x.name === 'dead-letter')
      r.fields.maxMessageSize = { value: '9999' }
    })
    await Promise.all([del, edit])
  })
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const names = doc.spec.resources.map(r => r.name)
  expect(names).not.toContain('work-queue')      // the delete survived
  const dl = doc.spec.resources.find(r => r.name === 'dead-letter')
  expect(dl.fields.maxMessageSize).toMatchObject({ value: '9999' }) // so did the edit
})

test('param add racing a field edit loses neither', async ({ page, request }) => {
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  await page.evaluate(async () => {
    const { store } = await import('/js/store.js')
    const a = store.addParameter('racer', { type: 'string' })
    const b = store.replaceDoc(d => {
      d.spec.resources.find(x => x.name === 'work-queue').fields.delaySeconds = { value: '5' }
    })
    await Promise.all([a, b])
  })
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.xrd.parameters.racer).toBeTruthy()
  const wq = doc.spec.resources.find(r => r.name === 'work-queue')
  expect(wq.fields.delaySeconds).toMatchObject({ value: '5' })
})
