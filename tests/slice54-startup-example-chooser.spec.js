// Slice 54 — Startup example chooser:
//  - Topbar Examples button opens the starter blueprint chooser modal
//  - Lists curated starter blueprints (IRSA, RDS, K8s App)
//  - Clicking Load Blueprint loads the blueprint into the canvas, replaces the doc (undoable)
//  - Guide tab in the left rail also offers quick-launch buttons for starter examples
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('examples button opens modal listing starter blueprints and closes on escape/close', async ({ page }) => {
  await page.goto('/')
  const btn = page.locator('#examplesBtn')
  await expect(btn).toBeVisible()

  const overlay = page.locator('#examplesOverlay')
  await expect(overlay).toBeHidden()

  await btn.click()
  await expect(overlay).toBeVisible()

  // Should list all starter examples
  await expect(page.locator('.example-card[data-id="irsa"]')).toBeVisible()
  await expect(page.locator('.example-card[data-id="rds-postgres"]')).toBeVisible()
  await expect(page.locator('.example-card[data-id="k8s-app"]')).toBeVisible()
  await expect(page.locator('.example-card[data-id="k8s-workload"]')).toBeVisible()
  await expect(page.locator('.example-card[data-id="k8s-cronjob"]')).toBeVisible()
  await expect(page.locator('.example-card[data-id="s3-bucket"]')).toBeVisible()
  await expect(page.locator('.example-card[data-id="sqs-queue"]')).toBeVisible()

  // Close via close button
  await page.click('#examplesCloseBtn')
  await expect(overlay).toBeHidden()

  // Reopen and close via Escape
  await btn.click()
  await expect(overlay).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(overlay).toBeHidden()
})

test('loading the IRSA starter blueprint replaces doc, renders nodes, and is undoable', async ({ page, request }) => {
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()

  await page.click('#examplesBtn')
  await expect(page.locator('#examplesOverlay')).toBeVisible()

  // Click Load Blueprint on the IRSA card
  const irsaCard = page.locator('.example-card[data-id="irsa"]')
  await irsaCard.locator('button[data-load-id="irsa"]').click()

  // Modal should close and IRSA resources should appear on canvas
  await expect(page.locator('#examplesOverlay')).toBeHidden()
  await expect(page.locator('.node[data-id="role"]')).toBeVisible({ timeout: 8000 })
  await expect(page.locator('.node[data-id="role-policy"]')).toBeVisible()
  await expect(page.locator('.node[data-id="sa"]')).toBeVisible()

  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.metadata.name).toBe('irsa')

  // Undo restores previous doc
  await page.click('#undoBtn')
  await expect.poll(async () => {
    const d = await (await request.get(ENGINE + '/api/blueprint')).json()
    return d.metadata.name
  }).toBe('xnotify')
})

test('guide tab offers starter blueprints that load into the canvas', async ({ page, request }) => {
  await page.goto('/')
  // Switch to Guide tab in left rail
  await page.click('#rtabs button[data-r="guide"]')

  // Find the RDS PostgreSQL example button in Guide
  const rdsGuideBtn = page.locator('button[data-guide-example="rds-postgres"]')
  await expect(rdsGuideBtn).toBeVisible()
  await rdsGuideBtn.click()

  // Canvas should now show the RDS database node
  await expect(page.locator('.node[data-id="db-instance"]')).toBeVisible({ timeout: 8000 })

  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.metadata.name).toBe('xpostgres')
})
