const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-188 — EnvironmentConfig inspector and XRD environment summary', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('selecting the EnvironmentConfig card opens an inspector with selection, keys, used-by list, and generated file', async ({ page, request }) => {
    // 1. Seed blueprint with spec.environment and a wire from env.clusterName to dead-letter.region
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      clusterName: {
        type: 'string',
        description: 'Target cluster name'
      },
      maxRetries: {
        type: 'integer',
        default: '3',
        description: 'Maximum retries'
      }
    };
    // Add wire from env.clusterName to dead-letter resource field
    const dl = docWithEnv.spec.resources.find((r) => r.name === 'dead-letter');
    dl.fields = dl.fields || {};
    dl.fields.region = { from: 'env.clusterName' };

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 2. Click EnvironmentConfig card to select it
    const envCard = page.locator('.node[data-id="environment"]');
    await expect(envCard).toBeVisible();
    await envCard.locator('.node-h').click();
    await expect(envCard).toHaveClass(/sel/);

    const inspector = page.locator('#region-inspector');
    // Ensure inspector is open and does not report "Resource not found"
    await expect(inspector).not.toContainText('Resource "environment" not found in blueprint');

    // 3. Selection section: shows selection name/labels
    const selInput = inspector.locator('input[data-env-selection-name], #envSelName');
    await expect(selInput).toBeVisible();
    await expect(selInput).toHaveValue('default');

    // 4. Keys section: shows clusterName and maxRetries with type, required, and value
    const clusterRow = inspector.locator('[data-env-key="clusterName"]');
    await expect(clusterRow).toBeVisible();
    const clusterType = clusterRow.locator('select[data-env-type]');
    await expect(clusterType).toHaveValue('string');
    const clusterReq = clusterRow.locator('input[data-env-req]');
    await expect(clusterReq).not.toBeChecked();
    const clusterVal = clusterRow.locator('input[data-env-val]');
    await expect(clusterVal).toHaveValue('');

    const retriesRow = inspector.locator('[data-env-key="maxRetries"]');
    await expect(retriesRow).toBeVisible();
    const retriesType = retriesRow.locator('select[data-env-type]');
    await expect(retriesType).toHaveValue('integer');
    const retriesVal = retriesRow.locator('input[data-env-val]');
    await expect(retriesVal).toHaveValue('3');

    // 5. "Used by" list naming each wire with a remove action
    const usedBySection = clusterRow.locator('.env-used-by');
    await expect(usedBySection).toBeVisible();
    await expect(usedBySection).toContainText('dead-letter.region');
    const wireDelBtn = usedBySection.locator('button[data-env-wire-del]');
    await expect(wireDelBtn).toBeVisible();

    // 6. Generated file section with count of empty values
    const genSection = inspector.locator('.env-gen-file');
    await expect(genSection).toBeVisible();
    // clusterName has empty value, maxRetries has value 3 -> 1 empty value
    await expect(genSection.locator('.env-empty-count')).toContainText('1 empty value');
    await expect(genSection).toContainText('kind: EnvironmentConfig');
    await expect(genSection).toContainText('name: default');

    // 7. Edit clusterName: toggle required to true, and enter a value
    await clusterReq.check();
    // Verify blueprint was updated via mutation queue
    await expect.poll(async () => {
      const getRes = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await getRes.json();
      return doc.spec.environment.clusterName.required;
    }).toBe(true);

    await clusterVal.fill('test-cluster-01');
    await clusterVal.dispatchEvent('change');
    await expect.poll(async () => {
      const getRes = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await getRes.json();
      return doc.spec.environment.clusterName.value || doc.spec.environment.clusterName.default;
    }).toBe('test-cluster-01');

    // Empty values count is now 0
    await expect(genSection.locator('.env-empty-count')).toContainText('0 empty values');

    // 8. Remove wire via remove action
    await wireDelBtn.click();
    // Verify wire was removed from blueprint
    await expect.poll(async () => {
      const getRes = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await getRes.json();
      const r = doc.spec.resources.find((x) => x.name === 'dead-letter');
      return r.fields && r.fields.region;
    }).toBeUndefined();

    // Used by list reflects removal
    await expect(clusterRow.locator('.env-used-by')).not.toContainText('dead-letter.region');

    // 9. Edit selection name: change name to "prod-env"
    await selInput.fill('prod-env');
    await selInput.dispatchEvent('change');
    await expect.poll(async () => {
      const getRes = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await getRes.json();
      const s = (doc.spec.pipeline || []).find((step) => step.functionRef === 'function-environment-configs' || step.name === 'environment-configs');
      return s && s.input && s.input.includes('prod-env');
    }).toBeTruthy();

    await expect(genSection).toContainText('name: prod-env');
  });

  test('XRD inspector shows a one-line environment summary that links to SHARED instead of its own key editor', async ({ page, request }) => {
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      clusterName: { type: 'string' },
      maxRetries: { type: 'integer', default: '3' }
    };
    await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });

    await page.goto('/');
    await canvasSettled(page);

    // XRD is selected by default when nothing else is selected
    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    // Verify XRD inspector shows one-line environment summary
    const envSummary = inspector.locator('.env-summary-row');
    await expect(envSummary).toBeVisible();
    await expect(envSummary).toContainText('2 keys');

    // Ensure it links to SHARED
    const sharedLink = envSummary.locator('[data-tab-switch="shared"]');
    await expect(sharedLink).toBeVisible();

    // Ensure XRD inspector does NOT have its own environment key editor
    await expect(inspector.locator('[data-env-key]')).toHaveCount(0);
    await expect(inspector.locator('#env-add-submit')).toHaveCount(0);

    // Clicking SHARED link switches the left palette rail to "shared"
    await sharedLink.click();
    const sharedTabBtn = page.locator('#rtabs button[data-r="shared"]');
    await expect(sharedTabBtn).toHaveAttribute('aria-pressed', 'true');
  });
});
