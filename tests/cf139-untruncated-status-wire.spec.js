const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled, clickWire } = require('./helpers');

guardPageErrors();

test.describe('CF-139 — Untruncated status wire binding', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    doc.spec.resources = [
      {
        name: 'role',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {}
      },
      {
        name: 'service-account',
        kind: 'Queue',
        provider: 'ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0',
        fields: {},
        annotations: {
          'eks.amazonaws.com/role-arn': {
            from: 'resources.role.status.atProvider.arn'
          }
        }
      }
    ];
    const put = await request.put(ENGINE + '/api/blueprint', { data: doc });
    expect(put.ok()).toBeTruthy();
  });

  test('wire selection displays full untruncated binding in canvas wire badge or hint bar', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const wirePath = page.locator('svg.wires path.wire-path.wire-status');
    await expect(wirePath).toHaveCount(1);

    await clickWire(page, 0);

    const badge = page.locator('#wire-badge, .wire-badge, .wire-hint-bar');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('role.status.atProvider.arn');
    await expect(badge).toContainText('eks.amazonaws.com/role-arn');
  });

  test('hovering wire or endpoints displays complete untruncated binding', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const wire = page.locator('svg.wires path.wire-path.wire-status');
    await expect(wire).toHaveAttribute('title', /role\.status\.atProvider\.arn\s*→\s*service-account\.annotations\.eks\.amazonaws\.com\/role-arn/);

    const targetPort = page.locator('.node[data-id="service-account"] .port[data-path="annotations.eks.amazonaws.com/role-arn"]');
    await expect(targetPort).toHaveAttribute('title', /role\.status\.atProvider\.arn\s*→\s*service-account\.annotations\.eks\.amazonaws\.com\/role-arn/);

    const srcPort = page.locator('.node[data-id="role"] .port[data-path="status.atProvider.arn"]');
    await expect(srcPort).toHaveAttribute('title', /role\.status\.atProvider\.arn\s*→\s*service-account\.annotations\.eks\.amazonaws\.com\/role-arn/);
  });

  test('inspector wire row exposes full untruncated binding on hover or click without layout breakage', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    await page.click('.node[data-id="service-account"] .node-h');
    const insp = page.locator('#insp');
    await expect(insp).toBeVisible();

    const annRow = page.locator('#insp .ann-row, #insp .frow:has(.ann-key)').first();
    await expect(annRow).toBeVisible();

    await annRow.click();
    const bindingText = await annRow.evaluate(el => el.getAttribute('title') || el.textContent || '');
    expect(bindingText).toMatch(/role\.status\.atProvider\.arn.*eks\.amazonaws\.com\/role-arn/);

    const hasHorizontalScroll = await insp.evaluate(el => el.scrollWidth > el.clientWidth + 1);
    expect(hasHorizontalScroll).toBe(false);
  });
});
