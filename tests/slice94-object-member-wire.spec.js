const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers')
guardPageErrors()

test.describe('Object parameter member wire rendering (CF-387, #278)', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request)
  })

  test('canvas renders wire connecting object parameter member to resource field', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    doc.spec.xrd.parameters = doc.spec.xrd.parameters || {}
    doc.spec.xrd.parameters.dbConfig = {
      type: 'object',
      required: true,
      properties: {
        host: { type: 'string', required: true }
      }
    }
    const res = doc.spec.resources[0]
    res.fields = res.fields || {}
    res.fields['region'] = { from: 'params.dbConfig.host', value: '', raw: '' }

    const put = await request.put(ENGINE + '/api/blueprint', { data: doc })
    if (!put.ok()) {
      throw new Error('PUT blueprint failed: ' + (await put.text()))
    }

    await page.goto('/')
    await canvasSettled(page)

    // The wire must be drawn on canvas connecting the XRD card to the resource
    const expectedTitle = `$dbConfig.host \u2192 ${res.name}.region`
    const wirePath = page.locator(`svg.wires path.wire-path[title*="${expectedTitle}"]`)
    await expect(wirePath).toBeVisible({ timeout: 2000 })

    const wireHit = page.locator(`svg.wires path.wire-hit[title*="${expectedTitle}"]`)
    await expect(wireHit).toHaveCount(1)
  })
})
