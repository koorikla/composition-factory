const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-408 (#299) — Inspector parameter and member type change wire cleanup', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('changing object parameter with wired member to string prompts and unwires fields', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.cfg = {
      type: 'object',
      properties: {
        loc: {
          type: 'string',
          default: 'us-east-1'
        }
      }
    };
    doc.spec.resources = [
      {
        name: 'dead-letter',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {
          region: { from: 'params.cfg.loc' }
        }
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    if (!putRes.ok()) {
      throw new Error(`PUT /api/blueprint failed (${putRes.status()}): ${await putRes.text()}`);
    }

    await page.goto('/');
    await canvasSettled(page);

    // Open XRD inspector
    const xrdCard = page.locator('.node[data-id="xrd"]');
    await expect(xrdCard).toBeVisible();
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    const typeSelect = inspector.locator('select[data-pt="cfg"]');
    await expect(typeSelect).toBeVisible();
    await expect(typeSelect).toHaveValue('object');

    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    await typeSelect.selectOption('string');

    expect(dialogMessage).toBe(
      'Parameter "cfg" is wired to 1 field. Changing type to string will remove its properties and unwire those fields. Proceed?'
    );

    // Verify no warnbar error or error toast
    await expect(inspector.locator('.warnbar')).toHaveCount(0);
    const toast = page.locator('#errtoast, #toast.err');
    await expect(toast).not.toBeVisible();

    // Verify parameter type changed in inspector
    await expect(typeSelect).toHaveValue('string');

    // Verify backend blueprint
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const cfg = updatedDoc.spec.xrd.parameters && updatedDoc.spec.xrd.parameters.cfg;
      const dl = (updatedDoc.spec.resources || []).find((r) => r.name === 'dead-letter');
      const isWireRemoved = !dl || !dl.fields || !dl.fields.region;
      const isTypeString = cfg && cfg.type === 'string' && !cfg.properties;
      return isWireRemoved && isTypeString;
    }).toBe(true);
  });

  test('cancelling parameter type change unwire dialog preserves wires and reverts type selector', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.cfg = {
      type: 'object',
      properties: {
        loc: {
          type: 'string',
          default: 'us-east-1'
        }
      }
    };
    doc.spec.resources = [
      {
        name: 'dead-letter',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {
          region: { from: 'params.cfg.loc' }
        }
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    if (!putRes.ok()) {
      throw new Error(`PUT /api/blueprint failed (${putRes.status()}): ${await putRes.text()}`);
    }

    await page.goto('/');
    await canvasSettled(page);

    const xrdCard = page.locator('.node[data-id="xrd"]');
    await expect(xrdCard).toBeVisible();
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    const typeSelect = inspector.locator('select[data-pt="cfg"]');
    await expect(typeSelect).toBeVisible();
    await expect(typeSelect).toHaveValue('object');

    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.dismiss();
    });

    await typeSelect.selectOption('string');

    expect(dialogMessage).toBe(
      'Parameter "cfg" is wired to 1 field. Changing type to string will remove its properties and unwire those fields. Proceed?'
    );

    // Reverts to object
    await expect(typeSelect).toHaveValue('object');

    // Verify wire remains in backend
    const res = await request.get(`${ENGINE}/api/blueprint`);
    const currentDoc = await res.json();
    const dl = (currentDoc.spec.resources || []).find((r) => r.name === 'dead-letter');
    expect(dl.fields.region.from).toBe('params.cfg.loc');
    expect(currentDoc.spec.xrd.parameters.cfg.type).toBe('object');
  });

  test('changing nested object member to string prompts and unwires child references', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.cfg = {
      type: 'object',
      properties: {
        net: {
          type: 'object',
          properties: {
            subnet: {
              type: 'string',
              default: 'subnet-123'
            }
          }
        }
      }
    };
    doc.spec.resources = [
      {
        name: 'dead-letter',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {
          region: { from: 'params.cfg.net.subnet' }
        }
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    if (!putRes.ok()) {
      throw new Error(`PUT /api/blueprint failed (${putRes.status()}): ${await putRes.text()}`);
    }

    await page.goto('/');
    await canvasSettled(page);

    const xrdCard = page.locator('.node[data-id="xrd"]');
    await expect(xrdCard).toBeVisible();
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    const memberTypeSelect = inspector.locator('select[data-mtype="cfg|net"]');
    await expect(memberTypeSelect).toBeVisible();
    await expect(memberTypeSelect).toHaveValue('object');

    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    await memberTypeSelect.selectOption('string');

    expect(dialogMessage).toBe(
      'Member "cfg.net" is wired to 1 field. Changing type to string will remove its properties and unwire those fields. Proceed?'
    );

    // Verify no error toast or warnbar
    await expect(inspector.locator('.warnbar')).toHaveCount(0);
    const toast = page.locator('#errtoast, #toast.err');
    await expect(toast).not.toBeVisible();

    // Verify backend blueprint
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const cfg = updatedDoc.spec.xrd.parameters && updatedDoc.spec.xrd.parameters.cfg;
      const net = cfg && cfg.properties && cfg.properties.net;
      const dl = (updatedDoc.spec.resources || []).find((r) => r.name === 'dead-letter');
      const isWireRemoved = !dl || !dl.fields || !dl.fields.region;
      const isNetString = net && net.type === 'string' && !net.properties;
      return isWireRemoved && isNetString;
    }).toBe(true);
  });

  test('changing directly-wired scalar parameter to object prompts and unwires referencing fields', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.region = {
      type: 'string',
      default: 'us-east-1'
    };
    doc.spec.resources = [
      {
        name: 'dead-letter',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {
          region: { from: 'params.region' }
        }
      }
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    if (!putRes.ok()) {
      throw new Error(`PUT /api/blueprint failed (${putRes.status()}): ${await putRes.text()}`);
    }

    await page.goto('/');
    await canvasSettled(page);

    const xrdCard = page.locator('.node[data-id="xrd"]');
    await expect(xrdCard).toBeVisible();
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    const typeSelect = inspector.locator('select[data-pt="region"]');
    await expect(typeSelect).toBeVisible();
    await expect(typeSelect).toHaveValue('string');

    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    await typeSelect.selectOption('object');

    expect(dialogMessage).toBe(
      'Parameter "region" is wired to 1 field. Changing type to object will remove its properties and unwire those fields. Proceed?'
    );

    // Verify no error toast or warnbar
    await expect(inspector.locator('.warnbar')).toHaveCount(0);
    const toast = page.locator('#errtoast, #toast.err');
    await expect(toast).not.toBeVisible();

    // Verify backend blueprint
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const reg = updatedDoc.spec.xrd.parameters && updatedDoc.spec.xrd.parameters.region;
      const dl = (updatedDoc.spec.resources || []).find((r) => r.name === 'dead-letter');
      const isWireRemoved = !dl || !dl.fields || !dl.fields.region;
      const isTypeObject = reg && reg.type === 'object';
      return isWireRemoved && isTypeObject;
    }).toBe(true);
  });

  test('unwired parameter type change does not prompt confirmation', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.unwiredParam = {
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
    await expect(xrdCard).toBeVisible();
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();

    const typeSelect = inspector.locator('select[data-pt="unwiredParam"]');
    await expect(typeSelect).toBeVisible();

    page.on('dialog', async (dialog) => {
      throw new Error(`Unexpected dialog prompted: ${dialog.message()}`);
    });

    await typeSelect.selectOption('string');

    await expect(typeSelect).toHaveValue('string');
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const p = updatedDoc.spec.xrd.parameters && updatedDoc.spec.xrd.parameters.unwiredParam;
      return p && p.type === 'string' && !p.properties;
    }).toBe(true);
  });
});
