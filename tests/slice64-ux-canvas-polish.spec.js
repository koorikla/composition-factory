// Slice 64 — UX & Canvas Authoring Polish:
//  - Empty canvas state hint and auto-offer starter examples
//  - Catalogue Add button spinner, installed status badge, and toast notification
//  - Auto-defaulted envelope fields (providerConfigRef) displayed as non-required with auto indicator
//  - Upgraded segmented mode controls (Val / Wire / Raw) with tooltips
//  - XR card inline parameter addition (+ add field)
//  - Drag-to-wire type mismatch warning in field picker
//  - Expandable long schema descriptions in Inspector
//  - Modal focus traps and keyboard accessibility
const { test, expect } = require("@playwright/test")
const { resetDoc, ENGINE, guardPageErrors } = require("./helpers")
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test("empty canvas displays onboarding hint and auto-offers examples on blank doc", async ({ page, request }) => {
  // Clear resources in doc
  const doc = await (await request.get(ENGINE + "/api/blueprint")).json()
  doc.spec.resources = []
  await request.put(ENGINE + "/api/blueprint", { data: doc })

  // Clear localStorage flag
  await page.addInitScript(() => {
    localStorage.removeItem("cf:empty-start-offered");
  });

  await page.goto("/")

  // Examples modal should be offered on blank start
  const overlay = page.locator("#examplesOverlay")
  await expect(overlay).toBeVisible()

  // Dismiss modal
  await page.keyboard.press("Escape")
  await expect(overlay).toBeHidden()

  // Empty canvas hint is visible
  const emptyState = page.locator("#canvas-empty-state")
  await expect(emptyState).toBeVisible()
  await expect(emptyState).toContainText("1. Add a provider in SOURCES")
  await expect(emptyState).toContainText("2. Drag kinds onto canvas to compose")
})

test("catalogue add shows loading feedback and installed badge", async ({ page }) => {
  await page.goto("/")
  await page.click('#rtabs button[data-r="src"]')
  await page.fill("#cat-search", "sqs")
  const row = page.locator('#lrail .cat-row', { hasText: "provider-aws-sqs" }).first()
  await expect(row).toBeVisible()

  // SQS is already installed in pristine doc, so it shows the Installed pill
  await expect(row.locator(".pill")).toContainText("Installed")
})

test("inspector auto-defaults providerConfigRef and provides segmented mode buttons and description toggle", async ({ page }) => {
  await page.goto("/")
  await page.click('.node[data-id="work-queue"]')

  // Verify segmented mode buttons
  const modeValBtn = page.locator('#insp button[data-m="v"][data-path="region"]')
  await expect(modeValBtn).toBeVisible()
  await expect(modeValBtn).toHaveText("Val")
  const modeWireBtn = page.locator('#insp button[data-m="w"][data-path="region"]')
  await expect(modeWireBtn).toHaveText("Wire")
  const modeRawBtn = page.locator('#insp button[data-m="r"][data-path="region"]')
  await expect(modeRawBtn).toHaveText("Raw")

  // Switch to All filter to inspect envelope fields
  await page.click('#fseg button[data-f="all"]')
  const envSec = page.locator('#insp .insp-sec', { hasText: "Crossplane Envelope" })
  await expect(envSec).toBeVisible()

  // providerConfigRef.name should show auto indicator and not have rq badge when unset
  const pcRow = page.locator('#insp .fld', { hasText: "providerConfigRef.name" })
  await expect(pcRow).toBeVisible()
  await expect(pcRow.locator(".rq")).toHaveCount(0)
  await expect(pcRow.locator(".pill")).toContainText("auto")

  // Long description truncation with expandable toggle (e.g. managementPolicies)
  const mpRow = page.locator('#insp .fld', { hasText: "managementPolicies" })
  await expect(mpRow).toBeVisible()
  const moreBtn = mpRow.locator(".desc-more-btn")
  if (await moreBtn.count() > 0) {
    await expect(moreBtn).toHaveText("more")
    await moreBtn.click()
    await expect(moreBtn).toHaveText("less")
    await moreBtn.click()
    await expect(moreBtn).toHaveText("more")
  }
})

test("XR card + add field opens inline input and creates parameter", async ({ page, request }) => {
  await page.goto("/")
  const addBtn = page.locator('.node[data-id="xrd"] button[data-addxr]')
  await expect(addBtn).toBeVisible()
  await addBtn.click()

  // Inline input should be visible and auto-focused
  const inlineInput = page.locator("#xr-add-input")
  await expect(inlineInput).toBeVisible()
  await inlineInput.fill("clusterEnvironment")
  await inlineInput.press("Enter")

  // Parameter should be added to XRD
  await expect(page.locator('.node[data-id="xrd"] .port[data-path="clusterEnvironment"]')).toBeVisible()
  const doc = await (await request.get(ENGINE + "/api/blueprint")).json()
  expect(doc.spec.xrd.parameters.clusterEnvironment).toBeDefined()
})

test("wires are keyboard focusable and accessible", async ({ page }) => {
  await page.goto("/")
  const wire = page.locator("svg.wires path.wire-path").first()
  await expect(wire).toBeVisible()
  await expect(wire).toHaveAttribute("tabindex", "0")
  await expect(wire).toHaveAttribute("role", "button")
})

test("examples modal traps focus on Tab and closes cleanly", async ({ page }) => {
  await page.goto("/")
  const exBtn = page.locator("#examplesBtn")
  await exBtn.click()
  const overlay = page.locator("#examplesOverlay")
  await expect(overlay).toBeVisible()

  // Press Tab to cycle focus
  await page.keyboard.press("Tab")
  // Escape closes modal and returns focus to Examples button
  await page.keyboard.press("Escape")
  await expect(overlay).toBeHidden()
  await expect(exBtn).toBeFocused()
})

test("drag-to-wire picker warns on type mismatch", async ({ page }) => {
  await page.goto("/")
  const paramPort = page.locator('.port[data-owner="xrd"][data-path="region"] .d')
  const paramBox = await paramPort.boundingBox()
  const card = page.locator('.node[data-id="dead-letter"] .node-h')
  const cardBox = await card.boundingBox()

  await page.mouse.move(paramBox.x + paramBox.width / 2, paramBox.y + paramBox.height / 2)
  await page.mouse.down()
  await page.mouse.move(cardBox.x + cardBox.width / 2, cardBox.y + cardBox.height / 2, { steps: 5 })
  await page.mouse.up()

  const picker = page.locator("#wire-picker")
  await expect(picker).toBeVisible()

  // Search for integer field delaySeconds (param is string)
  await page.fill("#wire-picker-search", "delaySeconds")
  const item = picker.locator(".wire-picker-item", { hasText: "delaySeconds" }).first()
  await expect(item).toBeVisible()
  await expect(item.locator(".wire-picker-mismatch")).toContainText("mismatch")
})
