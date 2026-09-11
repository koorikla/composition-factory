// CF-294 — Canvas and inspector must filter unaddressable array status paths
// Backend ParseFrom rejects array indexing like conditions[0].status.
// Canvas cards and inspector must only expose valid template-addressable status ports.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')

guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('canvas card and inspector filter out array-indexed status paths like conditions[0].status for Service', async ({ page, request }) => {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  doc.spec.resources.push({
    name: 'svc-source',
    kind: 'Service',
    provider: 'k8s',
    fields: {
      'spec.type': { value: 'ClusterIP' }
    }
  })
  const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(putRes.status()).toBe(200)

  await page.goto('/')

  const card = page.locator('.node[data-id="svc-source"]')
  await expect(card).toBeVisible()

  // 1. Canvas card must NOT render array-indexed status paths as output ports
  const cardPorts = card.locator('.port.status')
  const cardPortPaths = await cardPorts.evaluateAll(ports => ports.map(p => p.getAttribute('data-path') || ''))
  for (const p of cardPortPaths) {
    expect(p).not.toMatch(/\[\d+\]/)
    expect(p).not.toContain('conditions[0]')
    expect(p).not.toContain('ingress[0]')
  }
  const conditionPorts = card.locator('.port[data-path*="conditions"]')
  await expect(conditionPorts).toHaveCount(0)

  // 2. Select card and inspect Status Outputs
  await card.locator('.node-h').click()
  const insp = page.locator('#insp')
  await expect(insp).toBeVisible()

  // Under 'Status Outputs', array-indexed paths must not appear
  const inspCodes = insp.locator('.insp-sec code')
  const inspStatusTexts = await inspCodes.allTextContents()
  for (const t of inspStatusTexts) {
    expect(t).not.toMatch(/\[\d+\]/)
    expect(t).not.toContain('conditions[0]')
    expect(t).not.toContain('ingress[0]')
  }
})

test('managed resource with mixed status paths only exposes addressable paths (atProvider.id, atProvider.url) and remains wireable', async ({ page, request }) => {
  await page.goto('/')

  const card = page.locator('.node[data-id="work-queue"]')
  await expect(card).toBeVisible()

  // work-queue has atProvider.id / atProvider.url as addressable paths
  const idPort = card.locator('.port[data-path="status.atProvider.id"]')
  await expect(idPort).toBeVisible()

  // Array paths (e.g. conditions[0]) must not appear as ports on card
  await expect(card.locator('.port[data-path*="["]')).toHaveCount(0)
  await expect(card.locator('.port[data-path*="conditions"]')).toHaveCount(0)

  // Open inspector for work-queue
  await card.locator('.node-h').click()
  const insp = page.locator('#insp')
  await expect(insp).toBeVisible()

  // Inspector status outputs must only list valid identifiers
  const statusOutputsSec = insp.locator('.insp-sec', { hasText: 'Status Outputs' })
  await expect(statusOutputsSec).toBeVisible()
  const codes = await statusOutputsSec.locator('code').allTextContents()
  expect(codes).toContain('status.atProvider.id')
  for (const c of codes) {
    expect(c).not.toMatch(/\[\d+\]/)
    expect(c).not.toContain('conditions[0]')
  }

  // Verify wireability: select dead-letter and wire deduplicationScope from work-queue's status.atProvider.url
  await page.locator('.node[data-id="dead-letter"] .node-h').click()
  await page.click('#fseg button[data-f="all"]')
  const row = page.locator('#insp .fld', { hasText: 'deduplicationScope' }).first()
  await row.locator('button[data-m="w"]').click()
  const sel = page.locator('#insp select[data-wire="deduplicationScope"]')
  const optValue = 'resources.work-queue.status.atProvider.url'
  await expect(sel.locator(`option[value="${optValue}"]`)).toHaveCount(1)

  // Ensure array paths are NOT options in the wire dropdown
  const optValues = await sel.locator('option').evaluateAll(opts => opts.map(o => o.value))
  for (const v of optValues) {
    expect(v).not.toContain('conditions[0]')
    expect(v).not.toMatch(/\[\d+\]/)
  }

  // Wire it and verify blueprint accepts it without validation error
  await sel.selectOption(optValue)
  await expect.poll(async () => {
    const bDoc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const r = bDoc.spec.resources.find(x => x.name === 'dead-letter')
    return r && r.fields && r.fields.deduplicationScope ? r.fields.deduplicationScope.from : ''
  }).toBe(optValue)
  await expect(page.locator('#errorBanner, .toast.error')).toHaveCount(0)
})

test('resource with mixed status paths exposes addressable paths (loadBalancer.ip, atProvider.id) and filters array paths', async ({ page, request }) => {
  // Intercept GET for Service kind detail to return both addressable and array paths
  await page.route('**/api/kinds/v1/Service', async route => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        kind: 'Service',
        envelope: [],
        status: [
          { path: 'loadBalancer.ip', type: 'string' },
          { path: 'atProvider.id', type: 'string' },
          { path: 'conditions[0].status', type: 'string' },
          { path: 'ingress[0].hostname', type: 'string' }
        ]
      })
    })
  })

  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  doc.spec.resources.push({
    name: 'svc-lb',
    kind: 'Service',
    provider: 'k8s',
    fields: {
      'spec.type': { value: 'LoadBalancer' }
    }
  })
  const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(putRes.status()).toBe(200)

  await page.goto('/')

  const card = page.locator('.node[data-id="svc-lb"]')
  await expect(card).toBeVisible()

  // Addressable paths must be present on card
  const lbIpPort = card.locator('.port[data-path="status.loadBalancer.ip"]')
  await expect(lbIpPort).toBeVisible()

  // Array paths must NOT be present on card
  const condPort = card.locator('.port[data-path="status.conditions[0].status"]')
  await expect(condPort).toHaveCount(0)
  const ingressPort = card.locator('.port[data-path="status.ingress[0].hostname"]')
  await expect(ingressPort).toHaveCount(0)

  // In inspector, loadBalancer.ip should be listed under Status Outputs, but not conditions[0] or ingress[0]
  await card.locator('.node-h').click()
  const statusOutputsSec = page.locator('#insp .insp-sec', { hasText: 'Status Outputs' })
  await expect(statusOutputsSec).toBeVisible()

  const codes = await statusOutputsSec.locator('code').allTextContents()
  expect(codes).toContain('status.loadBalancer.ip')
  expect(codes).toContain('status.atProvider.id')
  for (const c of codes) {
    expect(c).not.toMatch(/\[\d+\]/)
  }

  // Inspect wire dropdown in dead-letter to ensure svc-lb array paths are filtered
  await page.locator('.node[data-id="dead-letter"] .node-h').click()
  await page.click('#fseg button[data-f="all"]')
  const row = page.locator('#insp .fld', { hasText: 'deduplicationScope' }).first()
  await row.locator('button[data-m="w"]').click()
  const sel = page.locator('#insp select[data-wire="deduplicationScope"]')
  await expect(sel.locator('option[value="resources.svc-lb.status.loadBalancer.ip"]')).toHaveCount(1)

  const optValues = await sel.locator('option').evaluateAll(opts => opts.map(o => o.value))
  for (const v of optValues) {
    expect(v).not.toContain('conditions[0]')
    expect(v).not.toContain('ingress[0]')
  }
})
