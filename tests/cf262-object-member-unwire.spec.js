const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-262 (#150) — Deleting or renaming an object parameter member when wired', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('deleting a wired member prompts confirmation, unwires referencing fields, and deletes cleanly', async ({ page, request }) => {
    // 1. Load canvas with a blueprint having object parameter `cfg` with member `region` wired to `q.fields.region`.
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.cfg = {
      type: 'object',
      properties: {
        region: {
          type: 'string',
          default: 'us-east-1',
        },
      },
    };
    doc.spec.resources = [
      {
        name: 'q',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {
          region: { from: 'params.cfg.region' },
        },
      },
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    if (!putRes.ok()) {
      throw new Error(`PUT /api/blueprint failed (${putRes.status()}): ${await putRes.text()}`);
    }

    await page.goto('/');
    await canvasSettled(page);

    // Verify initial wire on canvas resource port
    const qPort = page.locator('.port[data-owner="q"][data-path="region"]');
    await expect(qPort).toBeVisible();
    await expect(qPort).toHaveAttribute('title', /\$cfg\.region/);

    // 2. Inspect parameter cfg.
    const xrdCard = page.locator('.node[data-id="xrd"]');
    await expect(xrdCard).toBeVisible();
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();
    const memberNameInput = inspector.locator('input[data-mname="cfg|region"]');
    await expect(memberNameInput).toBeVisible();

    // 3. Click delete member ([data-mdel]).
    // 4. Accept unwire confirmation dialog.
    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    const delBtn = inspector.locator('button[data-mdel="cfg|region"]');
    await expect(delBtn).toBeVisible();
    await delBtn.click();

    expect(dialogMessage).toBe('Member "cfg.region" is wired into 1 field. Delete it and unwire all referencing fields?');

    // 5. Verify member is removed, no error toast is displayed, and wire on canvas is cleanly removed.
    const toast = page.locator('#errtoast, #toast.err');
    await expect(toast).not.toBeVisible();

    await expect(inspector.locator('input[data-mname="cfg|region"]')).toHaveCount(0);
    await canvasSettled(page);
    await expect(qPort).not.toHaveAttribute('title', /\$cfg\.region/);

    // Verify backend blueprint state
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const cfg = (updatedDoc.spec.xrd.parameters || {}).cfg;
      const hasRegionMember = !!(cfg && cfg.properties && cfg.properties.region);
      const q = (updatedDoc.spec.resources || []).find((r) => r.name === 'q');
      const hasRegionWire = !!(q && q.fields && q.fields.region);
      return hasRegionMember || hasRegionWire;
    }).toBe(false);
  });

  test('cancelling delete dialog preserves wired member and wires', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.cfg = {
      type: 'object',
      properties: {
        region: {
          type: 'string',
          default: 'us-east-1',
        },
      },
    };
    doc.spec.resources = [
      {
        name: 'q',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {
          region: { from: 'params.cfg.region' },
        },
      },
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBe(true);

    await page.goto('/');
    await canvasSettled(page);

    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    const memberNameInput = inspector.locator('input[data-mname="cfg|region"]');
    await expect(memberNameInput).toBeVisible();

    page.on('dialog', async (dialog) => {
      await dialog.dismiss();
    });

    const delBtn = inspector.locator('button[data-mdel="cfg|region"]');
    await delBtn.click();

    await expect(page.locator('#errtoast, #toast.err')).not.toBeVisible();
    await expect(inspector.locator('input[data-mname="cfg|region"]')).toBeVisible();

    const qPort = page.locator('.port[data-owner="q"][data-path="region"]');
    await expect(qPort).toHaveAttribute('title', /\$cfg\.region/);
  });

  test('renaming a wired member cascades the new path to referencing fields, envelope, and annotations without error', async ({ page, request }) => {
    // 1. Load canvas with a blueprint having object parameter `cfg` with member `region` wired to field, envelope, and annotations.
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.cfg = {
      type: 'object',
      properties: {
        region: {
          type: 'string',
          default: 'us-east-1',
        },
      },
    };
    doc.spec.resources = [
      {
        name: 'q',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {
          region: { from: 'params.cfg.region' },
        },
        envelope: {
          'writeConnectionSecretToRef.name': { from: 'params.cfg.region' },
        },
        annotations: {
          'example.com/region': { from: 'params.cfg.region' },
        },
      },
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    if (!putRes.ok()) {
      throw new Error(`PUT /api/blueprint failed (${putRes.status()}): ${await putRes.text()}`);
    }

    await page.goto('/');
    await canvasSettled(page);

    // Verify initial wire on canvas resource port
    const qPort = page.locator('.port[data-owner="q"][data-path="region"]');
    await expect(qPort).toBeVisible();
    await expect(qPort).toHaveAttribute('title', /\$cfg\.region/);

    // 2. Inspect parameter cfg.
    const xrdCard = page.locator('.node[data-id="xrd"]');
    await expect(xrdCard).toBeVisible();
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector).toBeVisible();
    const memberNameInput = inspector.locator('input[data-mname="cfg|region"]');
    await expect(memberNameInput).toBeVisible();

    // 6. Test rename: with wired member region, rename to awsRegion; verify field wire updates to params.cfg.awsRegion without error.
    await memberNameInput.fill('awsRegion');
    await memberNameInput.blur();

    const toast = page.locator('#errtoast, #toast.err');
    await expect(toast).not.toBeVisible();

    // Verify member input is now awsRegion
    await expect(inspector.locator('input[data-mname="cfg|awsRegion"]')).toBeVisible();
    await expect(inspector.locator('input[data-mname="cfg|awsRegion"]')).toHaveValue('awsRegion');

    // Verify canvas resource port wire updated to $cfg.awsRegion
    await canvasSettled(page);
    await expect(qPort).toHaveAttribute('title', /\$cfg\.awsRegion/);

    // Verify backend blueprint has updated parameter property and field/envelope/annotation from:
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const cfg = (updatedDoc.spec.xrd.parameters || {}).cfg;
      const hasOldMember = !!(cfg && cfg.properties && cfg.properties.region);
      const hasNewMember = !!(cfg && cfg.properties && cfg.properties.awsRegion);
      const q = (updatedDoc.spec.resources || []).find((r) => r.name === 'q');
      const wireFrom = (q && q.fields && q.fields.region && q.fields.region.from) || '';
      const envFrom = (q && q.envelope && q.envelope['writeConnectionSecretToRef.name'] && q.envelope['writeConnectionSecretToRef.name'].from) || '';
      const annFrom = (q && q.annotations && q.annotations['example.com/region'] && q.annotations['example.com/region'].from) || '';
      return {
        hasOldMember,
        hasNewMember,
        wireFrom,
        envFrom,
        annFrom,
      };
    }).toEqual({
      hasOldMember: false,
      hasNewMember: true,
      wireFrom: 'params.cfg.awsRegion',
      envFrom: 'params.cfg.awsRegion',
      annFrom: 'params.cfg.awsRegion',
    });
  });

  test('unwired object parameter member deletes and renames without dialog prompt', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.cfg = {
      type: 'object',
      properties: {
        unwired1: { type: 'string' },
        unwired2: { type: 'integer' },
      },
    };

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBe(true);

    await page.goto('/');
    await canvasSettled(page);

    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    await expect(inspector.locator('input[data-mname="cfg|unwired1"]')).toBeVisible();

    let dialogPrompted = false;
    page.on('dialog', async (dialog) => {
      dialogPrompted = true;
      await dialog.accept();
    });

    // Rename unwired member
    const nameInput = inspector.locator('input[data-mname="cfg|unwired1"]');
    await nameInput.fill('renamed1');
    await nameInput.blur();

    expect(dialogPrompted).toBe(false);
    await expect(page.locator('#errtoast, #toast.err')).not.toBeVisible();
    await expect(inspector.locator('input[data-mname="cfg|renamed1"]')).toBeVisible();

    // Delete unwired member
    const delBtn = inspector.locator('button[data-mdel="cfg|renamed1"]');
    await delBtn.click();

    expect(dialogPrompted).toBe(false);
    await expect(page.locator('#errtoast, #toast.err')).not.toBeVisible();
    await expect(inspector.locator('input[data-mname="cfg|renamed1"]')).toHaveCount(0);

    // Verify backend
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const cfg = (updatedDoc.spec.xrd.parameters || {}).cfg;
      return {
        hasRenamed1: !!(cfg && cfg.properties && cfg.properties.renamed1),
        hasUnwired2: !!(cfg && cfg.properties && cfg.properties.unwired2),
      };
    }).toEqual({
      hasRenamed1: false,
      hasUnwired2: true,
    });
  });

  test('nested object parameter member renames and deletes with cascading unwire', async ({ page, request }) => {
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.cfg = {
      type: 'object',
      properties: {
        net: {
          type: 'object',
          properties: {
            cidr: {
              type: 'string',
              default: '10.0.0.0/16',
            },
          },
        },
      },
    };
    doc.spec.resources = [
      {
        name: 'q',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {
          region: { from: 'params.cfg.net.cidr' },
        },
      },
    ];

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBe(true);

    await page.goto('/');
    await canvasSettled(page);

    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();

    const inspector = page.locator('#region-inspector');
    const nestedInput = inspector.locator('input[data-mname="cfg|net.cidr"]');
    await expect(nestedInput).toBeVisible();

    // 1. Rename nested member net.cidr -> net.ipRange
    await nestedInput.fill('ipRange');
    await nestedInput.blur();

    await expect(page.locator('#errtoast, #toast.err')).not.toBeVisible();
    await expect(inspector.locator('input[data-mname="cfg|net.ipRange"]')).toBeVisible();

    // Verify backend has updated wire
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const q = (updatedDoc.spec.resources || []).find((r) => r.name === 'q');
      return (q && q.fields && q.fields.region && q.fields.region.from) || '';
    }).toBe('params.cfg.net.ipRange');

    // 2. Delete nested member net.ipRange
    let dialogMessage = '';
    page.on('dialog', async (dialog) => {
      dialogMessage = dialog.message();
      await dialog.accept();
    });

    const delBtn = inspector.locator('button[data-mdel="cfg|net.ipRange"]');
    await delBtn.click();

    expect(dialogMessage).toBe('Member "cfg.net.ipRange" is wired into 1 field. Delete it and unwire all referencing fields?');
    await expect(page.locator('#errtoast, #toast.err')).not.toBeVisible();
    await expect(inspector.locator('input[data-mname="cfg|net.ipRange"]')).toHaveCount(0);

    // Verify backend unwired
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updatedDoc = await res.json();
      const q = (updatedDoc.spec.resources || []).find((r) => r.name === 'q');
      return !!(q && q.fields && q.fields.region);
    }).toBe(false);
  });
});
