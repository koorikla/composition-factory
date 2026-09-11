const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors, canvasSettled, settledBox } = require('./helpers')

guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

function rectsOverlap(r1, r2) {
  return !(
    r1.x + r1.width <= r2.x ||
    r1.x >= r2.x + r2.width ||
    r1.y + r1.height <= r2.y ||
    r1.y >= r2.y + r2.height
  )
}

async function assertNoCardOverlapsInspector(page) {
  const inspector = page.locator('#region-inspector')
  await expect(inspector).toBeVisible()
  const inspBox = await settledBox(inspector)

  const cards = page.locator('.node')
  const count = await cards.count()
  for (let i = 0; i < count; i++) {
    const card = cards.nth(i)
    const cardBox = await settledBox(card)
    const cardId = await card.getAttribute('data-id')
    const overlaps = rectsOverlap(cardBox, inspBox)
    expect(overlaps, `Card "${cardId}" bounding box overlaps inspector drawer`).toBe(false)
  }
}

test.describe('CF-158: Card positions persistence and inspector boundary constraint', () => {
  test('dragged resource card position persists across reload and does not overlap inspector', async ({ page }) => {
    await page.goto('/')
    await canvasSettled(page)

    const node = page.locator('.node[data-id="work-queue"]')
    await expect(node).toBeVisible()
    const startPos = await node.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))

    const header = node.locator('.node-h')
    const box = await settledBox(header)

    const targetX = 450
    const targetY = 320
    const deltaX = targetX - startPos.x
    const deltaY = targetY - startPos.y

    const startX = box.x + box.width / 2
    const startY = box.y + box.height / 2

    await page.mouse.move(startX, startY)
    await page.mouse.down()
    await page.mouse.move(startX + deltaX, startY + deltaY, { steps: 5 })
    await page.mouse.up()
    await canvasSettled(page)

    const afterDrag = await node.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))
    expect(Math.abs(afterDrag.x - targetX)).toBeLessThanOrEqual(2)
    expect(Math.abs(afterDrag.y - targetY)).toBeLessThanOrEqual(2)

    // Reload page
    await page.reload()
    await canvasSettled(page)

    // Position must survive reload
    const reloadedNode = page.locator('.node[data-id="work-queue"]')
    await expect(reloadedNode).toBeVisible()
    const reloadedPos = await reloadedNode.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))
    expect(Math.abs(reloadedPos.x - targetX)).toBeLessThanOrEqual(2)
    expect(Math.abs(reloadedPos.y - targetY)).toBeLessThanOrEqual(2)

    // No card bounding box overlaps inspector drawer
    await assertNoCardOverlapsInspector(page)
  })

  test('auto-layout constrains cards so no card lands under the inspector drawer', async ({ page, request }) => {
    // Setup a 3-tier dependency chain: XRD -> work-queue -> dead-letter
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
    const dl = doc.spec.resources.find(r => r.name === 'dead-letter')
    dl.annotations = { 'sparky.ee/src-arn': { from: 'resources.work-queue.status.atProvider.arn' } }
    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc })
    expect(putRes.ok()).toBeTruthy()

    await page.goto('/')
    await page.evaluate(() => localStorage.clear())
    await page.reload()
    await canvasSettled(page)

    // Assert no card overlaps inspector on auto-layout
    await assertNoCardOverlapsInspector(page)

    // Tidy button re-runs auto-layout and preserves inspector constraint
    await page.click('#layout-btn')
    await canvasSettled(page)
    await assertNoCardOverlapsInspector(page)
  })

  test('tidy control (#layout-btn) clears manual offsets from persistence and restores pure layout', async ({ page }) => {
    await page.goto('/')
    await canvasSettled(page)

    const node = page.locator('.node[data-id="work-queue"]')
    await expect(node).toBeVisible()
    const startPos = await node.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))

    // Drag to manual position (450, 320)
    const header = node.locator('.node-h')
    const box = await settledBox(header)
    const deltaX = 450 - startPos.x
    const deltaY = 320 - startPos.y
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width / 2 + deltaX, box.y + box.height / 2 + deltaY, { steps: 5 })
    await page.mouse.up()
    await canvasSettled(page)

    // Reload to confirm persistence
    await page.reload()
    await canvasSettled(page)
    const reloadedPos = await page.locator('.node[data-id="work-queue"]').evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))
    expect(Math.abs(reloadedPos.x - 450)).toBeLessThanOrEqual(2)

    // Click tidy button
    await page.click('#layout-btn')
    await canvasSettled(page)

    // Reload after tidy: card should NOT return to manual (450, 320) position
    await page.reload()
    await canvasSettled(page)
    const postTidyPos = await page.locator('.node[data-id="work-queue"]').evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))
    expect(Math.abs(postTidyPos.x - 450)).toBeGreaterThan(10)
    await assertNoCardOverlapsInspector(page)
  })
})
