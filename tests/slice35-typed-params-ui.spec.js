// Slice 35 — typed object parameters in the GUI: declare members from the
// SHARED form, see them on the card, wire fields to params.obj.member.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('the SHARED form declares typed members and persists properties', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')
  await page.click('#param-add-btn')
  await page.fill('#param-add-name', 'tuning')
  await page.selectOption('#param-add-type', 'object')
  await page.click('#param-add-member')                       // "+ member"
  await page.fill('input[data-member-name]', 'retention')
  await page.selectOption('select[data-member-type]', 'integer')
  await page.fill('input[data-member-default]', '345600')
  await page.click('#param-add-submit')
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const p = doc.spec.xrd.parameters.tuning
    return p && p.properties && p.properties.retention
      ? p.properties.retention.type + ':' + p.properties.retention.default : 'missing'
  }).toBe('integer:345600')
  // the SHARED card lists the member
  await expect(page.locator('#lrail .card', { hasText: '$tuning' })).toContainText('retention')
  await expect(page.locator('#lrail .card', { hasText: '$tuning' })).toContainText('integer')
})

test('a field can wire to a member and the doc records params.obj.member', async ({ page, request }) => {
  await request.post(ENGINE + '/api/blueprint/parameters', {
    data: { name: 'tuning', parameter: { type: 'object', properties: { retention: { type: 'integer', default: '345600' } } } } })
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.click('#fseg button[data-f="all"]')
  await page.click('#insp button[data-m="w"][data-path="delaySeconds"]')
  const sel = page.locator('#insp select[data-wire="delaySeconds"]')
  await sel.selectOption('params.tuning.retention')
  await expect.poll(async () => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const f = doc.spec.resources.find(x => x.name === 'work-queue').fields.delaySeconds
    return f && f.from || 'unset'
  }).toBe('params.tuning.retention')
})
