const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, dropKind } = require('./helpers');

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

async function resourceNamed(request, name) {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
  return (doc.spec.resources || []).find(r => r.name === name) || null;
}

const IMG = 'spec.template.spec.containers[0].image';
const ENV = 'spec.template.spec.containers[0].env';

test.describe('CF-467 — essentials form', () => {
  test('Deployment opens with typed essentials rows and editing image commits a value', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    const ess = page.locator('#insp .essentials');
    await expect(ess).toBeVisible();
    await expect(ess.locator('[data-ess-row="spec.replicas"] input[type="number"]')).toHaveValue('2');
    const img = ess.locator(`[data-ess-row="${IMG}"] input.val`);
    await expect(img).toHaveValue('nginx:1.27');
    await img.fill('ghcr.io/acme/web:1.4.2');
    await img.press('Enter');
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      return r && r.fields[IMG] && r.fields[IMG].value;
    }).toBe('ghcr.io/acme/web:1.4.2');
  });

  test('expose creates an XRD parameter with the literal as default and wires the field', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.locator(`#insp [data-ess-row="${IMG}"] button[data-expose]`).click();
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const p = doc.spec.xrd.parameters.image;
      const r = doc.spec.resources.find(x => x.name === 'deployment');
      return { pType: p && p.type, pDefault: p && p.default, from: r && r.fields[IMG] && r.fields[IMG].from };
    }).toEqual({ pType: 'string', pDefault: 'nginx:1.27', from: 'params.image' });
    await expect(page.locator(`#insp [data-ess-row="${IMG}"] .bound`)).toContainText('params.image');
    await expect(page.locator('.node[data-id="xrd"]')).toContainText('image');
  });

  test('env repeater adds a second variable and delete renumbers', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    const ess = page.locator('#insp .essentials');
    await ess.locator(`button[data-env-row-add="${ENV}"]`).click();
    await ess.locator(`button[data-env-row-add="${ENV}"]`).click();
    await expect(ess.locator('[data-env-row]')).toHaveCount(2);
    const name1 = ess.locator(`input[data-v="${ENV}[1].name"]`);
    await name1.fill('DB_NAME');
    await name1.press('Enter');
    const val1 = ess.locator(`input[data-v="${ENV}[1].value"]`);
    await val1.fill('orders');
    await val1.press('Enter');
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      return r && r.fields[ENV + '[1].name'] && r.fields[ENV + '[1].name'].value + '=' + (r.fields[ENV + '[1].value'] || {}).value;
    }).toBe('DB_NAME=orders');

    await ess.locator(`button[data-env-row-del="${ENV}|0"]`).click();
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      const f = r.fields;
      return { n0: f[ENV + '[0].name'] && f[ENV + '[0].name'].value, has1: !!f[ENV + '[1].name'] };
    }).toEqual({ n0: 'DB_NAME', has1: false });
  });

  test('a provider kind without a profile shows required leaves plus set fields only', async ({ page, request }) => {
    await page.goto('/');
    await page.click('.node[data-id="work-queue"] .node-h');
    const ess = page.locator('#insp .essentials');
    await expect(ess).toBeVisible();
    await expect(ess.locator('[data-ess-row="region"]')).toHaveCount(1);
    const rows = await ess.locator('[data-ess-row]').count();
    expect(rows).toBeLessThan(10);
  });
});
