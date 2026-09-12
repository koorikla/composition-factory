// CF-417 — Native kinds must not render a status.atProvider.id output port
// A kind whose schema declares no status (or no atProvider status) must not present
// status output ports on its card; whatever the canvas offers as a wire source must
// be a source the engine will accept.
const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, dropKind } = require('./helpers');

test.describe('CF-417 — Native kinds render no status.atProvider.id output port', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('native kinds (ServiceAccount, ConfigMap, Secret, Service) do not grow status output ports', async ({ page }) => {
    await page.goto('/');

    // Drop native kinds onto canvas
    await dropKind(page, 'ServiceAccount', 'v1', 400, 150);
    await dropKind(page, 'ConfigMap', 'v1', 400, 300);
    await dropKind(page, 'Secret', 'v1', 400, 450);
    await dropKind(page, 'Service', 'v1', 400, 600);

    const nativeIds = ['service-account', 'config-map', 'secret', 'service'];

    for (const id of nativeIds) {
      const card = page.locator(`.node[data-id="${id}"]`);
      await expect(card).toBeVisible();

      // No status.atProvider.id port
      await expect(card.locator('.port[data-path="status.atProvider.id"]')).toHaveCount(0);

      // No status output ports at all
      await expect(card.locator('.port.status')).toHaveCount(0);

      // No outputs section header
      await expect(card.locator('.node-grp', { hasText: 'outputs' })).toHaveCount(0);
    }

    // Managed resource (work-queue) with status schema still exposes its status outputs
    const wqCard = page.locator('.node[data-id="work-queue"]');
    await expect(wqCard).toBeVisible();
    await expect(wqCard.locator('.port[data-path="status.atProvider.id"]')).toBeVisible();
    await expect(wqCard.locator('.node-grp', { hasText: 'outputs' })).toBeVisible();
  });
});
