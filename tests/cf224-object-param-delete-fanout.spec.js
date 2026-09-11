const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-224 (#110) — Object parameters in XRD inspector delete button and fan-out', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('object parameter fan-out reflects nested wires and delete button unwires references', async ({ page, request }) => {
    // 1. Seed blueprint with an object parameter `config` with member `tier`, wired to main-queue.region
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.config = {
      type: 'object',
      properties: {
        tier: {
          type: 'string',
          default: 'standard'
        }
      }
    };
    doc.spec.resources = [
      {
        name: 'main-queue',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {
          region: { from: 'params.config.tier' }
        }
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    if (!putRes.ok()) {
      throw new Error(`PUT /api/blueprint failed (${putRes.status()}): ${await putRes.text()}`);
    }

    await page.goto('/');
    await canvasSettled(page);

    // 2. Select XRD card to open XRD inspector
    const xrdCard = page.locator('.node[data-id="xrd"]');
    await expect(xrdCard).toBeVisible();
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();
    await expect(inspector.locator('input[data-pn="config"]')).toBeVisible();

    // 3. Verify fan-out badge for `config` shows ×1, not ×0
    const fanBadge = inspector.locator('.fld:has(input[data-pn="config"]) .fan');
    await expect(fanBadge).toHaveText('×1');
    await expect(fanBadge).toHaveAttribute('title', 'Wired into 1 field');

    // 4. Verify delete button exists for `config`
    const delBtn = inspector.locator('button[data-pd="config"]');
    await expect(delBtn).toBeVisible();

    // 5. Setup dialog handler to capture confirmation message and accept
    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    // 6. Click delete button for parameter `config`
    await delBtn.click();

    // 7. Verify confirmation dialog text
    expect(dialogMessage).toBe('Parameter "config" is wired into 1 field. Delete it and unwire all referencing fields?');

    // 8. Verify no error toast appears
    const toast = page.locator('#errtoast');
    await expect(toast).not.toBeVisible();

    // 9. Verify parameter `config` is removed from inspector
    await expect(inspector.locator('input[data-pn="config"]')).toHaveCount(0);

    // 10. Verify backend blueprint has removed `config` and unwired `region` on main-queue
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const hasParam = 'config' in (updatedDoc.spec.xrd.parameters || {});
      const q = (updatedDoc.spec.resources || []).find((r) => r.name === 'main-queue');
      const hasWire = q && q.fields && q.fields.region;
      return hasParam || !!hasWire;
    }).toBe(false);
  });

  test('unwired object parameter has delete button and deletes without confirmation prompt', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.unwiredObj = {
      type: 'object',
      properties: {
        key: { type: 'string' }
      }
    };

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    if (!putRes.ok()) {
      throw new Error(`PUT /api/blueprint failed (${putRes.status()}): ${await putRes.text()}`);
    }

    await page.goto('/');
    await canvasSettled(page);

    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector.locator('input[data-pn="unwiredObj"]')).toBeVisible();

    // Fan-out is 0
    const fanBadge = inspector.locator('.fld:has(input[data-pn="unwiredObj"]) .fan');
    await expect(fanBadge).toHaveText('×0');

    let dialogPrompted = false;
    page.on('dialog', async (dialog) => {
      dialogPrompted = true;
      await dialog.accept();
    });

    const delBtn = inspector.locator('button[data-pd="unwiredObj"]');
    await expect(delBtn).toBeVisible();
    await delBtn.click();

    expect(dialogPrompted).toBe(false);
    await expect(page.locator('#errtoast')).not.toBeVisible();
    await expect(inspector.locator('input[data-pn="unwiredObj"]')).toHaveCount(0);

    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      return 'unwiredObj' in (updatedDoc.spec.xrd.parameters || {});
    }).toBe(false);
  });
});
