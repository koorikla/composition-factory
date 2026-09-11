const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('CF-155: renaming a parameter preserves its row index while editing so next clicks land on the same row', async ({ page, request }) => {
  await page.goto('/')

  // Select XRD
  await page.click('.node[data-id="xrd"] .node-h')

  // Initial order of parameter inputs: providerName (0), region (1), retention (2)
  const initialNames = await page.locator('#insp input[data-pn]').evaluateAll(inputs => inputs.map(i => i.value))
  expect(initialNames).toEqual(['providerName', 'region', 'retention'])

  // Rename "region" to "awsRegion"
  const regionInput = page.locator('#insp input[data-pn="region"]')
  await regionInput.fill('awsRegion')
  await regionInput.press('Enter')

  // Wait for rename to be reflected in inspector
  const renamedInput = page.locator('#insp input[data-pn="awsRegion"]')
  await expect(renamedInput).toBeVisible()

  // Contract: a row keeps its position while being edited (row index unchanged)
  // awsRegion must remain at index 1 (2nd row), not jump to index 0
  const afterRenameNames = await page.locator('#insp input[data-pn]').evaluateAll(inputs => inputs.map(i => i.value))
  expect(afterRenameNames).toEqual(['providerName', 'awsRegion', 'retention'])

  // Next clicks test: controls in the 2nd row belong to awsRegion, not providerName
  const secondRow = page.locator('#insp .fld').nth(1)
  await expect(secondRow.locator('input[data-pn]')).toHaveValue('awsRegion')
  await expect(secondRow.locator('input[data-pe]')).toHaveValue('eu-north-1,us-east-1')

  // The first row is providerName and its default remains untouched
  const firstRow = page.locator('#insp .fld').nth(0)
  await expect(firstRow.locator('input[data-pn]')).toHaveValue('providerName')
  await expect(firstRow.locator('input[data-pdef]')).toHaveValue('')

  // Edit enum in the 2nd row
  const enumInput = secondRow.locator('input[data-pe]')
  await enumInput.click()
  await enumInput.fill('eu-north-1,us-east-1,ap-south-1')
  await enumInput.press('Enter')
  await page.waitForTimeout(400)

  // Verify enum was saved on awsRegion and providerName remains unchanged
  const docRes = await request.get(ENGINE + '/api/blueprint')
  const doc = await docRes.json()
  expect(doc.spec.xrd.parameters.awsRegion.enum).toEqual(['eu-north-1', 'us-east-1', 'ap-south-1'])
  expect(doc.spec.xrd.parameters.providerName.default || '').toBe('')
})

test('CF-155: reverse rename keeps row position while editing', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="xrd"] .node-h')

  // Rename region -> awsRegion
  const regionInput = page.locator('#insp input[data-pn="region"]')
  await regionInput.fill('awsRegion')
  await regionInput.press('Enter')
  await expect(page.locator('#insp input[data-pn="awsRegion"]')).toBeVisible()

  // Verify it stayed in 2nd row
  let names = await page.locator('#insp input[data-pn]').evaluateAll(inputs => inputs.map(i => i.value))
  expect(names).toEqual(['providerName', 'awsRegion', 'retention'])

  // Reverse rename: awsRegion -> region
  const awsInput = page.locator('#insp input[data-pn="awsRegion"]')
  await awsInput.fill('region')
  await awsInput.press('Enter')
  await expect(page.locator('#insp input[data-pn="region"]')).toBeVisible()

  // Verify it stayed in 2nd row
  names = await page.locator('#insp input[data-pn]').evaluateAll(inputs => inputs.map(i => i.value))
  expect(names).toEqual(['providerName', 'region', 'retention'])
})

test('CF-155: re-opening the XRD inspector re-sorts the parameter list alphabetically', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="xrd"] .node-h')

  // Rename region -> awsRegion
  const regionInput = page.locator('#insp input[data-pn="region"]')
  await regionInput.fill('awsRegion')
  await regionInput.press('Enter')
  await expect(page.locator('#insp input[data-pn="awsRegion"]')).toBeVisible()

  // Order while editing: row index unchanged
  let names = await page.locator('#insp input[data-pn]').evaluateAll(inputs => inputs.map(i => i.value))
  expect(names).toEqual(['providerName', 'awsRegion', 'retention'])

  // Re-open: select resource card, then re-select XRD
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.click('.node[data-id="xrd"] .node-h')

  // Now the list should be re-sorted alphabetically on re-open
  names = await page.locator('#insp input[data-pn]').evaluateAll(inputs => inputs.map(i => i.value))
  expect(names).toEqual(['awsRegion', 'providerName', 'retention'])
})

test('CF-155: focus follows the renamed row when tabbing to type select', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="xrd"] .node-h')

  const regionInput = page.locator('#insp input[data-pn="region"]')
  await regionInput.fill('awsRegion')
  // Tab to the next control in the same row (type select)
  await regionInput.press('Tab')

  // Wait for rename to be processed
  await expect(page.locator('#insp select[data-pt="awsRegion"]')).toBeVisible()

  // The active element should now be the type select of the renamed parameter
  const focusedParamType = await page.locator(':focus').getAttribute('data-pt')
  expect(focusedParamType).toBe('awsRegion')
})
