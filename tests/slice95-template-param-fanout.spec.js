const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers')
guardPageErrors()

test.describe('CF-377 — fanOut template parameter reference', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request)
  })

  test('parameter referenced in spec.templates reflects fan-out count >= 1 and prompts unwire on delete', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    doc.spec.xrd = doc.spec.xrd || {}
    doc.spec.xrd.parameters = doc.spec.xrd.parameters || {}
    doc.spec.xrd.parameters.policy = {
      type: 'string',
      default: 'standard'
    }
    delete doc.spec.conventions
    doc.spec.templates = {
      'cf.policy': '{{ .spec.policy }}'
    }

    const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
    if (!put.ok()) {
      throw new Error('PUT blueprint failed: ' + (await put.text()))
    }

    await page.goto('/')
    await canvasSettled(page)

    // Check shared rail / palette shows 1 bound for policy
    await page.click('#rtabs button[data-r="shared"]')
    const card = page.locator('.card:has([data-param-del="policy"])')
    await expect(card.locator('.bind')).toHaveText('1 bound')

    // Click delete on policy parameter; captures window.confirm dialog triggered by fanOut > 0
    let dialogMessage = ''
    page.on('dialog', async (d) => {
      dialogMessage = d.message()
      await d.accept()
    })

    await page.click('[data-param-del="policy"]')

    // Confirm dialog / prompt should appear because fanOut > 0
    await expect.poll(() => dialogMessage).toBe('Parameter "policy" is wired into 1 field. Delete it and unwire all referencing fields?')

    // Verify deletion succeeded cleanly: parameter deleted and template cleaned without 409 error
    await expect(page.locator('#region-palette .warnbar')).toHaveCount(0)
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`)
      const updated = await res.json()
      const p = (updated.spec.xrd && updated.spec.xrd.parameters) || {}
      const tmpls = updated.spec.templates || {}
      return {
        hasParam: 'policy' in p,
        hasTemplate: 'cf.policy' in tmpls
      }
    }).toEqual({
      hasParam: false,
      hasTemplate: false
    })
  })
})
