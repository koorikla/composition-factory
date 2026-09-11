const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, ENGINE } = require('./helpers');

test.describe('CF-154 — Linking two managed resources (bucketRef.name → Bucket)', () => {
  guardPageErrors();

  const s3Doc = {
    apiVersion: 'factory.crossplane.io/v1alpha1',
    kind: 'Blueprint',
    metadata: { name: 's3-bucket-link' },
    spec: {
      sources: [
        { provider: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0' }
      ],
      xrd: {
        group: 'storage.example.org',
        kind: 'XStorage',
        plural: 'xstorages',
        version: 'v1alpha1',
        scope: 'Namespaced',
        parameters: {
          providerName: { type: 'string', required: true },
          region: { type: 'string', default: 'us-east-1', required: true }
        }
      },
      resources: [
        {
          name: 'bucket',
          kind: 'Bucket',
          provider: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0',
          fields: {}
        },
        {
          name: 'bucket-sse',
          kind: 'BucketServerSideEncryptionConfiguration',
          provider: 'ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0',
          fields: {}
        }
      ]
    }
  };

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
    const put = await request.put(ENGINE + '/api/blueprint', { data: s3Doc });
    expect(put.ok()).toBe(true);
  });

  test('required *Ref.name field appears on the managed resource card on the canvas as a droppable input row', async ({ page }) => {
    await page.goto('/');

    const sseCard = page.locator('.node[data-id="bucket-sse"]');
    await expect(sseCard).toBeVisible();

    // Contract 1: A required *Ref.name field (bucketRef.name) appears on the managed resource card as an input row
    const refPort = sseCard.locator('.port[data-path="bucketRef.name"]');
    await expect(refPort).toBeVisible();
    await expect(refPort).toHaveClass(/req/);
    await expect(refPort.locator('.d.in')).toBeVisible();
    await expect(refPort.locator('.nm')).toHaveText('bucketRef.name');
  });

  test('a resource own name/ID is offered as an output source on the card', async ({ page }) => {
    await page.goto('/');

    const bucketCard = page.locator('.node[data-id="bucket"]');
    await expect(bucketCard).toBeVisible();

    // Contract 2: A resource's own name is offered as a source on the card (an output dot / row for resource name/ID)
    const idPort = bucketCard.locator('.port[data-path="status.atProvider.id"]');
    await expect(idPort).toBeVisible();
    await expect(idPort.locator('.d.out')).toBeVisible();
    const portText = await idPort.locator('.nm').textContent();
    expect(portText).toMatch(/name\s*\/\s*id|id/i);
  });

  test('the wire menu in inspector ranks sibling-resource names above status paths', async ({ page }) => {
    await page.goto('/');

    // Select bucket-sse card
    await page.click('.node[data-id="bucket-sse"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    // Locate bucketRef.name row in inspector
    const row = page.locator('#insp .fld', { hasText: 'bucketRef.name' }).first();
    await expect(row).toBeVisible();

    // Switch to wire mode
    await row.locator('button[data-m="w"]').click();

    const sel = page.locator('#insp select[data-wire="bucketRef.name"]');
    await expect(sel).toBeVisible();

    // Get all options in the wire menu
    const optionValues = await sel.locator('option').evaluateAll(opts => opts.map(o => ({ value: o.value, text: o.textContent.trim() })));

    // Find the index of the sibling resource name (bucket name / ID) option
    const siblingOptIndex = optionValues.findIndex(o =>
      o.text.includes('bucket (name / ID)') || o.value === 'resources.bucket.status.atProvider.id' || o.value === 'resources.bucket.metadata.name'
    );
    expect(siblingOptIndex).toBeGreaterThan(-1);

    // Find the index of status.atProvider.* options for the sibling
    const statusIndices = optionValues
      .map((o, idx) => ({ ...o, idx }))
      .filter(o => o.value.startsWith('resources.bucket.status.atProvider.') && o.value !== 'resources.bucket.status.atProvider.id')
      .map(o => o.idx);

    // Contract 3: Sibling-resource name ranks above status paths (resources.<res>.metadata.name or sibling name ranks above status.atProvider.*)
    expect(statusIndices.length).toBeGreaterThan(0);
    for (const statusIdx of statusIndices) {
      expect(siblingOptIndex).toBeLessThan(statusIdx);
    }
  });

  test('dragging resource name/ID output dot to bucketRef.name input creates wire', async ({ page, request }) => {
    await page.goto('/');

    const bucketCard = page.locator('.node[data-id="bucket"]');
    const sseCard = page.locator('.node[data-id="bucket-sse"]');
    await expect(bucketCard).toBeVisible();
    await expect(sseCard).toBeVisible();

    const idOutDot = bucketCard.locator('.port[data-path="status.atProvider.id"] .d.out');
    const refInPort = sseCard.locator('.port[data-path="bucketRef.name"]');
    await expect(idOutDot).toBeVisible();
    await expect(refInPort).toBeVisible();

    // Drag from bucket name/ID output dot to bucketRef.name input
    await idOutDot.hover();
    await page.mouse.down();
    await refInPort.hover();
    await page.mouse.up();

    // Verify wire created in blueprint
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const sse = doc.spec.resources.find(r => r.name === 'bucket-sse');
      return sse && sse.fields && sse.fields['bucketRef.name'] ? sse.fields['bucketRef.name'].from : null;
    }).toMatch(/^resources\.bucket\.(status\.atProvider\.id|metadata\.name)$/);

    // Verify wire renders on canvas
    await expect(page.locator('#wires path.wire-status').first()).toBeVisible();
  });
});
