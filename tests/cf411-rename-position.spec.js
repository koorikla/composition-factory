const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors, canvasSettled, settledBox } = require('./helpers')

guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test.describe('CF-411: Canvas renameResource races doc render causing renamed cards to jump', () => {
  test('renamed resource card preserves exact coordinates and does not jump to auto-layout', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto('/')
    await canvasSettled(page)

    // Locate the card to drag and rename
    const node = page.locator('.node[data-id="dead-letter"]')
    await expect(node).toBeVisible()
    const startPos = await node.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))

    const header = node.locator('.node-h')
    const box = await settledBox(header)

    // 1. Place resource at custom canvas coordinates (600, 400)
    const targetX = 600
    const targetY = 400
    const deltaX = targetX - startPos.x
    const deltaY = targetY - startPos.y

    const startX = box.x + box.width / 2
    const startY = box.y + box.height / 2

    await page.mouse.move(startX, startY)
    await page.mouse.down()
    await page.mouse.move(startX + deltaX, startY + deltaY, { steps: 5 })
    await page.mouse.up()
    await canvasSettled(page)

    const draggedPos = await node.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))
    expect(Math.abs(draggedPos.x - targetX)).toBeLessThanOrEqual(2)
    expect(Math.abs(draggedPos.y - targetY)).toBeLessThanOrEqual(2)

    // 2. Right click dead-letter -> Rename -> enter storage
    page.on('dialog', d => d.type() === 'prompt' ? d.accept('storage') : d.accept())
    await page.click('.node[data-id="dead-letter"] .node-h', { button: 'right' })
    await page.locator('#ctx-menu').getByRole('menuitem', { name: /rename/i }).click()
    await canvasSettled(page)

    // 3. Renamed card should exist and remain at (600, 400), not jumped to auto-layout
    const renamedNode = page.locator('.node[data-id="storage"]')
    await expect(renamedNode).toBeVisible()
    const renamedPos = await renamedNode.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))

    expect(Math.abs(renamedPos.x - targetX)).toBeLessThanOrEqual(2)
    expect(Math.abs(renamedPos.y - targetY)).toBeLessThanOrEqual(2)

    // 4. Renamed card must not be added to autoPlaced
    const isAutoPlaced = await page.evaluate(() => {
      return window._canvasAutoPlaced ? window._canvasAutoPlaced.has('storage') : false
    })
    expect(isAutoPlaced).toBe(false)
  })

  test('store.renameResource preserves position before emitting doc event', async ({ page }) => {
    await page.goto('/')
    await canvasSettled(page)

    // Set custom position on work-queue
    const targetX = 450
    const targetY = 300
    await page.evaluate(({ tx, ty }) => {
      window.store.setPosition('work-queue', { x: tx, y: ty }, true)
      if (window._canvasAutoPlaced) window._canvasAutoPlaced.delete('work-queue')
    }, { tx: targetX, ty: targetY })

    // Listen for doc event and check position during the doc event callback
    const positionDuringDocEmit = await page.evaluate(async () => {
      let capturedPos = null
      const docPromise = new Promise(resolve => {
        const unsub = window.store.subscribe('doc', () => {
          capturedPos = window.store.getPosition('queue-renamed')
          unsub()
          resolve(capturedPos)
        })
      })
      await window.store.renameResource('work-queue', 'queue-renamed')
      return await docPromise
    })

    expect(positionDuringDocEmit).toEqual({ x: targetX, y: targetY })

    // Node on canvas remains at target coordinates
    const renamedNode = page.locator('.node[data-id="queue-renamed"]')
    await expect(renamedNode).toBeVisible()
    const pos = await renamedNode.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))
    expect(Math.abs(pos.x - targetX)).toBeLessThanOrEqual(2)
    expect(Math.abs(pos.y - targetY)).toBeLessThanOrEqual(2)
  })
})
