const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('SHARED shows an ENVIRONMENT section after PARAMETERS with palette hint', async ({ page }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')

  // Verify palette hint mentions keys are dragged from EnvironmentConfig card
  const hint = page.locator('#lhint')
  await expect(hint).toContainText(/keys are dragged from the EnvironmentConfig card/i)

  // Verify Parameters section comes first, then Environment section
  const railText = await page.locator('#lrail').innerText()
  const upperText = railText.toUpperCase()
  const paramsIdx = upperText.indexOf('PARAMETERS')
  const envIdx = upperText.indexOf('ENVIRONMENT')
  expect(paramsIdx).toBeGreaterThanOrEqual(0)
  expect(envIdx).toBeGreaterThan(paramsIdx)

  // Default pristine doc has no environment declared
  await expect(page.locator('#lrail')).toContainText(/no environment declared/i)
  await expect(page.locator('#env-add-btn')).toBeVisible()
})

test('an inline form declares an environment key with type and default and writes spec.environment.<key>', async ({ page, request }) => {
  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')

  // Open inline add form
  await page.click('#env-add-btn')
  const form = page.locator('#env-add-form')
  await expect(form).toBeVisible()

  // Verify form elements
  await expect(page.locator('#env-add-name')).toBeVisible()
  await expect(page.locator('#env-add-type')).toBeVisible()
  await expect(page.locator('#env-add-default')).toBeVisible()

  // Fill in environment key
  await page.fill('#env-add-name', 'vpcId')
  await page.selectOption('#env-add-type', 'string')
  await page.fill('#env-add-default', 'vpc-123456')
  await page.click('#env-add-submit')

  // Form closes and key is rendered in default config card
  await expect(form).toBeHidden()
  const card = page.locator('.card[data-config="default"]')
  await expect(card).toBeVisible()
  await expect(card).toContainText('$env.vpcId')
  await expect(card).toContainText('0 bound')
  await expect(card.locator('[data-env-del="vpcId"]')).toBeVisible()

  // Verify blueprint doc persisted on server
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.environment).toBeDefined()
  expect(doc.spec.environment.vpcId).toEqual({
    type: 'string',
    default: 'vpc-123456',
  })
})

test('an unused environment key can be removed from the SHARED rail', async ({ page, request }) => {
  // Pre-seed an environment key
  const docBefore = await (await request.get(ENGINE + '/api/blueprint')).json()
  docBefore.spec.environment = {
    scratch: { type: 'string', default: 'temp' },
  }
  await request.put(ENGINE + '/api/blueprint', { data: docBefore })

  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')

  page.on('dialog', d => d.accept())
  await page.click('[data-env-del="scratch"]')

  // Key is removed from rail
  await expect(page.locator('#lrail').getByText('$env.scratch')).toHaveCount(0)

  // Key is removed from blueprint
  const docAfter = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(docAfter.spec.environment?.scratch).toBeUndefined()
})

test('removing a wired environment key unwires referencers and removes it from the SHARED rail', async ({ page, request }) => {
  // Pre-seed an environment key wired to a valid resource field
  const docBefore = await (await request.get(ENGINE + '/api/blueprint')).json()
  docBefore.spec.environment = {
    sharedRegion: { type: 'string', default: 'us-east-1' },
  }
  // Wire to work-queue region field
  docBefore.spec.resources[0].fields.region = { from: 'env.sharedRegion' }
  const putRes = await request.put(ENGINE + '/api/blueprint', { data: docBefore })
  expect(putRes.ok()).toBeTruthy()

  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')

  // Verify bound count is 1
  const row = page.locator('.card[data-config="default"]')
  await expect(row).toContainText('$env.sharedRegion')
  await expect(row).toContainText('1 bound')

  page.on('dialog', d => d.accept())
  await page.click('[data-env-del="sharedRegion"]')

  // Key is removed from rail
  await expect(page.locator('#lrail').getByText('$env.sharedRegion')).toHaveCount(0)

  // Key is removed from blueprint and referencing wire is removed
  const docAfter = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(docAfter.spec.environment?.sharedRegion).toBeUndefined()
  expect(docAfter.spec.resources[0].fields?.region?.from).toBeUndefined()
})

test('cancelling unwire dialog for a wired environment key keeps it', async ({ page, request }) => {
  const docBefore = await (await request.get(ENGINE + '/api/blueprint')).json()
  docBefore.spec.environment = {
    sharedRegion: { type: 'string', default: 'us-east-1' },
  }
  docBefore.spec.resources[0].fields.region = { from: 'env.sharedRegion' }
  const putRes = await request.put(ENGINE + '/api/blueprint', { data: docBefore })
  expect(putRes.ok()).toBeTruthy()

  await page.goto('/')
  await page.click('#rtabs button[data-r="shared"]')

  page.on('dialog', d => d.dismiss())
  await page.click('[data-env-del="sharedRegion"]')

  // Key remains in blueprint
  const docAfter = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(docAfter.spec.environment?.sharedRegion).toBeDefined()
  expect(docAfter.spec.resources[0].fields?.region?.from).toBe('env.sharedRegion')
})
