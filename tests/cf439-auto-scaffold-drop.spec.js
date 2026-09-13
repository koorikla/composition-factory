const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, dropKind } = require('./helpers');

guardPageErrors();

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

test.describe('CF-439 — Auto-scaffold minimum required fields on canvas drop and inspector action', () => {
  test('dropping a provider resource (Queue) onto canvas auto-scaffolds region', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Queue', 'v1beta1', 400, 300);
    const card = page.locator('.node[data-id="queue"]');
    await expect(card).toBeVisible();

    // Verify fields persisted server-side with scaffolded values
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const r = (doc.spec.resources || []).find(x => x.name === 'queue');
      return r && r.fields && r.fields.region && r.fields.region.value;
    }).toBe('us-east-1');
  });

  test('dropping a native workload (StatefulSet) onto canvas auto-scaffolds selector, template labels, container name and image', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'StatefulSet', 'apps/v1', 400, 300);
    const card = page.locator('.node[data-id="stateful-set"]');
    await expect(card).toBeVisible();

    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const r = (doc.spec.resources || []).find(x => x.name === 'stateful-set');
      if (!r || !r.fields) return null;
      return {
        hasSelector: !!r.fields['spec.selector.matchLabels[app]'],
        hasLabels: !!r.fields['spec.template.metadata.labels[app]'],
        cName: r.fields['spec.template.spec.containers[0].name'] && r.fields['spec.template.spec.containers[0].name'].value,
        cImage: r.fields['spec.template.spec.containers[0].image'] && r.fields['spec.template.spec.containers[0].image'].value,
      };
    }).toEqual({
      hasSelector: true,
      hasLabels: true,
      cName: 'stateful-set',
      cImage: 'nginx:1.27',
    });
  });

  test('inspector provides 1-click action to scaffold required fields on empty provider resource', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    doc.spec.resources.push({
      name: 'empty-queue',
      kind: 'Queue',
      provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
      fields: {}
    });
    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc });
    expect(putRes.status()).toBe(200);

    await page.goto('/');
    await page.click('.node[data-id="empty-queue"] .node-h');

    const scaffoldBtn = page.locator('#insp [data-scaffold-required="empty-queue"], #insp button:has-text("Scaffold Required Fields")').first();
    await expect(scaffoldBtn).toBeVisible();

    await scaffoldBtn.click();

    await expect.poll(async () => {
      const updatedDoc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const r = updatedDoc.spec.resources.find(x => x.name === 'empty-queue');
      return r && r.fields && r.fields.region && r.fields.region.value;
    }).toBe('us-east-1');

    // Button disappears once required fields are satisfied
    await expect(page.locator('#insp [data-scaffold-required="empty-queue"]')).toHaveCount(0);
  });

  test('inspector provides 1-click action to scaffold native workload (Deployment) required fields', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    doc.spec.resources.push({
      name: 'empty-deployment',
      kind: 'Deployment',
      provider: 'k8s',
      fields: {}
    });
    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc });
    expect(putRes.status()).toBe(200);

    await page.goto('/');
    await page.click('.node[data-id="empty-deployment"] .node-h');

    const scaffoldBtn = page.locator('#insp [data-scaffold-required="empty-deployment"], #insp button:has-text("Scaffold Required Fields")').first();
    await expect(scaffoldBtn).toBeVisible();

    await scaffoldBtn.click();

    await expect.poll(async () => {
      const updatedDoc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const r = updatedDoc.spec.resources.find(x => x.name === 'empty-deployment');
      if (!r || !r.fields) return null;
      return {
        hasSelector: !!r.fields['spec.selector.matchLabels[app]'],
        hasLabels: !!r.fields['spec.template.metadata.labels[app]'],
        cName: r.fields['spec.template.spec.containers[0].name'] && r.fields['spec.template.spec.containers[0].name'].value,
        cImage: r.fields['spec.template.spec.containers[0].image'] && r.fields['spec.template.spec.containers[0].image'].value,
      };
    }).toEqual({
      hasSelector: true,
      hasLabels: true,
      cName: 'empty-deployment',
      cImage: 'nginx:1.27',
    });
  });

  test('freshly dropped provider resource (Queue) passes generate validation and writes cleanly', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Queue', 'v1beta1', 400, 300);
    await expect(page.locator('.node[data-id="queue"]')).toBeVisible();

    const genRes = await request.post(ENGINE + '/api/generate', { data: { write: true } });
    expect(genRes.status()).toBe(200);
  });
});
