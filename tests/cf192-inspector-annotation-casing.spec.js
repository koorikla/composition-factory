// tests/cf192-inspector-annotation-casing.spec.js
// CF-192 (issue #78) — The inspector uppercases annotation keys, so a case-sensitive Kubernetes key reads wrong
//
// Contract:
// Annotation keys must be displayed verbatim wherever they are shown, with no
// case transform; if the row is too narrow, truncate with the full key reachable
// (title/expansion), not by changing the characters.

const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')

guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test.describe('CF-192: Inspector annotation key verbatim casing and reachable full key', () => {
  test('adding annotation to ServiceAccount displays key verbatim with no uppercase transform and full key in title', async ({ page, request }) => {
    // 1. Blueprint with a ServiceAccount
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    doc.spec.resources.push({
      name: 'service-account',
      kind: 'ServiceAccount',
      provider: 'k8s',
    })
    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc })
    if (!putRes.ok()) throw new Error('PUT failed: ' + (await putRes.text()))
    expect(putRes.ok()).toBeTruthy()

    await page.goto('/')

    // Select the ServiceAccount card
    await page.click('.node[data-id="service-account"] .node-h')

    const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
    await expect(sec).toBeVisible()

    // Add key eks.amazonaws.com/role-arn, value TBD
    const keyInput = sec.locator('input[data-ann-key]')
    const valInput = sec.locator('input[data-ann-value]')
    const addBtn = sec.locator('button[data-ann-add]')

    await keyInput.fill('eks.amazonaws.com/role-arn')
    await valInput.fill('TBD')
    await addBtn.click()

    // Locate the rendered key in the annotation row
    const row = sec.locator('.frow', { has: page.locator('button[data-ann-del="eks.amazonaws.com/role-arn"]') })
    await expect(row).toBeVisible()

    // The key element is the first span in the annotation row
    const keyEl = row.locator('span').first()
    await expect(keyEl).toBeVisible()

    // Contract: No case transform (getComputedStyle textTransform must not be "uppercase")
    const textTransform = await keyEl.evaluate((el) => window.getComputedStyle(el).textTransform)
    expect(textTransform).toBe('none')

    // Contract: Displayed verbatim with no uppercase
    const renderedText = await keyEl.evaluate((el) => el.innerText)
    expect(renderedText).toBe('eks.amazonaws.com/role-arn')

    // Contract: If row is narrow, truncate with the full key reachable (title)
    await expect(keyEl).toHaveAttribute('title', 'eks.amazonaws.com/role-arn')

    // Deleting annotation works
    await row.locator('button[data-ann-del="eks.amazonaws.com/role-arn"]').click()
    await expect(row).not.toBeVisible()
  })

  test('pre-existing annotations with mixed-case and lowercase keys render verbatim with title', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const wq = doc.spec.resources.find((r) => r.name === 'work-queue')
    wq.annotations = {
      'eks.amazonaws.com/role-arn': { value: 'arn:aws:iam::123456789012:role/my-role' },
      'example.com/MyCustomKey': { value: 'custom-value' },
    }
    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc })
    expect(putRes.ok()).toBeTruthy()

    await page.goto('/')
    await page.click('.node[data-id="work-queue"] .node-h')

    const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
    await expect(sec).toBeVisible()

    // Check mixed-case key
    const customRow = sec.locator('.frow', { has: page.locator('button[data-ann-del="example.com/MyCustomKey"]') })
    await expect(customRow).toBeVisible()
    const customKeyEl = customRow.locator('span.ann-key')
    const customTransform = await customKeyEl.evaluate((el) => window.getComputedStyle(el).textTransform)
    expect(customTransform).toBe('none')
    expect(await customKeyEl.evaluate((el) => el.innerText)).toBe('example.com/MyCustomKey')
    await expect(customKeyEl).toHaveAttribute('title', 'example.com/MyCustomKey')

    // Check lowercase key
    const eksRow = sec.locator('.frow', { has: page.locator('button[data-ann-del="eks.amazonaws.com/role-arn"]') })
    await expect(eksRow).toBeVisible()
    const eksKeyEl = eksRow.locator('span.ann-key')
    const eksTransform = await eksKeyEl.evaluate((el) => window.getComputedStyle(el).textTransform)
    expect(eksTransform).toBe('none')
    expect(await eksKeyEl.evaluate((el) => el.innerText)).toBe('eks.amazonaws.com/role-arn')
    await expect(eksKeyEl).toHaveAttribute('title', 'eks.amazonaws.com/role-arn')
  })

  test('preset External Name annotation renders verbatim without uppercase transform', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const wq = doc.spec.resources.find((r) => r.name === 'work-queue')
    delete wq.annotations
    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc })
    expect(putRes.ok()).toBeTruthy()

    await page.goto('/')
    await page.click('.node[data-id="work-queue"] .node-h')

    // Click + External Name preset button
    const extNameBtn = page.locator('button[data-apply-ext-name="work-queue"]')
    await expect(extNameBtn).toBeVisible()
    await extNameBtn.click()

    const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
    await expect(sec).toBeVisible()

    const extRow = sec.locator('.frow', { has: page.locator('button[data-ann-del="crossplane.io/external-name"]') })
    await expect(extRow).toBeVisible()

    const extKeyEl = extRow.locator('span.ann-key')
    const extTransform = await extKeyEl.evaluate((el) => window.getComputedStyle(el).textTransform)
    expect(extTransform).toBe('none')
    expect(await extKeyEl.evaluate((el) => el.innerText)).toBe('crossplane.io/external-name')
    await expect(extKeyEl).toHaveAttribute('title', 'crossplane.io/external-name')
  })
})
