const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('palette kind rows are focusable buttons and can be activated via Enter to place on canvas', async ({ page }) => {
  await page.goto('/')
  const rail = page.locator('#region-palette #lrail')
  const queueRow = rail.locator('.kind[data-kind="Queue"]').first()
  await expect(queueRow).toBeVisible()
  await expect(queueRow).toHaveAttribute('tabindex', '0')
  await expect(queueRow).toHaveAttribute('role', 'button')
  await expect(queueRow).toHaveAttribute('aria-label', 'Add Queue')

  // Focus the kind row and press Enter
  await queueRow.focus()
  await page.keyboard.press('Enter')

  // Verify a new card is placed on canvas and selected
  const newCard = page.locator('#canvas .node[data-id="queue"]')
  await expect(newCard).toBeVisible()
  await expect(newCard).toHaveClass(/sel/)
  await expect(page.locator('#insp')).toContainText('queue')
})

test('clicking a palette kind row adds the resource to canvas and selects it', async ({ page }) => {
  await page.goto('/')
  const rail = page.locator('#region-palette #lrail')
  const queueRow = rail.locator('.kind[data-kind="Queue"]').first()
  await expect(queueRow).toBeVisible()

  await queueRow.click()

  const newCard = page.locator('#canvas .node[data-id="queue"]')
  await expect(newCard).toBeVisible()
  await expect(newCard).toHaveClass(/sel/)
})

test('canvas cards are focusable regions and can be selected via Enter or Space', async ({ page }) => {
  await page.goto('/')

  const card = page.locator('#canvas .node[data-id="work-queue"]')
  await expect(card).toBeVisible()
  await expect(card).toHaveAttribute('tabindex', '0')
  await expect(card).toHaveAttribute('role', 'region')
  await expect(card).toHaveAttribute('aria-label', 'Queue resource work-queue')
  await expect(card).not.toHaveClass(/sel/)

  // Focus the card and press Space
  await card.focus()
  await page.keyboard.press('Space')

  await expect(card).toHaveClass(/sel/)
  await expect(page.locator('#insp')).toContainText('work-queue')

  // Focus another card and press Enter to select it instead
  const otherCard = page.locator('#canvas .node[data-id="dead-letter"]')
  await otherCard.focus()
  await page.keyboard.press('Enter')
  await expect(otherCard).toHaveClass(/sel/)
  await expect(card).not.toHaveClass(/sel/)

  // Focus work-queue again and press Enter
  await card.focus()
  await page.keyboard.press('Enter')
  await expect(card).toHaveClass(/sel/)
  await expect(page.locator('#insp')).toContainText('work-queue')
})

test('touching or clicking any part of a card selects it immediately', async ({ page }) => {
  await page.goto('/')
  const card = page.locator('#canvas .node[data-id="work-queue"]')
  await expect(card).not.toHaveClass(/sel/)

  // Dispatch pointerdown on card body
  await card.locator('.ports').dispatchEvent('pointerdown', { pointerType: 'touch', button: 0 })
  await expect(card).toHaveClass(/sel/)
})

test('card action buttons are accessible and work when card is focused', async ({ page, request }) => {
  await page.goto('/')

  const card = page.locator('#canvas .node[data-id="dead-letter"]')
  await expect(card).toBeVisible()
  await expect(card).not.toHaveClass(/sel/)

  // When card is focused, action buttons should be visible/accessible
  await card.focus()
  const dupBtn = card.locator('[data-act="duplicate"]')
  const delBtn = card.locator('[data-act="delete"]')
  await expect(dupBtn).toBeVisible()
  await expect(delBtn).toBeVisible()
  await expect(delBtn).toHaveAttribute('aria-label', /Delete/)

  page.on('dialog', d => d.accept())
  await delBtn.click()
  await expect(page.locator('#canvas .node[data-id="dead-letter"]')).toHaveCount(0)

  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.map(r => r.name)).not.toContain('dead-letter')
})
