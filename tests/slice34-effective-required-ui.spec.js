// Slice 34 — the GUI adopts effective requiredness: a native Deployment's
// Required view shows selector+template (branches), not 250 conditional
// leaves; managed kinds are unchanged (chain == raw there).
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

async function dropKind(page, kind, av, x, y) {
  await page.evaluate(({ kind, av, x, y }) => {
    const row = document.querySelector('.kind[data-kind="' + kind + '"][data-av="' + av + '"]')
    const cw = document.getElementById('cw')
    const r = cw.getBoundingClientRect()
    const dt = new DataTransfer()
    row.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }))
    const ev = { clientX: r.left + x, clientY: r.top + y }
    cw.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt, ...ev }))
    cw.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt, ...ev }))
  }, { kind, av, x, y })
}

test('a dropped Deployment card shows its true must-set fields, not a flood', async ({ page }) => {
  await page.goto('/')
  await dropKind(page, 'Deployment', 'apps/v1', 400, 300)
  const card = page.locator('.node[data-id="deployment"]')
  await expect(card).toBeVisible()
  await expect(card).toContainText('selector')
  await expect(card).toContainText('template')
  const reqRows = await card.locator('.port.req').count()
  expect(reqRows).toBeLessThanOrEqual(4)   // selector+template(+set rows), not hundreds
})

test('the inspector Required filter shows the branches for Deployment', async ({ page }) => {
  await page.goto('/')
  await dropKind(page, 'Deployment', 'apps/v1', 400, 300)
  await page.click('.node[data-id="deployment"] .node-h')
  await page.click('#fseg button[data-f="req"]')
  const insp = page.locator('#insp')
  await expect(insp).toContainText('spec.selector')
  await expect(insp).toContainText('spec.template')
  await expect(insp).toContainText(/required object|must be set|expand/i)
  await expect(insp).not.toContainText('imagePullPolicy') // conditional leaf noise gone
})

test('managed Queue required view is unchanged (chain equals raw)', async ({ page }) => {
  await page.goto('/')
  await page.click('.node[data-id="work-queue"] .node-h')
  await page.click('#fseg button[data-f="req"]')
  await expect(page.locator('#insp')).toContainText('region')
})

test('a dropped CronJob card and inspector show required schedule and jobTemplate branch', async ({ page }) => {
  await page.goto('/')
  await dropKind(page, 'CronJob', 'batch/v1', 400, 300)
  const card = page.locator('.node[data-id="cron-job"]')
  await expect(card).toBeVisible()
  await expect(card).toContainText('schedule')
  await expect(card).toContainText('jobTemplate')
  await page.click('.node[data-id="cron-job"] .node-h')
  await page.click('#fseg button[data-f="req"]')
  const insp = page.locator('#insp')
  await expect(insp).toContainText('spec.schedule')
  await expect(insp).toContainText('spec.jobTemplate')
})
