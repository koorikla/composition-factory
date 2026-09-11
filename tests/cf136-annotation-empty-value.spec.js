const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')

guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test.describe('CF-136: Annotation form empty value and toast lifecycle', () => {
  test('adding annotation with empty value retains the key, does not send invalid request, and warns in user terms', async ({ page }) => {
    let putSent = false
    await page.route('**/api/blueprint', async (route) => {
      if (route.request().method() === 'PUT') {
        putSent = true
      }
      await route.continue()
    })

    await page.goto('/')
    await page.click('.node[data-id="work-queue"] .node-h')

    const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
    await expect(sec).toBeVisible()

    const keyInput = sec.locator('input[data-ann-key]')
    const valInput = sec.locator('input[data-ann-value]')
    const addBtn = sec.locator('button[data-ann-add]')

    await keyInput.fill('eks.amazonaws.com/role-arn')
    // Leave value blank and click Add
    putSent = false
    await addBtn.click()

    // 1. Request must not be sent with an invalid empty value
    expect(putSent).toBe(false)

    // 2. The key input must not be discarded
    await expect(keyInput).toHaveValue('eks.amazonaws.com/role-arn')

    // 3. User-facing error / guidance must be shown without DSL mode jargon
    const warn = page.locator('#insp .warnbar, #canvas-error-toast .toast-msg')
    await expect(warn.first()).toBeVisible()
    const warnText = await warn.first().textContent()
    expect(warnText).not.toMatch(/set exactly one of from, value, raw or template/i)
    expect(warnText).toMatch(/value|placeholder/i)
  })

  test('error text does not leak DSL mode names to the user', async ({ page }) => {
    await page.goto('/')

    // Dispatch error with DSL jargon to store
    await page.evaluate(() => {
      window.store.emit('error', {
        message: 'resource "service-account" annotation "eks.amazonaws.com/role-arn": set exactly one of from, value, raw or template (got 0)',
      })
    })

    const toast = page.locator('#canvas-error-toast')
    await expect(toast).toBeVisible()
    const text = await toast.locator('.toast-msg').textContent()
    expect(text).not.toMatch(/set exactly one of from, value, raw or template/i)
    expect(text).not.toMatch(/got 0/i)
  })

  test('multi-mode error text does not leak DSL mode names', async ({ page }) => {
    await page.goto('/')

    await page.evaluate(() => {
      window.store.emit('error', {
        message: 'resource "db" field "engine": set exactly one of from, value, raw or template (got 2)',
      })
    })

    const toast = page.locator('#canvas-error-toast')
    await expect(toast).toBeVisible()
    const text = await toast.locator('.toast-msg').textContent()
    expect(text).not.toMatch(/set exactly one of from, value, raw or template/i)
    expect(text).toContain('set only one value or wire (got 2)')
  })

  test('toast does not outlive the next successful action', async ({ page, request }) => {
    await page.goto('/')
    await page.click('.node[data-id="work-queue"] .node-h')

    // Trigger an error toast
    await page.evaluate(() => {
      window.store.emit('error', {
        message: 'resource "work-queue" annotation "test": error occurred',
      })
    })

    const toast = page.locator('#canvas-error-toast')
    await expect(toast).toBeVisible()

    // Perform a successful action: add an annotation with a valid value
    const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
    await sec.locator('input[data-ann-key]').fill('sparky.ee/role')
    await sec.locator('input[data-ann-value]').fill('worker')
    await sec.locator('button[data-ann-add]').click()

    // Toast must be removed on successful action
    await expect(toast).not.toBeVisible()

    // Verify document was persisted
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
      const r = doc.spec.resources.find(x => x.name === 'work-queue')
      return r.annotations && r.annotations['sparky.ee/role']
        ? r.annotations['sparky.ee/role'].value : null
    }).toBe('worker')
  })

  test('pressing Enter on empty value retains key and warns; pressing Enter on valid value submits and clears toast', async ({ page, request }) => {
    await page.goto('/')
    await page.click('.node[data-id="work-queue"] .node-h')

    const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i })
    await expect(sec).toBeVisible()

    const keyInput = sec.locator('input[data-ann-key]')
    const valInput = sec.locator('input[data-ann-value]')

    await keyInput.fill('sparky.ee/owner')
    await keyInput.press('Enter')

    // Key retained, warning displayed
    await expect(keyInput).toHaveValue('sparky.ee/owner')
    const warn = page.locator('#insp .warnbar, #canvas-error-toast .toast-msg')
    await expect(warn.first()).toBeVisible()

    // Now fill value and press Enter
    await valInput.fill('infra')
    await valInput.press('Enter')

    // Toast cleared, annotation persisted
    const toast = page.locator('#canvas-error-toast')
    await expect(toast).not.toBeVisible()

    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
      const r = doc.spec.resources.find(x => x.name === 'work-queue')
      return r.annotations && r.annotations['sparky.ee/owner']
        ? r.annotations['sparky.ee/owner'].value : null
    }).toBe('infra')
  })
})
