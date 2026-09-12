// CF-418 (issue #310) — Canvas keyboard paste fails when no resource card is selected or when XRD/Environment node is active
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers')
const pristine = require('./fixtures/pristine-doc.json')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('paste duplicates copied resource when selection is cleared via Escape', async ({ page, request }) => {
  await page.goto('/')
  await canvasSettled(page)

  // 1. Select and copy work-queue
  await page.click('.node[data-id="work-queue"] .node-h')
  await expect(page.locator('.node[data-id="work-queue"]')).toHaveClass(/sel/)
  await page.keyboard.press('ControlOrMeta+c')

  // 2. Clear selection via Escape
  await page.keyboard.press('Escape')
  await expect(page.locator('.node.sel')).toHaveCount(0)

  // 3. Paste with no active selection
  await page.keyboard.press('ControlOrMeta+v')
  await expect(page.locator('.node[data-id="work-queue-2"]')).toBeVisible()

  // 4. Verify in doc
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const dupe = doc.spec.resources.find(r => r.name === 'work-queue-2')
  expect(dupe).toBeTruthy()
  expect(dupe.kind).toBe('Queue')
})

test('paste duplicates copied resource when Composite (XRD) node is selected', async ({ page, request }) => {
  await page.goto('/')
  await canvasSettled(page)

  // 1. Select and copy work-queue
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.keyboard.press('ControlOrMeta+c')

  // 2. Select Composite node (xrd)
  await page.click('.node[data-id="xrd"] .node-h')
  await expect(page.locator('.node[data-id="xrd"]')).toHaveClass(/sel/)

  // 3. Paste while XRD node is active
  await page.keyboard.press('ControlOrMeta+v')
  await expect(page.locator('.node[data-id="work-queue-2"]')).toBeVisible()

  // 4. Verify in doc
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const dupe = doc.spec.resources.find(r => r.name === 'work-queue-2')
  expect(dupe).toBeTruthy()
  expect(dupe.kind).toBe('Queue')
})

test('paste duplicates copied resource when Environment node is selected', async ({ page, request }) => {
  const docWithEnv = JSON.parse(JSON.stringify(pristine))
  docWithEnv.spec.environment = {
    clusterName: {
      type: 'string',
      description: 'Target EKS cluster name'
    }
  }
  const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv })
  expect(putRes.ok()).toBeTruthy()

  await page.goto('/')
  await canvasSettled(page)

  // 1. Select and copy work-queue
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.keyboard.press('ControlOrMeta+c')

  // 2. Select Environment node
  await page.click('.node[data-id="environment"] .node-h')
  await expect(page.locator('.node[data-id="environment"]')).toHaveClass(/sel/)

  // 3. Paste while Environment node is active
  await page.keyboard.press('ControlOrMeta+v')
  await expect(page.locator('.node[data-id="work-queue-2"]')).toBeVisible()

  // 4. Verify in doc
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const dupe = doc.spec.resources.find(r => r.name === 'work-queue-2')
  expect(dupe).toBeTruthy()
  expect(dupe.kind).toBe('Queue')
})

test('existing copy and delete behavior on XRD node is preserved', async ({ page, request }) => {
  await page.goto('/')
  await canvasSettled(page)

  // Select Composite node (xrd)
  await page.click('.node[data-id="xrd"] .node-h')
  await expect(page.locator('.node[data-id="xrd"]')).toHaveClass(/sel/)

  // Pressing Delete on XRD node does nothing
  await page.keyboard.press('Delete')
  await expect(page.locator('.node[data-id="xrd"]')).toBeVisible()

  // Pressing copy on XRD node does not populate copiedResource (paste still does nothing if buffer was empty)
  await page.keyboard.press('ControlOrMeta+c')
  await page.keyboard.press('ControlOrMeta+v')

  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.xrd).toBeTruthy()
  expect(doc.spec.resources.length).toBe(2)
})
