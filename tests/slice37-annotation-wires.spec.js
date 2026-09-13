// Slice 37 — annotation wires render and are authorable: the IRSA arn →
// ServiceAccount annotation must be VISIBLE (teal, like status wires) and
// editable from the inspector's new annotations section.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()



test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('an annotation wire from a status output renders on the canvas', async ({ page, request }) => {
  // wire dead-letter's arn into work-queue's annotation (same mechanism as IRSA)
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const wq = doc.spec.resources.find(r => r.name === 'work-queue')
  wq.annotations = { 'sparky.ee/dlq-arn': { from: 'resources.dead-letter.status.atProvider.arn' } }
  const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(put.ok()).toBeTruthy()
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toContainText('sparky.ee/dlq-arn')
  await expect(page.locator('#wires path.wire-status')).toHaveCount(1)
})

test('the inspector lists annotations and adds one', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
  await expect(sec).toBeVisible()
  await sec.locator('input[data-ann-key]').fill('sparky.ee/team')
  await sec.locator('input[data-ann-value]').fill('platform')
  await sec.locator('button[data-ann-add]').click()
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = doc.spec.resources.find(x => x.name === 'work-queue')
    return r.annotations && r.annotations['sparky.ee/team']
      ? r.annotations['sparky.ee/team'].value : 'missing'
  }).toBe('platform')
})

test('authors an annotation wire through the inspector UI', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
  await expect(sec).toBeVisible()
  await sec.locator('input[data-ann-key]').fill('sparky.ee/dlq-arn')
  await sec.locator('input[data-ann-value]').fill('placeholder')
  await sec.locator('button[data-ann-add]').click()

  const row = sec.locator('.ann-row', { has: page.locator('span.ann-key', { hasText: 'sparky.ee/dlq-arn' }) })
  await expect(row).toBeVisible()

  // Mode buttons must be present on the annotation row
  const wireBtn = row.locator('button[data-m="w"]')
  await expect(wireBtn).toBeVisible()

  // Switch to wire mode and select the dead-letter status wire
  await wireBtn.click()
  const select = row.locator('select[data-wire="annotations.sparky.ee/dlq-arn"]')
  await expect(select).toBeVisible()
  await select.selectOption('resources.dead-letter.status.atProvider.arn')

  // Blueprint document must reflect the wire binding
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = doc.spec.resources.find(x => x.name === 'work-queue')
    return r.annotations && r.annotations['sparky.ee/dlq-arn']
      ? r.annotations['sparky.ee/dlq-arn'].from : 'missing'
  }).toBe('resources.dead-letter.status.atProvider.arn')

  // Canvas wire must render
  await expect(page.locator('#wires path.wire-status')).toHaveCount(1)

  // Unwire via chip x button
  await row.locator('.bound .x').click()
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = doc.spec.resources.find(x => x.name === 'work-queue')
    return r.annotations && r.annotations['sparky.ee/dlq-arn'] ? 'present' : 'removed'
  }).toBe('removed')
})

test('can edit literal values and raw expressions on annotation rows', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
  await expect(sec).toBeVisible()
  await sec.locator('input[data-ann-key]').fill('sparky.ee/note')
  await sec.locator('input[data-ann-value]').fill('initial')
  await sec.locator('button[data-ann-add]').click()

  const row = sec.locator('.ann-row', { has: page.locator('span.ann-key', { hasText: 'sparky.ee/note' }) })
  await expect(row).toBeVisible()

  // Edit literal value
  const valInput = row.locator('input[data-v="annotations.sparky.ee/note"]')
  await expect(valInput).toHaveValue('initial')
  await valInput.fill('updated')
  await valInput.blur()

  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = doc.spec.resources.find(x => x.name === 'work-queue')
    return r.annotations && r.annotations['sparky.ee/note'] ? r.annotations['sparky.ee/note'].value : ''
  }).toBe('updated')

  // Switch to raw mode
  await row.locator('button[data-m="r"]').click()
  const rawTa = row.locator('textarea[data-raw="annotations.sparky.ee/note"]')
  await expect(rawTa).toBeVisible()
  await rawTa.fill("'{{ $xr }}'")
  await rawTa.blur()

  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = doc.spec.resources.find(x => x.name === 'work-queue')
    return r.annotations && r.annotations['sparky.ee/note'] ? r.annotations['sparky.ee/note'].raw : ''
  }).toBe("'{{ $xr }}'")
})

test('literal repro: IRSA eks.amazonaws.com/role-arn can be wired to status output from inspector', async ({ page, request }) => {
  // Add native ServiceAccount to the blueprint
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  doc.spec.resources.push({
    name: 'service-account',
    kind: 'ServiceAccount',
    provider: 'k8s',
    fields: {},
  })
  const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(putRes.ok()).toBeTruthy()

  await page.goto('/')
  await page.click('.node[data-id="service-account"] .node-h')

  const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
  await expect(sec).toBeVisible()
  await sec.locator('input[data-ann-key]').fill('eks.amazonaws.com/role-arn')
  await sec.locator('input[data-ann-value]').fill('TBD')
  await sec.locator('button[data-ann-add]').click()

  const row = sec.locator('.ann-row', { has: page.locator('span.ann-key', { hasText: 'eks.amazonaws.com/role-arn' }) })
  await expect(row).toBeVisible()

  // Affordances: Val, Wire, Raw mode buttons
  await expect(row.locator('button[data-m="v"]')).toBeVisible()
  await expect(row.locator('button[data-m="w"]')).toBeVisible()
  await expect(row.locator('button[data-m="r"]')).toBeVisible()

  // Switch to Wire mode and wire from work-queue's status
  await row.locator('button[data-m="w"]').click()
  const select = row.locator('select[data-wire="annotations.eks.amazonaws.com/role-arn"]')
  await expect(select).toBeVisible()
  await select.selectOption('resources.work-queue.status.atProvider.arn')

  await expect.poll(async () => {
    const docAfter = await (await request.get(ENGINE + '/api/blueprint')).json()
    const sa = docAfter.spec.resources.find(x => x.name === 'service-account')
    return sa.annotations && sa.annotations['eks.amazonaws.com/role-arn']
      ? sa.annotations['eks.amazonaws.com/role-arn'].from : ''
  }).toBe('resources.work-queue.status.atProvider.arn')

  // Status wire rendered on canvas
  await expect(page.locator('#wires path.wire-status')).toHaveCount(1)
})
