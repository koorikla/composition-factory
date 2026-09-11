const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled, settledBox } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-185 — Environment keys source card on canvas', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('canvas shows EnvironmentConfig card when spec.environment is non-empty, with shared-colour dots and no env rows on XRD card', async ({ page, request }) => {
    // 1. Without spec.environment, canvas has no EnvironmentConfig card
    await page.goto('/');
    await canvasSettled(page);
    await expect(page.locator('.node[data-id="environment"], .node[data-kind="EnvironmentConfig"]')).toHaveCount(0);

    // 2. Add spec.environment to doc
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      clusterName: {
        type: 'string',
        description: 'Target EKS cluster name'
      },
      maxRetries: {
        type: 'integer',
        description: 'Maximum retry count'
      }
    };
    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 3. Canvas shows one EnvironmentConfig source card named "default"
    const envCard = page.locator('.node[data-id="environment"]');
    await expect(envCard).toBeVisible();
    await expect(envCard.locator('.k')).toHaveText('EnvironmentConfig');
    await expect(envCard.locator('.node-h .nm')).toHaveText('default');
    await expect(envCard.locator('.node-grp')).toHaveText('apiextensions.crossplane.io/v1beta1');

    // 4. Output dot per key in the shared colour
    const clusterPort = envCard.locator('.port[data-path="clusterName"]');
    await expect(clusterPort).toBeVisible();
    const clusterDot = clusterPort.locator('.d.out');
    await expect(clusterDot).toBeVisible();
    const clusterColor = await clusterDot.evaluate((el) => el.style.background || getComputedStyle(el).backgroundColor);
    expect(clusterColor).toContain('var(--shared)');

    const retryPort = envCard.locator('.port[data-path="maxRetries"]');
    await expect(retryPort).toBeVisible();
    const retryDot = retryPort.locator('.d.out');
    await expect(retryDot).toBeVisible();
    const retryColor = await retryDot.evaluate((el) => el.style.background || getComputedStyle(el).backgroundColor);
    expect(retryColor).toContain('var(--shared)');

    // 5. XRD card carries no env rows
    const xrdCard = page.locator('.node[data-id="xrd"]');
    await expect(xrdCard).toBeVisible();
    await expect(xrdCard.locator('.port[data-path="clusterName"]')).toHaveCount(0);
    await expect(xrdCard.locator('.port[data-path="maxRetries"]')).toHaveCount(0);

    // 6. Legend reads "shared · env"
    const legend = page.locator('.legend');
    await expect(legend).toBeVisible();
    await expect(legend).toContainText('shared · env');
  });

  test('selecting the EnvironmentConfig card selects it and opens the inspector', async ({ page, request }) => {
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      vpcId: { type: 'string', description: 'VPC ID' }
    };
    await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });

    await page.goto('/');
    await canvasSettled(page);

    const envCard = page.locator('.node[data-id="environment"]');
    await expect(envCard).toBeVisible();
    await expect(envCard).not.toHaveClass(/sel/);

    // Click to select
    await envCard.locator('.node-h').click();
    await expect(envCard).toHaveClass(/sel/);

    // Selected in store / DOM
    const selected = await page.evaluate(() => {
      const selNode = document.querySelector('.node.sel');
      return selNode ? selNode.getAttribute('data-id') : null;
    });
    expect(selected).toBe('environment');

    const storeSel = await page.evaluate(() => window.store?.state?.selectedResource);
    expect(storeSel).toBe('environment');

    // On narrow screens (< 900px), selected resource opens inspector drawer
    await page.setViewportSize({ width: 800, height: 600 });
    const inspector = page.locator('#region-inspector');
    await expect(inspector).toHaveClass(/drawer-open/);
  });

  test('dragging an environment key onto a resource card opens the bind menu and creates from: env.<key>', async ({ page, request }) => {
    page.on('dialog', async (d) => await d.accept());
    const docWithEnv = JSON.parse(JSON.stringify(pristine));
    docWithEnv.spec.environment = {
      clusterName: { type: 'string', description: 'Target cluster' }
    };
    await request.put(`${ENGINE}/api/blueprint`, { data: docWithEnv });

    await page.goto('/');
    await canvasSettled(page);

    const envPortDot = page.locator('.node[data-id="environment"] .port[data-path="clusterName"] .d.out');
    const envBox = await settledBox(envPortDot);

    const targetCardHeader = page.locator('.node[data-id="dead-letter"] .node-h');
    const targetBox = await settledBox(targetCardHeader);

    // Drag from environment key dot to target card
    await page.mouse.move(envBox.x + envBox.width / 2, envBox.y + envBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(targetBox.x + targetBox.width / 2, targetBox.y + targetBox.height / 2, { steps: 5 });
    await page.mouse.up();

    // Bind menu (wire-picker) opens with env.<key> title
    const picker = page.locator('#wire-picker');
    await expect(picker).toBeVisible();
    await expect(picker.locator('.wire-picker-h')).toContainText('Wire env.clusterName → dead-letter');

    // Filter for region field and select it
    const search = page.locator('#wire-picker-search');
    await search.fill('region');
    const regionItem = picker.locator('.wire-picker-item', { hasText: 'region' }).first();
    await expect(regionItem).toBeVisible();
    await regionItem.click();
    await expect(picker).toBeHidden();

    // Verify doc was updated with from: env.clusterName
    const getRes = await request.get(`${ENGINE}/api/blueprint`);
    const doc = await getRes.json();
    const dl = doc.spec.resources.find((r) => r.name === 'dead-letter');
    expect(dl.fields['region']).toEqual({ from: 'env.clusterName' });

    // Verify wire renders with shared colour
    await canvasSettled(page);
    const wirePath = page.locator('#wires path.wire-shared');
    await expect(wirePath.first()).toBeVisible();
    const stroke = await wirePath.first().getAttribute('stroke');
    expect(stroke).toContain('var(--shared)');
  });
});
