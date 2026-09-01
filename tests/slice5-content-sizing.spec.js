// Slice 5 — names size their boxes: no clipped field names on cards, no
// mangled long kind names in the palette.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running on 8080')
})

test('every field label on every card is fully visible', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
  const clipped = await page.evaluate(() => {
    const bad = []
    document.querySelectorAll('.node .port .nm')
      .forEach(el => {
        if (el.scrollWidth > el.clientWidth + 1) bad.push(el.textContent.trim() + ' (' + el.scrollWidth + '>' + el.clientWidth + ')')
      })
    return bad
  })
  expect(clipped).toEqual([])
})

test('long palette kind names ellipsize cleanly and carry the full name as a title', async ({ page }) => {
  await page.goto('/')
  const row = page.locator('#lrail .kind[data-kind="BucketAccelerateConfiguration"]').first()
  await expect(row).toBeVisible()
  const state = await row.evaluate(el => {
    const name = el.querySelector('.nm') || el
    const cs = getComputedStyle(name)
    const badge = el.querySelector('.rq, [class*="req"], .n')
    const nameR = name.getBoundingClientRect()
    const badgeR = badge ? badge.getBoundingClientRect() : null
    return {
      overflow: cs.textOverflow,
      title: name.title || el.title,
      overlaps: badgeR ? nameR.right > badgeR.left + 1 : false,
    }
  })
  expect(state.overflow).toBe('ellipsis')
  expect(state.title).toBe('BucketAccelerateConfiguration')
  expect(state.overlaps).toBe(false)
})
