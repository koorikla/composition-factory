const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test('CF-211: deleting a resource with downstream wires unwires dependents and succeeds', async ({ page, request }) => {
  // 1. Load the SQS starter blueprint (main-queue feeds status wire to queue-policy.queueUrl)
  const loadRes = await request.post(ENGINE + '/api/examples/sqs-queue/load')
  expect(loadRes.ok()).toBe(true)

  await page.goto('/')

  // Verify main-queue and queue-policy are on canvas
  await expect(page.locator('.node[data-id="main-queue"]')).toBeVisible()
  await expect(page.locator('.node[data-id="queue-policy"]')).toBeVisible()

  // Verify initial blueprint has the status wire
  const initialDoc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const initialPolicy = initialDoc.spec.resources.find(r => r.name === 'queue-policy')
  expect(initialPolicy.fields.queueUrl).toMatchObject({
    from: 'resources.main-queue.status.atProvider.url',
  })

  // 2. Select main-queue and press Delete
  let dialogMessage = ''
  page.on('dialog', dialog => {
    dialogMessage = dialog.message()
    dialog.accept()
  })

  await page.click('.node[data-id="main-queue"] .node-h')
  await page.keyboard.press('Delete')

  // 3. Verify confirmation prompt includes both dropped incoming wires AND unwired downstream references
  expect(dialogMessage).toContain('Remove "main-queue"?')
  expect(dialogMessage).toContain('Wired fields will be dropped:')
  expect(dialogMessage).toContain('maxMessageSize')
  expect(dialogMessage).toContain('Downstream references in queue-policy (queueUrl) will be unwired')

  // 4. Verify no error toast appears and main-queue is removed from the canvas
  await expect(page.locator('#canvas-error-toast')).toHaveCount(0)
  await expect(page.locator('.node[data-id="main-queue"]')).toHaveCount(0)
  await expect(page.locator('.node[data-id="queue-policy"]')).toBeVisible()

  // 5. Verify blueprint on server: main-queue removed, queue-policy.queueUrl wire cleanly unwired
  const updatedDoc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(updatedDoc.spec.resources.map(r => r.name)).not.toContain('main-queue')
  const updatedPolicy = updatedDoc.spec.resources.find(r => r.name === 'queue-policy')
  expect(updatedPolicy).toBeTruthy()
  expect(updatedPolicy.fields.queueUrl).toBeUndefined()
  // queue-policy's other fields (e.g. region, policy) are intact
  expect(updatedPolicy.fields.region).toBeDefined()
  expect(updatedPolicy.fields.policy).toBeDefined()
})

test('CF-211: canceling confirmation leaves resource and wires untouched', async ({ page, request }) => {
  const loadRes = await request.post(ENGINE + '/api/examples/sqs-queue/load')
  expect(loadRes.ok()).toBe(true)

  await page.goto('/')
  await expect(page.locator('.node[data-id="main-queue"]')).toBeVisible()

  page.on('dialog', dialog => dialog.dismiss())

  await page.click('.node[data-id="main-queue"] .node-h')
  await page.keyboard.press('Delete')

  // main-queue remains on canvas
  await expect(page.locator('.node[data-id="main-queue"]')).toBeVisible()

  // Blueprint on server is unchanged
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.map(r => r.name)).toContain('main-queue')
  const policy = doc.spec.resources.find(r => r.name === 'queue-policy')
  expect(policy.fields.queueUrl).toMatchObject({
    from: 'resources.main-queue.status.atProvider.url',
  })
})

test('CF-211: deleting via card header delete button uses consistent unwiring logic', async ({ page, request }) => {
  const loadRes = await request.post(ENGINE + '/api/examples/sqs-queue/load')
  expect(loadRes.ok()).toBe(true)

  await page.goto('/')
  await expect(page.locator('.node[data-id="main-queue"]')).toBeVisible()

  let dialogMessage = ''
  page.on('dialog', dialog => {
    dialogMessage = dialog.message()
    dialog.accept()
  })

  // Select card first to reveal action buttons
  await page.click('.node[data-id="main-queue"] .node-h')
  // Click card header delete button
  await page.click('.node[data-id="main-queue"] button[data-act="delete"]')

  expect(dialogMessage).toContain('Remove "main-queue"?')
  expect(dialogMessage).toContain('Downstream references in queue-policy (queueUrl) will be unwired')

  await expect(page.locator('#canvas-error-toast')).toHaveCount(0)
  await expect(page.locator('.node[data-id="main-queue"]')).toHaveCount(0)

  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.map(r => r.name)).not.toContain('main-queue')
  const policy = doc.spec.resources.find(r => r.name === 'queue-policy')
  expect(policy.fields.queueUrl).toBeUndefined()
})

test('CF-211: resource with no incoming wires but downstream references prompts and unwires', async ({ page, request }) => {
  // Load sqs-queue starter, then remove incoming wires from main-queue (only literal values)
  const loadRes = await request.post(ENGINE + '/api/examples/sqs-queue/load')
  expect(loadRes.ok()).toBe(true)

  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  const mq = doc.spec.resources.find(r => r.name === 'main-queue')
  mq.fields = {
    region: { value: 'eu-north-1' },
    sqsManagedSseEnabled: { value: true },
  }
  const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc })
  expect(putRes.ok()).toBe(true)

  await page.goto('/')
  await expect(page.locator('.node[data-id="main-queue"]')).toBeVisible()

  let dialogMessage = ''
  page.on('dialog', dialog => {
    dialogMessage = dialog.message()
    dialog.accept()
  })

  await page.click('.node[data-id="main-queue"] .node-h')
  await page.keyboard.press('Delete')

  // Prompt should NOT say "Wired fields will be dropped:" because main-queue has no incoming wires
  expect(dialogMessage).not.toContain('Wired fields will be dropped')
  expect(dialogMessage).toBe('Remove "main-queue"? Downstream references in queue-policy (queueUrl) will be unwired.')

  await expect(page.locator('#canvas-error-toast')).toHaveCount(0)
  await expect(page.locator('.node[data-id="main-queue"]')).toHaveCount(0)

  const updated = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(updated.spec.resources.map(r => r.name)).not.toContain('main-queue')
  const policy = updated.spec.resources.find(r => r.name === 'queue-policy')
  expect(policy.fields.queueUrl).toBeUndefined()
  expect(policy.fields.region).toBeDefined()
})
