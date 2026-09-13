const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, dropKind } = require('./helpers');

test.use({ storageState: { cookies: [], origins: [] } });

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

async function resourceNamed(request, name) {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
  return (doc.spec.resources || []).find(r => r.name === name) || null;
}

test.describe('CF-471 — manifest editor, view toggle and search', () => {
  test('Manifest view is the default and nests the starter', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await expect(page.locator('#vseg button[data-view="manifest"]')).toHaveAttribute('aria-pressed', 'true');
    const view = page.locator('#insp pre.manifest-view');
    await expect(view).toBeVisible();
    await expect(view).toContainText('containers:');
    await expect(view).toContainText('image: nginx:1.27');
    await expect(page.locator('#fseg')).toBeHidden();
    await expect(page.locator('#insp .fld')).toHaveCount(0);
  });

  test('edit, apply and add a second ports item', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    await expect(ta).toBeVisible();
    let text = await ta.inputValue();
    text = text.replace('replicas: 2', 'replicas: 3').replace('- containerPort: 80\n', '- containerPort: 80\n            - containerPort: 9090\n');
    await ta.fill(text);
    await page.click('#insp button[data-manifest-apply]');
    await expect(ta).toBeHidden();
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      const f = r.fields;
      return { rep: f['spec.replicas'] && f['spec.replicas'].value, p1: f['spec.template.spec.containers[0].ports[1].containerPort'] && f['spec.template.spec.containers[0].ports[1].containerPort'].value };
    }).toEqual({ rep: '3', p1: '9090' });
  });

  test('an unknown key keeps the editor open and names the line', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    await ta.fill('spec:\n  replicas: 2\n  replicaz: 4\n');
    await page.click('#insp button[data-manifest-apply]');
    await expect(ta).toBeVisible();
    await expect(page.locator('#insp .manifest-err')).toContainText('line 3');
    await expect(page.locator('#insp .manifest-err')).toContainText('spec.replicaz');
  });

  test('Fields toggle restores the old list and search filters it', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#vseg button[data-view="fields"]');
    await expect(page.locator('#fseg')).toBeVisible();
    await page.click('#fseg button[data-f="all"]');
    await expect.poll(() => page.locator('#insp .fld').count()).toBeGreaterThan(100);
    await page.fill('#insp-search', 'imagePullPolicy');
    await expect.poll(() => page.locator('#insp .fld').count()).toBeLessThan(10);
    await expect(page.locator('#insp .fld').first()).toContainText('imagePullPolicy');
  });

  test('search in Manifest view lists schema hits and a click opens Fields view filtered', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.fill('#insp-search', 'terminationGracePeriod');
    const hit = page.locator('#insp .search-hit').first();
    await expect(hit).toContainText('terminationGracePeriodSeconds');
    await hit.click();
    await expect(page.locator('#vseg button[data-view="fields"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#insp .fld').first()).toContainText('terminationGracePeriodSeconds');
  });
});
