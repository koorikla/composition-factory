// Slice 1 — the core behavior the product exists for, against the LIVE engine:
// see real kinds, drop one, move cards, edit a field, watch the real YAML
// change, see a real 400 verbatim. Selectors match web-proto/index.html
// (region roots #region-palette / #cw / #region-inspector / #region-output).
//
// The suite runs against its own isolated engine on 127.0.0.1:8081. Every test restores
// the live blueprint to the exact doc it found, so the suite is idempotent.
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

let baseline = null

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
  let api
  baseline = await (await request.get(ENGINE + '/api/blueprint')).json()
})

test.afterEach(async ({ request }) => {
  if (baseline) await request.put(ENGINE + '/api/blueprint', { data: baseline })
})

test('palette lists the provider kinds from the live API, with server-side search', async ({ page }) => {
  await page.goto('/')
  const rail = page.locator('#region-palette #lrail')
  await expect(rail.locator('.kind .nm', { hasText: /^Queue$/ }).first()).toBeVisible()
  await expect(rail.locator('.kind .nm', { hasText: 'QueuePolicy' }).first()).toBeVisible()
  await expect(rail.locator('.grp .lbl', { hasText: 'sqs.aws.m.upbound.io' })).toBeVisible()
  // server-side ?q= search narrows the rail
  await page.fill('#psearch', 'redrive')
  await expect(rail.locator('.kind .nm', { hasText: 'QueueRedrivePolicy' }).first()).toBeVisible()
  await expect(rail.locator('.kind .nm', { hasText: /^Queue$/ })).toHaveCount(0)
})

test('the loaded blueprint renders three cards with wires; shared fan-out is colored shared', async ({ page }) => {
  await page.goto('/')
  const canvas = page.locator('#canvas')
  await expect(canvas.locator('.node')).toHaveCount(3)
  await expect(canvas.locator('.node[data-id="xrd"] .k')).toHaveText('XNotify')
  await expect(canvas.locator('.node[data-id="work-queue"]')).toBeVisible()
  await expect(canvas.locator('.node[data-id="dead-letter"]')).toBeVisible()
  // wires live IN the doc: region -> 2 fields (shared), retention -> 1 (xrd blue)
  await expect(page.locator('#wires path')).toHaveCount(3)
  await expect(page.locator('#wires path.wire-shared')).toHaveCount(2)
  await expect(page.locator('#wires path.wire-xrd')).toHaveCount(1)
  // the XR card marks region's fan-out ×2
  await expect(canvas.locator('.node[data-id="xrd"] .port[data-path="region"] .fan')).toHaveText('×2')
})

test('dragging a card persists its position across re-renders', async ({ page }) => {
  await page.goto('/')
  const node = page.locator('.node[data-id="work-queue"]')
  await expect(node).toBeVisible()
  const before = await node.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))
  const head = node.locator('.node-h')
  const box = await head.boundingBox()
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width / 2 + 55, box.y + box.height / 2 + 38, { steps: 5 })
  await page.mouse.up()
  const after = await node.evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))
  expect(after.x - before.x).toBeGreaterThan(45)
  expect(after.y - before.y).toBeGreaterThan(28)
  // force a full canvas re-render (selection change) — position must survive
  await page.click('.node[data-id="xrd"] .node-h')
  const persisted = await page.locator('.node[data-id="work-queue"]')
    .evaluate(el => ({ x: el.offsetLeft, y: el.offsetTop }))
  expect(persisted).toEqual(after)
})

test('dropping a palette kind persists a new resource in the doc', async ({ page, request }) => {
  await page.goto('/')
  const row = page.locator('#lrail .kind[data-kind="QueuePolicy"]').first()
  await expect(row).toBeVisible()
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible() // doc loaded
  // Synthesize HTML5 DnD (dataTransfer payload is what the palette sets).
  await page.evaluate(() => {
    const row = document.querySelector('#lrail .kind[data-kind="QueuePolicy"]')
    const cw = document.getElementById('cw')
    const r = cw.getBoundingClientRect()
    const dt = new DataTransfer()
    row.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }))
    const at = { clientX: r.left + 160, clientY: r.top + 260 }
    cw.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt, ...at }))
    cw.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt, ...at }))
  })
  const dropped = page.locator('.node[data-id="queue-policy"]')
  await expect(dropped).toBeVisible()
  // persisted server-side by the full-doc PUT
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.resources.map(r => r.name)).toContain('queue-policy')
  // and selected: the inspector shows the new resource's schema header
  await expect(page.locator('#insp .insp-t .k')).toContainText('QueuePolicy')
})

