const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-305 (#193) — Environment inspector reports key as unused when referenced by forEach, when, or connectionSecret', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('keys referenced by forEach, when, and connectionSecret are not marked as unused in environment inspector', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.environment = {
      nodeCount: { type: 'integer', default: '3' },
      environment: { type: 'string', default: 'prod' },
      secretName: { type: 'string', default: 'my-secret' },
      trulyUnused: { type: 'string', default: 'none' }
    };
    doc.spec.resources[0].forEach = 'env.nodeCount';
    doc.spec.resources[0].when = 'env.environment == "prod"';
    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    await page.evaluate(() => {
      window.store.state.doc.spec.resources[0].connectionSecret = { name: 'env.secretName' };
      window.store.emit('doc', window.store.state.doc);
    });

    // Open environment inspector
    const envCard = page.locator('.node[data-id="environment"]');
    await expect(envCard).toBeVisible();
    await envCard.locator('.node-h').click();
    await expect(envCard).toHaveClass(/sel/);

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    // Check nodeCount row (referenced by forEach)
    const nodeCountRow = inspector.locator('[data-env-key="nodeCount"]');
    await expect(nodeCountRow).toBeVisible();
    await expect(nodeCountRow.locator('.env-used-by')).not.toContainText('Not used by any resource');
    await expect(nodeCountRow.locator('.env-used-by')).toContainText('Used by:');
    await expect(nodeCountRow.locator('.env-used-by')).toContainText('work-queue.forEach');

    // Check environment row (referenced by when)
    const environmentRow = inspector.locator('[data-env-key="environment"]');
    await expect(environmentRow).toBeVisible();
    await expect(environmentRow.locator('.env-used-by')).not.toContainText('Not used by any resource');
    await expect(environmentRow.locator('.env-used-by')).toContainText('Used by:');
    await expect(environmentRow.locator('.env-used-by')).toContainText('work-queue.when');

    // Check secretName row (referenced by connectionSecret)
    const secretNameRow = inspector.locator('[data-env-key="secretName"]');
    await expect(secretNameRow).toBeVisible();
    await expect(secretNameRow.locator('.env-used-by')).not.toContainText('Not used by any resource');
    await expect(secretNameRow.locator('.env-used-by')).toContainText('Used by:');
    await expect(secretNameRow.locator('.env-used-by')).toContainText('work-queue.connectionSecret');

    // Check trulyUnused row (not referenced anywhere)
    const unusedRow = inspector.locator('[data-env-key="trulyUnused"]');
    await expect(unusedRow).toBeVisible();
    await expect(unusedRow.locator('.env-used-by')).toContainText('Not used by any resource');
  });
});
