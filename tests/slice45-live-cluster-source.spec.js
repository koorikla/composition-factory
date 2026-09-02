// Slice 45 — Live-cluster schema source: discover CRDs dynamically from any live cluster
const { test, expect } = require('@playwright/test')
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test.describe('Live-Cluster Schema Source', () => {
  test('SOURCES rail displays the Live Cluster section with connection status', async ({ page }) => {
    await page.goto('/')
    // Open SOURCES tab
    await page.click('#rtabs button[data-r="src"]')
    const rail = page.locator('#lrail')
    await expect(rail).toContainText('Live Cluster')
    await expect(rail.locator('#cluster-sync-btn')).toBeVisible()
  })

  test('Syncing cluster populates kinds list with cluster CRDs', async ({ page }) => {
    // Route mock cluster responses if not connected to a real live cluster
    await page.route('**/api/blueprint', async route => {
      if (route.request().method() === 'PUT') {
        const body = route.request().postDataJSON();
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(body),
        });
      } else {
        await route.continue();
      }
    });
    await page.route('**/api/cluster', async route => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            connected: true,
            context: 'kind-local-test',
            server: 'https://127.0.0.1:6443',
            crdCount: 3,
          }),
        })
      } else {
        await route.continue()
      }
    })

    await page.route('**/api/cluster/sync', async route => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          connected: true,
          context: 'kind-local-test',
          server: 'https://127.0.0.1:6443',
          crdCount: 3,
        }),
      })
    })

    await page.route('**/api/kinds/**', async route => {
      const url = route.request().url()
      if (url.includes('/fields')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            fields: [
              { path: 'spec.secretName', type: 'string', description: 'Secret storing cert', required: true, requiredChain: true, depth: 1 },
              { path: 'spec.dnsNames', type: 'array', description: 'Domain names', required: false, requiredChain: false, depth: 1 },
            ],
            total: 2,
            requiredBranches: [],
          }),
        })
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            kind: { kind: 'Certificate', apiVersion: 'cert-manager.io/v1', provider: 'cluster' },
            envelope: [],
            status: [],
          }),
        })
      }
    })

    await page.route('**/api/kinds?*', async route => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          kinds: [
            {
              kind: 'Certificate',
              group: 'cert-manager.io',
              version: 'v1',
              apiVersion: 'cert-manager.io/v1',
              provider: 'cluster',
              scope: 'Namespaced',
              namespaced: true,
              required: 1,
              fields: 2,
            },
          ],
        }),
      })
    })

    await page.route(url => url.pathname === '/api/kinds', async route => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          kinds: [
            {
              kind: 'Certificate',
              group: 'cert-manager.io',
              version: 'v1',
              apiVersion: 'cert-manager.io/v1',
              provider: 'cluster',
              scope: 'Namespaced',
              namespaced: true,
              required: 1,
              fields: 2,
            },
          ],
        }),
      })
    })

    await page.goto('/')

    // Check SOURCES tab shows cluster info
    await page.click('#rtabs button[data-r="src"]')
    const rail = page.locator('#lrail')
    await expect(rail).toContainText('Live Cluster')
    await expect(rail).toContainText('kind-local-test')
    await expect(rail).toContainText('3 CRDs')

    // Click Sync
    await Promise.all([
      page.waitForResponse(res => res.url().includes('/api/cluster/sync') && res.status() === 200),
      page.click('#cluster-sync-btn'),
    ]);

    // Switch back to KINDS tab
    await page.click('#rtabs button[data-r="kinds"]');
    const kindRow = page.locator('.kind[data-kind="Certificate"]')
    await expect(kindRow).toBeVisible()
    await expect(kindRow).toContainText('cluster')

    // Drag the Certificate kind onto the canvas
    await page.evaluate(() => {
      const row = document.querySelector('.kind[data-kind="Certificate"]')
      const cw = document.getElementById('cw')
      const r = cw.getBoundingClientRect()
      const dt = new DataTransfer()
      row.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }))
      const ev = { clientX: r.left + 400, clientY: r.top + 300 }
      cw.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt, ...ev }))
      cw.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt, ...ev }))
    })

    const card = page.locator('.node[data-id="certificate"]')
    await expect(card).toBeVisible()
    await expect(card.locator('.k')).toHaveText('Certificate')
    await expect(card).toContainText('secretName')
  })
})
