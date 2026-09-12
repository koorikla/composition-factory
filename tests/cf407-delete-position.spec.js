const { test, expect } = require("@playwright/test")
const { resetDoc, guardPageErrors, canvasSettled, settledBox } = require("./helpers")

guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test.describe("CF-407: Canvas removeResource purges card positions and layout caches", () => {
  test("deleting a dragged resource card via Delete key purges position from store and localStorage", async ({ page }) => {
    await page.goto("/")
    await canvasSettled(page)

    const node = page.locator('.node[data-id="dead-letter"]')
    await expect(node).toBeVisible()
    const startPos = await node.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))

    const header = node.locator(".node-h")
    const box = await settledBox(header)

    const targetX = startPos.x + 100
    const targetY = startPos.y + 80
    const deltaX = targetX - startPos.x
    const deltaY = targetY - startPos.y

    const startX = box.x + box.width / 2
    const startY = box.y + box.height / 2

    await page.mouse.move(startX, startY)
    await page.mouse.down()
    await page.mouse.move(startX + deltaX, startY + deltaY, { steps: 5 })
    await page.mouse.up()
    await canvasSettled(page)

    // Verify position recorded in store and localStorage before deletion
    const posBefore = await page.evaluate(() => window.store.getPosition("dead-letter"))
    expect(posBefore).not.toBeNull()
    expect(posBefore.x).toBe(targetX)
    expect(posBefore.y).toBe(targetY)

    const storedBefore = await page.evaluate(() => {
      const raw = localStorage.getItem(window.store._storageKey())
      return raw ? JSON.parse(raw) : null
    })
    expect(storedBefore).not.toBeNull()
    expect(storedBefore["dead-letter"]).toEqual({ x: targetX, y: targetY })

    // Accept delete confirmation dialog and delete the card via keyboard
    page.on("dialog", dialog => dialog.accept())
    await page.click('.node[data-id="dead-letter"] .node-h')
    await page.keyboard.press("Delete")
    await canvasSettled(page)

    await expect(page.locator('.node[data-id="dead-letter"]')).toHaveCount(0)

    // Verify position is deleted from store and localStorage
    const posAfter = await page.evaluate(() => window.store.getPosition("dead-letter"))
    expect(posAfter).toBeNull()

    const inPositions = await page.evaluate(() => "dead-letter" in window.store.state.positions)
    expect(inPositions).toBe(false)

    const inPersistedKeys = await page.evaluate(() =>
      window.store.state.persistedKeys ? window.store.state.persistedKeys.has("dead-letter") : false
    )
    expect(inPersistedKeys).toBe(false)

    const storedAfter = await page.evaluate(() => {
      const raw = localStorage.getItem(window.store._storageKey())
      return raw ? JSON.parse(raw) : {}
    })
    expect(storedAfter["dead-letter"]).toBeUndefined()
  })

  test("deleting a resource card via context menu purges position from store and localStorage", async ({ page }) => {
    await page.goto("/")
    await canvasSettled(page)

    const node = page.locator('.node[data-id="work-queue"]')
    await expect(node).toBeVisible()
    const startPos = await node.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))

    const header = node.locator(".node-h")
    const box = await settledBox(header)

    const targetX = startPos.x + 80
    const targetY = startPos.y + 60
    const deltaX = targetX - startPos.x
    const deltaY = targetY - startPos.y

    const startX = box.x + box.width / 2
    const startY = box.y + box.height / 2

    await page.mouse.move(startX, startY)
    await page.mouse.down()
    await page.mouse.move(startX + deltaX, startY + deltaY, { steps: 5 })
    await page.mouse.up()
    await canvasSettled(page)

    const posBefore = await page.evaluate(() => window.store.getPosition("work-queue"))
    expect(posBefore).not.toBeNull()

    // Right-click to open context menu and click Delete
    page.on("dialog", dialog => dialog.accept())
    await page.click('.node[data-id="work-queue"] .node-h', { button: "right" })
    const menu = page.locator('#ctx-menu[role="menu"]')
    await expect(menu).toBeVisible()
    await menu.getByRole("menuitem", { name: /delete/i }).click()
    await canvasSettled(page)

    await expect(page.locator('.node[data-id="work-queue"]')).toHaveCount(0)

    const posAfter = await page.evaluate(() => window.store.getPosition("work-queue"))
    expect(posAfter).toBeNull()

    const storedAfter = await page.evaluate(() => {
      const raw = localStorage.getItem(window.store._storageKey())
      return raw ? JSON.parse(raw) : {}
    })
    expect(storedAfter["work-queue"]).toBeUndefined()
  })

  test("re-adding a deleted resource applies automatic layout rather than stale dragged coordinates", async ({ page }) => {
    await page.goto("/")
    await canvasSettled(page)

    const node = page.locator('.node[data-id="dead-letter"]')
    await expect(node).toBeVisible()
    const startPos = await node.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))

    // Drag to a custom offset
    const header = node.locator(".node-h")
    const box = await settledBox(header)
    const targetX = startPos.x + 120
    const targetY = startPos.y + 100

    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width / 2 + (targetX - startPos.x), box.y + box.height / 2 + (targetY - startPos.y), { steps: 5 })
    await page.mouse.up()
    await canvasSettled(page)

    // Delete card
    page.on("dialog", dialog => dialog.accept())
    await page.click('.node[data-id="dead-letter"] .node-h')
    await page.keyboard.press("Delete")
    await canvasSettled(page)
    await expect(page.locator('.node[data-id="dead-letter"]')).toHaveCount(0)

    // Re-add resource with same name
    await page.evaluate(() => {
      window.store.replaceDoc(draft => {
        draft.spec.resources.push({
          name: "dead-letter",
          kind: "Queue",
          provider: "ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",
          fields: {},
        })
      })
    })
    await canvasSettled(page)

    const readdedNode = page.locator('.node[data-id="dead-letter"]')
    await expect(readdedNode).toBeVisible()
    const readdedPos = await readdedNode.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))

    // Re-added card should NOT be at the stale dragged position (targetX, targetY)
    expect(readdedPos.x).not.toBe(targetX)
    expect(readdedPos.y).not.toBe(targetY)
  })
})
