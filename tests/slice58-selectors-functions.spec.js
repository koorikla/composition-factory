const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE } = require('./helpers')

test.beforeEach(async ({ request, page }) => {
  await resetDoc(request)
  const api = await request.get(ENGINE + '/api/kinds')
  test.skip(!api.ok(), 'cf serve is not running')
  await page.goto('/')
  await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible()
})

test('Functions catalogue search & 1-click pipeline addition', async ({ page }) => {
  page.on('console', msg => console.log('PAGE LOG:', msg.text()))

  await page.evaluate(() => {
    window.store.subscribe('error', err => console.log('STORE ERROR:', JSON.stringify(err)));
  });

  // Select XRD node first to inspect default pipeline
  await page.click('.node[data-id="xrd"] .node-h')
  await expect(page.locator('#insp')).toContainText('Pipeline (default)')

  // Click on SOURCES rail
  const srcTab = page.locator('#rtabs button[data-r="src"]')
  await srcTab.click()

  // Click Functions subtab
  const fnSubTab = page.locator('#src-subtabs button[data-src-sub="fn"]')
  await expect(fnSubTab).toBeVisible()
  await fnSubTab.click()

  // Verify search bar is visible
  const fnSearch = page.locator('#fn-search')
  await expect(fnSearch).toBeVisible()

  // Search for auto-ready
  await fnSearch.fill('auto-ready')
  await page.waitForTimeout(300)

  // Verify auto-ready function card appears
  const autoReadyCard = page.locator('.cat-row:has-text("function-auto-ready")')
  await expect(autoReadyCard).toBeVisible()

  // Click + Pipe button
  const addPipeBtn = autoReadyCard.locator('button[data-add-fn-pipe*="function-auto-ready"]')
  await expect(addPipeBtn).toBeVisible()
  await addPipeBtn.click()

  // Switch back to XRD / check inspector
  await page.click('.node[data-id="xrd"] .node-h')
  const stepNameInp = page.locator('#insp input[data-pipe-name="0"]')
  await expect(stepNameInp).toHaveValue('auto-ready')
})

test('Kubernetes workload selectors auto-match & pod spec helper', async ({ page }) => {
  page.on('console', msg => console.log('PAGE LOG 2:', msg.text()))

  // Click K8s App example from starter blueprints
  const guideTab = page.locator('#rtabs button[data-r="guide"]')
  await guideTab.click()
  await page.locator('button[data-guide-example="k8s-app"]').click()
  await page.waitForTimeout(600)

  // Select deployment node
  const depNode = page.locator('.node[data-id="app-deploy"] .node-h')
  await expect(depNode).toBeVisible()
  await depNode.click()

  // Verify Workload Selectors & Pod Spec card is rendered in inspector
  const wlCard = page.locator('.workload-card')
  await expect(wlCard).toBeVisible()

  // Edit app selector and click Sync
  const appInp = wlCard.locator('input[data-wl-app]')
  await expect(appInp).toBeVisible()
  await appInp.fill('test-app')

  const syncBtn = wlCard.locator('button[data-wl-sync-app]')
  await syncBtn.click()
  await page.waitForTimeout(400)

  // Verify selector alignment badge
  await expect(wlCard.locator('.chip-ok')).toContainText('Selectors Aligned')

  // Click Standard Labels preset
  const stdLblBtn = page.locator('button[data-apply-std-labels]')
  await expect(stdLblBtn).toBeVisible()
  await stdLblBtn.click()
  await page.waitForTimeout(400)

  // Click External Name preset
  const extNameBtn = page.locator('button[data-apply-ext-name]')
  await expect(extNameBtn).toBeVisible()
  await extNameBtn.click()
  await page.waitForTimeout(400)

  // Verify generated Composition YAML output reflects changes
  await page.click('#tabs button[data-t="comp"]')
  await expect(page.locator('#code')).toContainText('app.kubernetes.io/managed-by')
  await expect(page.locator('#code')).toContainText('crossplane.io/external-name')
})
