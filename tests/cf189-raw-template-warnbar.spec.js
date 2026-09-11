const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')

guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test.describe('CF-189: Raw template warnbar accuracy, grammar, links, and complete count', () => {
  test('singular raw field: warns of CRD schema bypass with correct grammar, names and links the field', async ({ page }) => {
    // Pristine doc has exactly 1 raw field: dead-letter.tags
    await page.goto('/')

    const warn = page.locator('#region-output #warn, #warn')
    await expect(warn).toBeVisible()

    const warnText = await warn.textContent()
    // 1. Must NOT claim the canvas cannot validate raw fields (stale claim)
    expect(warnText).not.toMatch(/the canvas can show them but not validate them/i)

    // 2. Must state what is actually not checked: CRD schema validation
    expect(warnText).toMatch(/crd schema/i)

    // 3. Must agree in grammatical number for 1 field: "1 field uses", "it" (not "1 field use", not "them")
    expect(warnText).toMatch(/1 field uses a raw template/i)
    expect(warnText).toMatch(/guards it/i)
    expect(warnText).not.toMatch(/1 field use\b/i)

    // 4. Must name and link the field
    const fieldBtn = warn.locator('.warnbar-field')
    await expect(fieldBtn).toHaveCount(1)
    await expect(fieldBtn).toContainText('dead-letter.tags')

    // 5. Clicking the field link selects the resource in the store
    await fieldBtn.click()
    const selectedResource = await page.evaluate(() => window.store.state.selectedResource)
    expect(selectedResource).toBe('dead-letter')
  })

  test('plural raw fields: agrees in number, names and links all fields', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const wq = doc.spec.resources.find(r => r.name === 'work-queue')
    wq.fields.tags = { raw: '{tier: "front"}' }
    const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
    expect(put.ok()).toBeTruthy()

    await page.goto('/')

    const warn = page.locator('#region-output #warn, #warn')
    await expect(warn).toBeVisible()

    const warnText = await warn.textContent()
    // Grammatical number agreement for plural: "2 fields use", "them"
    expect(warnText).toMatch(/2 fields use raw templates/i)
    expect(warnText).toMatch(/guards them/i)

    // All fields named and linked
    const fieldBtns = warn.locator('.warnbar-field')
    await expect(fieldBtns).toHaveCount(2)
    const btnTexts = await fieldBtns.allTextContents()
    expect(btnTexts).toContain('dead-letter.tags')
    expect(btnTexts).toContain('work-queue.tags')
  })

  test('counts raw fields in envelope and annotations as well as fields', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    // Remove tags from dead-letter so there are no raw fields in spec.resources[].fields
    const dl = doc.spec.resources.find(r => r.name === 'dead-letter')
    delete dl.fields.tags

    const wq = doc.spec.resources.find(r => r.name === 'work-queue')
    // Put raw in envelope and annotations only
    wq.envelope = {
      managementPolicies: { raw: "['Observe']" }
    }
    wq.annotations = {
      'sparky.ee/notify-mode': { raw: "'{{ $xr }}'" }
    }
    const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
    expect(put.ok()).toBeTruthy()

    await page.goto('/')

    const warn = page.locator('#region-output #warn, #warn')
    await expect(warn).toBeVisible()

    const warnText = await warn.textContent()
    expect(warnText).toMatch(/2 fields use raw templates/i)

    const fieldBtns = warn.locator('.warnbar-field')
    await expect(fieldBtns).toHaveCount(2)
    const btnTexts = await fieldBtns.allTextContents()
    expect(btnTexts.some(t => t.includes('work-queue.envelope.managementPolicies'))).toBe(true)
    expect(btnTexts.some(t => t.includes('work-queue.annotations[sparky.ee/notify-mode]'))).toBe(true)
  })

  test('zero raw fields hides warnbar', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const dl = doc.spec.resources.find(r => r.name === 'dead-letter')
    delete dl.fields.tags
    const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
    expect(put.ok()).toBeTruthy()

    await page.goto('/')
    const warn = page.locator('#region-output #warn')
    await expect(warn).toBeHidden()
  })
})
