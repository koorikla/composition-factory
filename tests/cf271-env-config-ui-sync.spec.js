const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-271 — Canvas environment inspector syncs with spec.environmentConfigs', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('canvas and inspector reflect declared spec.environmentConfigs name, and editing selection updates spec.environmentConfigs and generates in sync', async ({ page, request }) => {
    // 1. Seed blueprint with spec.environment and spec.environmentConfigs declaring staging-env
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      region: {
        type: 'string',
        description: 'Target region'
      }
    };
    docWithEnv.spec.environmentConfigs = [
      {
        name: 'staging-env'
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 2. Canvas card header must reflect "staging-env", not "default"
    const envCard = page.locator('.node[data-id="environment"]');
    await expect(envCard).toBeVisible();
    await expect(envCard.locator('.node-h .nm')).toHaveText('staging-env');

    // 3. Click EnvironmentConfig card to open inspector
    await envCard.locator('.node-h').click();
    await expect(envCard).toHaveClass(/sel/);

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    // Inspector header must display "staging-env"
    await expect(inspector.locator('.insp-t .k')).toContainText('staging-env');

    // Selection name input must display "staging-env"
    const selInput = inspector.locator('#envSelName, input[data-env-selection-name]');
    await expect(selInput).toBeVisible();
    await expect(selInput).toHaveValue('staging-env');

    // Generated manifest preview in inspector must show name: staging-env
    const genSection = inspector.locator('.env-gen-file');
    await expect(genSection).toBeVisible();
    await expect(genSection).toContainText('name: staging-env');

    // 4. Edit selection name: change name to "custom-env"
    await selInput.fill('custom-env');
    await selInput.dispatchEvent('change');

    // 5. Verify GET /api/blueprint has spec.environmentConfigs updated to custom-env
    await expect.poll(async () => {
      const getRes = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await getRes.json();
      return doc.spec.environmentConfigs && doc.spec.environmentConfigs[0] && doc.spec.environmentConfigs[0].name;
    }).toBe('custom-env');

    // Verify canvas card header updates to "custom-env"
    await expect(envCard.locator('.node-h .nm')).toHaveText('custom-env');

    // Verify inspector header and preview update to "custom-env"
    await expect(inspector.locator('.insp-t .k')).toContainText('custom-env');
    await expect(genSection).toContainText('name: custom-env');

    // 6. Verify POST /api/generate produces matching EnvironmentConfig manifest and Composition pipeline step
    const genRes = await request.post(`${ENGINE}/api/generate`, { data: { write: false } });
    expect(genRes.ok()).toBeTruthy();
    const genData = await genRes.json();

    const envCfgOutput = (genData.outputs || []).find((o) => o.path.includes('environmentconfigs/custom-env.yaml'));
    expect(envCfgOutput).toBeDefined();
    expect(envCfgOutput.body).toContain('name: custom-env');

    const compOutput = (genData.outputs || []).find((o) => o.path.includes('compositions/'));
    expect(compOutput).toBeDefined();
    expect(compOutput.body).toContain('custom-env');
    expect(compOutput.body).not.toContain('staging-env');
  });

  test('canvas and inspector reflect declared spec.environmentConfigs selector, and switching modes updates spec.environmentConfigs', async ({ page, request }) => {
    // 1. Seed blueprint with spec.environment and selector-based environmentConfigs
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      region: {
        type: 'string'
      }
    };
    docWithEnv.spec.environmentConfigs = [
      {
        selector: {
          matchLabels: {
            environment: 'staging'
          }
        }
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // Canvas card header must show the selector labels
    const envCard = page.locator('.node[data-id="environment"]');
    await expect(envCard).toBeVisible();
    await expect(envCard.locator('.node-h .nm')).toHaveText('environment=staging');

    // Click to select
    await envCard.locator('.node-h').click();
    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    // Mode dropdown should be "Selector" and labels input should have "environment=staging"
    const modeSelect = inspector.locator('#envSelMode');
    await expect(modeSelect).toHaveValue('Selector');

    const lblInput = inspector.locator('#envSelLabels, input[data-env-selection-labels]');
    await expect(lblInput).toBeVisible();
    await expect(lblInput).toHaveValue('environment=staging');

    // Generated manifest preview should contain labels
    const genSection = inspector.locator('.env-gen-file');
    await expect(genSection).toContainText('environment: "staging"');

    // 2. Edit labels to "environment=production"
    await lblInput.fill('environment=production');
    await lblInput.dispatchEvent('change');

    // Verify GET /api/blueprint reflects updated matchLabels
    await expect.poll(async () => {
      const getRes = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await getRes.json();
      const cfg = doc.spec.environmentConfigs && doc.spec.environmentConfigs[0];
      return cfg && cfg.selector && cfg.selector.matchLabels && cfg.selector.matchLabels.environment;
    }).toBe('production');

    // 3. Switch mode to Reference
    await modeSelect.selectOption('Reference');

    // Verify GET /api/blueprint reflects Reference mode with name
    await expect.poll(async () => {
      const getRes = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await getRes.json();
      const cfg = doc.spec.environmentConfigs && doc.spec.environmentConfigs[0];
      return {
        hasSelector: !!(cfg && cfg.selector),
        name: cfg && cfg.name
      };
    }).toEqual({ hasSelector: false, name: 'default' });
  });
});
