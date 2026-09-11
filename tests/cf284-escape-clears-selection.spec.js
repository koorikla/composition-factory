const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');

test.describe('CF-284 — Canvas Escape key shortcut does not clear card selection', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('pressing Escape clears active resource selection and deselects node', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    // 1. Select a resource card
    const card = page.locator('.node[data-id="work-queue"]');
    await card.locator('.node-h').click();
    await expect(card).toHaveClass(/sel/);

    // Inspector should be open and display resource header
    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();
    await expect(inspector.locator('.insp-t .k')).toContainText('work-queue');

    // 2. Press Escape key on canvas
    await page.keyboard.press('Escape');

    // 3. Selection should be cleared: no node should have .sel
    await expect(card).not.toHaveClass(/sel/);
    await expect(page.locator('.node.sel')).toHaveCount(0);

    // Inspector should reset to default XRD view
    await expect(inspector.locator('.insp-t')).not.toContainText('work-queue');
  });

  test('pressing Escape clears active XRD card selection', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    // 1. Click XRD card
    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();
    await expect(xrdCard).toHaveClass(/sel/);

    // 2. Press Escape key
    await page.keyboard.press('Escape');

    // 3. Selection should be cleared
    await expect(xrdCard).not.toHaveClass(/sel/);
    await expect(page.locator('.node.sel')).toHaveCount(0);
  });

  test('pressing Escape clears active ENV card selection', async ({ page, request }) => {
    const pristine = await request.get(`${ENGINE}/api/blueprint`).then((r) => r.json());
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      clusterName: { type: 'string', required: true }
    };
    await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });

    await page.goto('/');
    await canvasSettled(page);

    // 1. Click ENV card
    const envCard = page.locator('.node[data-id="environment"]');
    await expect(envCard).toBeVisible();
    await envCard.locator('.node-h').click();
    await expect(envCard).toHaveClass(/sel/);

    // 2. Press Escape key
    await page.keyboard.press('Escape');

    // 3. Selection should be cleared
    await expect(envCard).not.toHaveClass(/sel/);
    await expect(page.locator('.node.sel')).toHaveCount(0);
  });
});