test('a value edit PUTs the doc and the output regenerates with real YAML', async ({ page, request }) => {
  await page.goto('/')
  // initial generate: topbar chip + composition tab render live engine output
  await expect(page.locator('#valid')).toHaveText(/ok · \d+ files/)
  await expect(page.locator('#code')).toContainText('kind: Composition')
  await expect(page.locator('#code')).not.toContainText('maxMessageSize')
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.click('#fseg button[data-f="all"]')
  const input = page.locator('#insp input[data-v="maxMessageSize"]')
  await input.fill('2048')
  await input.press('Tab') // change -> replaceDoc PUT
  const doc = await expect.poll(async () =>
    (await (await request.get(ENGINE + '/api/blueprint')).json())
      .spec.resources.find(r => r.name === 'work-queue').fields.maxMessageSize?.value
  ).toBe('2048').then(() => true)
  // debounced regenerate picks the edit up into the composition template
  await expect(page.locator('#code')).toContainText('maxMessageSize', { timeout: 5000 })
})

test('a server 400 shows verbatim in the inspector and the doc stays unchanged', async ({ page, request }) => {
  await page.goto('/')
  await page.click('.node[data-id="xrd"] .node-h')
  const kindInput = page.locator('#xk')
  await expect(kindInput).toHaveValue('XNotify')
  await kindInput.fill('xnotify!')
  await kindInput.press('Tab')
  // the engine's message, verbatim
  await expect(page.locator('#insp .warnbar')).toContainText(
    'spec.xrd.kind: "xnotify!" is not a valid Kind (must start with an uppercase letter, e.g. XQueue)')
  // store contract: rejected replace leaves the doc untouched
  await expect(page.locator('#xk')).toHaveValue('XNotify')
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json()
  expect(doc.spec.xrd.kind).toBe('XNotify')
})

test('output tabs switch between the real generated files and the blueprint', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('#code')).toContainText('kind: Composition')
  await expect(page.locator('#meta')).toContainText('lines · deterministic')
  await page.click('#tabs button[data-t="xrd"]')
  await expect(page.locator('#code')).toContainText('kind: CompositeResourceDefinition')
  await expect(page.locator('#tabs button[data-t="bp"]')).toHaveText('xnotify.cf.yaml')
  await page.click('#tabs button[data-t="bp"]')
  await expect(page.locator('#code')).toContainText('kind: Blueprint')
  await expect(page.locator('#code')).toContainText('from: params.region')
})

test('the drawer splitter resizes and double-click collapses', async ({ page }) => {
  await page.goto('/')
  const drawer = page.locator('#region-output')
  const split = page.locator('#region-output .of-split')
  await expect(split).toBeAttached()
  const h1 = await drawer.evaluate(el => el.offsetHeight)
  const box = await split.boundingBox()
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2 - 80, { steps: 4 })
  await page.mouse.up()
  const h2 = await drawer.evaluate(el => el.offsetHeight)
  expect(Math.abs(h2 - (h1 + 80))).toBeLessThanOrEqual(8)
  await split.dblclick()
  await expect(drawer).toHaveAttribute('data-collapsed', '')
  await expect(page.locator('#code')).toBeHidden()
  await split.dblclick() // pointerdown on a collapsed drawer re-expands
  await expect(drawer).not.toHaveAttribute('data-collapsed', '')
  await expect(page.locator('#code')).toBeVisible()
})

test('theme button cycles system → light → dark → system', async ({ page }) => {
  await page.goto('/')
  const html = page.locator('html')
  await expect(html).not.toHaveAttribute('data-theme')
  await page.click('#themeBtn')
  await expect(html).toHaveAttribute('data-theme', 'light')
  await page.click('#themeBtn')
  await expect(html).toHaveAttribute('data-theme', 'dark')
  await page.click('#themeBtn')
  await expect(html).not.toHaveAttribute('data-theme')
})
