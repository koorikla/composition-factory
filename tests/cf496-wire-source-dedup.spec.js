const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers');

test.describe('CF-496 — Wire source deduplication in inspector dropdown', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('literal repro: wire-source dropdown deduplicates promoted status.atProvider.id option', async ({ page, request }) => {
    // Start with a blueprint containing an SQS Queue and a ServiceAccount
    const doc = {
      apiVersion: 'factory.crossplane.io/v1alpha1',
      kind: 'Blueprint',
      metadata: { name: 'cf496-repro' },
      spec: {
        sources: [
          { provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0' }
        ],
        xrd: {
          group: 'platform.example.org',
          kind: 'XApp',
          plural: 'xapps',
          version: 'v1alpha1',
          scope: 'Namespaced',
          parameters: {
            providerName: { type: 'string', required: true }
          }
        },
        resources: [
          {
            name: 'queue',
            kind: 'Queue',
            provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
            fields: {}
          },
          {
            name: 'sa',
            kind: 'ServiceAccount',
            provider: 'k8s',
            fields: {}
          }
        ]
      }
    };

    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');

    // Select the ServiceAccount card
    await page.click('.node[data-id="sa"] .node-h');

    // Add an annotation: eks.amazonaws.com/role-arn
    const sec = page.locator('#insp .insp-sec', { hasText: /annotations/i });
    await expect(sec).toBeVisible();
    await sec.locator('input[data-ann-key]').fill('eks.amazonaws.com/role-arn');
    await sec.locator('input[data-ann-value]').fill('TBD');
    await sec.locator('button[data-ann-add]').click();

    const row = sec.locator('.ann-row', { has: page.locator('span.ann-key', { hasText: 'eks.amazonaws.com/role-arn' }) });
    await expect(row).toBeVisible();

    // Switch to wire mode
    await row.locator('button[data-m="w"]').click();

    const select = row.locator('select[data-wire="annotations.eks.amazonaws.com/role-arn"]');
    await expect(select).toBeVisible();

    // Enumerate options
    const options = await select.locator('option').evaluateAll(opts =>
      opts.map(o => ({ value: o.value, text: o.textContent.trim() }))
    );

    // Filter out placeholder option
    const nonPlaceholder = options.filter(o => o.value !== '');
    const values = nonPlaceholder.map(o => o.value);
    const valueCounts = {};
    for (const v of values) {
      valueCounts[v] = (valueCounts[v] || 0) + 1;
    }
    const duplicates = Object.keys(valueCounts).filter(v => valueCounts[v] > 1);

    // Contract: One wire source must appear once in the source list (no duplicate option values)
    expect(duplicates).toEqual([]);
    expect(values.length).toBe(new Set(values).size);

    // Contract: Where a path has a friendly name, that name replaces the raw row rather than adding to it
    const idOptions = options.filter(o => o.value === 'resources.queue.status.atProvider.id');
    expect(idOptions).toHaveLength(1);
    expect(idOptions[0].text).toContain('queue (name / ID)');

    // The raw path should NOT appear as an option label
    const rawLabelOptions = options.filter(o => o.text === 'resources.queue.status.atProvider.id');
    expect(rawLabelOptions).toHaveLength(0);
  });

  test('field wire-source dropdown on managed resource replaces raw status.atProvider.id with friendly name', async ({ page, request }) => {
    // Start with blueprint containing two SQS Queues
    const doc = {
      apiVersion: 'factory.crossplane.io/v1alpha1',
      kind: 'Blueprint',
      metadata: { name: 'cf496-fields' },
      spec: {
        sources: [
          { provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0' }
        ],
        xrd: {
          group: 'platform.example.org',
          kind: 'XApp',
          plural: 'xapps',
          version: 'v1alpha1',
          scope: 'Namespaced',
          parameters: {
            providerName: { type: 'string', required: true }
          }
        },
        resources: [
          {
            name: 'primary',
            kind: 'Queue',
            provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
            fields: {}
          },
          {
            name: 'secondary',
            kind: 'Queue',
            provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
            fields: {}
          }
        ]
      }
    };

    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');

    // Select secondary queue and switch to fields view
    await page.click('.node[data-id="secondary"] .node-h');
    await page.click('#fseg button[data-f="all"]');

    // Locate kmsMasterKeyId field
    const row = page.locator('#insp .fld', { hasText: 'kmsMasterKeyId' }).first();
    await expect(row).toBeVisible();

    // Switch to wire mode
    await row.locator('button[data-m="w"]').click();

    const select = page.locator('#insp select[data-wire="kmsMasterKeyId"]');
    await expect(select).toBeVisible();

    const options = await select.locator('option').evaluateAll(opts =>
      opts.map(o => ({ value: o.value, text: o.textContent.trim() }))
    );

    const nonPlaceholder = options.filter(o => o.value !== '');
    const values = nonPlaceholder.map(o => o.value);
    const valueCounts = {};
    for (const v of values) {
      valueCounts[v] = (valueCounts[v] || 0) + 1;
    }
    const duplicates = Object.keys(valueCounts).filter(v => valueCounts[v] > 1);

    expect(duplicates).toEqual([]);
    expect(values.length).toBe(new Set(values).size);

    // Primary queue's status.atProvider.id should only appear once, as primary (name / ID)
    const idOptions = options.filter(o => o.value === 'resources.primary.status.atProvider.id');
    expect(idOptions).toHaveLength(1);
    expect(idOptions[0].text).toContain('primary (name / ID)');
    const rawLabelOptions = options.filter(o => o.text === 'resources.primary.status.atProvider.id');
    expect(rawLabelOptions).toHaveLength(0);
  });
});
