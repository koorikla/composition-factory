// Slice 40 — no vanishing inspector on width changes; no dying drags when
// the doc changes mid-gesture (the remaining "random mouse clutches").
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('crossing the narrow breakpoint keeps a selected inspector visible', async ({ page }) => {
  await page.setViewportSize({ width: 1200, height: 800 })
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await expect(page.locator('#insp .insp-t .k')).toContainText('Queue')
  await page.setViewportSize({ width: 840, height: 800 })   // cross into narrow
  await expect(page.locator('.pane.r')).toBeVisible()        // drawer auto-opens
  await page.setViewportSize({ width: 1200, height: 800 })  // back to desktop
  await expect(page.locator('.pane.r')).toBeVisible()
  await expect(page.locator('#insp .insp-t .k')).toContainText('Queue')
})

test('a drag survives a doc change that lands mid-gesture', async ({ page }) => {
  await page.goto('/')
  const card = page.locator('.node[data-id="dead-letter"]')
  await expect(card).toBeVisible()
  const before = await card.boundingBox()
  await page.mouse.move(before.x + 60, before.y + 8)
  await page.mouse.down()
  await page.mouse.move(before.x + 120, before.y + 60, { steps: 3 })
  // a doc mutation arrives while the button is still down (another op's
  // response, a regenerate — anything): the drag must keep working
  await page.evaluate(async () => {
    const { store } = await import('/js/store.js')
    await store.replaceDoc(d => {
      d.spec.resources.find(r => r.name === 'work-queue').fields.delaySeconds = { value: '7' }
    })
  })
  await page.mouse.move(before.x + 180, before.y + 120, { steps: 3 })
  await page.mouse.up()
  const after = await card.boundingBox()
  expect(after.x - before.x).toBeGreaterThan(90)   // the full drag landed
  expect(after.y - before.y).toBeGreaterThan(90)
})

test('a click immediately after an edit lands (no rebuild-eaten clicks)', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.click('#fseg button[data-f="all"]')
  const input = page.locator('#insp input[data-v="maxMessageSize"]')
  await input.fill('2048')
  await input.press('Tab')                          // doc change → regenerate soon
  await page.click('.node[data-id="dead-letter"] .node-h')  // immediate next click
  await expect(page.locator('.node.sel')).toHaveAttribute('data-id', 'dead-letter')
  await expect(page.locator('#insp .insp-t')).toContainText('dead-letter')
})
