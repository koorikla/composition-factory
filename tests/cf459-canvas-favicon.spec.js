const { test, expect } = require('@playwright/test')
const { resetDoc, canvasSettled, guardPageErrors } = require('./helpers')

test.describe('CF-459: Canvas favicon', () => {
  guardPageErrors()

  test.beforeEach(async ({ request }) => {
    await resetDoc(request)
  })

  test('declares and serves favicon cleanly without console errors or 404s', async ({ page, request }) => {
    const consoleErrors = []
    page.on('console', msg => {
      if (msg.type() === 'error') {
        consoleErrors.push(msg.text())
      }
    })

    // 1. Navigates to canvas URL and asserts no console error (specifically no 404 for favicon)
    await page.goto('/')
    await canvasSettled(page)

    const faviconErrors = consoleErrors.filter(err => /favicon|404/i.test(err))
    expect(faviconErrors).toHaveLength(0)
    expect(consoleErrors).toEqual([])

    // 2. Verifies <link rel="icon"> exists in DOM head
    const iconLink = page.locator('head link[rel="icon"]')
    await expect(iconLink).toBeAttached()
    await expect(iconLink).toHaveAttribute('type', 'image/svg+xml')
    await expect(iconLink).toHaveAttribute('href', 'favicon.svg')

    const altIconLink = page.locator('head link[rel="alternate icon"]')
    await expect(altIconLink).toBeAttached()
    await expect(altIconLink).toHaveAttribute('href', 'favicon.ico')

    // 3. Verifies GET /favicon.svg and GET /favicon.ico respond with HTTP 200 OK
    const resSvg = await request.get('/favicon.svg')
    expect(resSvg.status()).toBe(200)
    expect(resSvg.headers()['content-type']).toContain('image/svg+xml')
    const svgBody = await resSvg.text()
    expect(svgBody).toContain('<svg')
    expect(svgBody).toContain('#1358B7')

    const resIco = await request.get('/favicon.ico')
    expect(resIco.status()).toBe(200)
    const icoBody = await resIco.body()
    expect(icoBody.length).toBeGreaterThan(0)
    // ICO header magic: 0x00, 0x00, 0x01, 0x00
    expect(icoBody[0]).toBe(0)
    expect(icoBody[1]).toBe(0)
    expect(icoBody[2]).toBe(1)
    expect(icoBody[3]).toBe(0)
  })
})
