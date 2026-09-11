const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-208 — Environment key renaming in inspector', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('EnvironmentConfig inspector allows renaming keys, auto-focuses new keys, and updates dependent wires', async ({ page, request }) => {
    // 1. Open canvas with EnvironmentConfig card present
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      initialKey: { type: 'string', description: 'Initial env key' }
    };
    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 2. Inspect the EnvironmentConfig card
    const envCard = page.locator('.node[data-id="environment"]');
    await expect(envCard).toBeVisible();
    await envCard.locator('.node-h').click();
    await expect(envCard).toHaveClass(/sel/);

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    // 3. Click + Add key (#envAddKeyBtn)
    const addKeyBtn = inspector.locator('#envAddKeyBtn');
    await expect(addKeyBtn).toBeVisible();
    await addKeyBtn.click();

    // 4. Verify the newly created row has an editable (not readonly) key name input
    const key1Input = inspector.locator('input[data-env-name="key1"]');
    await expect(key1Input).toBeVisible();
    await expect(key1Input).not.toHaveAttribute('readonly');
    // Verify auto-focus
    await expect(key1Input).toBeFocused();

    // 5. Change the key name from key1 to clusterName and trigger change/blur
    await key1Input.fill('clusterName');
    await key1Input.press('Enter');

    // 6. Verify clusterName is reflected in the blueprint doc (doc.spec.environment.clusterName)
    await expect.poll(async () => {
      const getRes = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await getRes.json();
      return doc.spec && doc.spec.environment && doc.spec.environment.clusterName;
    }).toBeTruthy();

    // Ensure key1 is no longer in environment
    const docCheck1 = await (await request.get(`${ENGINE}/api/blueprint`)).json();
    expect(docCheck1.spec.environment.key1).toBeUndefined();

    // 7. Wire a resource field to env.clusterName
    await page.evaluate(() => {
      window.store.replaceDoc((d) => {
        const dl = d.spec.resources.find((r) => r.name === 'dead-letter');
        dl.fields = dl.fields || {};
        dl.fields.region = { from: 'env.clusterName' };
      });
    });

    // Verify wire created in blueprint
    await expect.poll(async () => {
      const getRes = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await getRes.json();
      const dl = doc.spec.resources.find((r) => r.name === 'dead-letter');
      return dl.fields && dl.fields.region && dl.fields.region.from;
    }).toMatch(/(?:\$)?env\.clusterName/);

    // 8. Rename clusterName to productionCluster
    // Re-inspect EnvironmentConfig card
    await envCard.locator('.node-h').click();
    await expect(envCard).toHaveClass(/sel/);

    const clusterInput = inspector.locator('input[data-env-name="clusterName"]');
    await expect(clusterInput).toBeVisible();
    await clusterInput.fill('productionCluster');
    await clusterInput.blur();

    // 9. Verify the wire updates to $env.productionCluster / env.productionCluster and canvas remains valid
    await expect.poll(async () => {
      const getRes = await request.get(`${ENGINE}/api/blueprint`);
      const doc = await getRes.json();
      const dl = doc.spec.resources.find((r) => r.name === 'dead-letter');
      return dl.fields && dl.fields.region && dl.fields.region.from;
    }).toMatch(/(?:\$)?env\.productionCluster/);

    // Verify blueprint doc environment updated
    const finalDoc = await (await request.get(`${ENGINE}/api/blueprint`)).json();
    expect(finalDoc.spec.environment.productionCluster).toBeTruthy();
    expect(finalDoc.spec.environment.clusterName).toBeUndefined();

    // Verify canvas remains valid (#valid status chip does not show error)
    await canvasSettled(page);
    const validChip = page.locator('#valid');
    await expect(validChip).not.toHaveText(/error/i);
  });

  test('validates key rename rejects empty name, invalid characters, and collisions', async ({ page, request }) => {
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      existingKey: { type: 'string' },
      editMe: { type: 'string' }
    };
    await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });

    await page.goto('/');
    await canvasSettled(page);

    const envCard = page.locator('.node[data-id="environment"]');
    await envCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    const editInput = inspector.locator('input[data-env-name="editMe"]');
    await expect(editInput).toBeVisible();

    // 1. Collision with existing key
    await editInput.fill('existingKey');
    await editInput.press('Enter');

    // Should revert and show warn/toast
    await expect(editInput).toHaveValue('editMe');
    const errorToast = page.locator('#canvas-error-toast, #insp .warnbar');
    await expect(errorToast.first()).toBeVisible();
    await expect(errorToast.first()).toContainText(/already exists/i);

    // 2. Invalid syntax (spaces/dashes)
    await editInput.fill('invalid-key-name');
    await editInput.press('Enter');
    await expect(editInput).toHaveValue('editMe');

    // 3. Empty name
    await editInput.fill('');
    await editInput.press('Enter');
    await expect(editInput).toHaveValue('editMe');
  });
});
