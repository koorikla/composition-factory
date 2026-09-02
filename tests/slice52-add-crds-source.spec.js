const { test, expect } = require('@playwright/test')
const fs = require('fs')
const { resetDoc, ENGINE } = require('./helpers')

const xrCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: xdatabases.platform.example.org
spec:
  group: platform.example.org
  names: {kind: XDatabase, plural: xdatabases}
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          properties:
            spec:
              properties:
                engine: {type: string}
                sizeGB: {type: integer}
`;

test('uploading a CRD manifest makes its kinds droppable objects', async ({ page, request }) => {
  await resetDoc(request);
  fs.writeFileSync('.testrun/xdatabase-crd.yaml', xrCRD);

  await page.goto('/');
  await expect(page.locator('.node')).toHaveCount(3);

  // SOURCES rail → Add CRDs → pick the manifest
  await page.click('#rtabs button[data-r="src"]');
  const [chooser] = await Promise.all([
    page.waitForEvent('filechooser'),
    page.click('#addCrdsBtn'),
  ]);
  await chooser.setFiles('.testrun/xdatabase-crd.yaml');

  // the scanned kind lands in the KINDS rail
  await page.click('#rtabs button[data-r="kinds"]');
  await expect(page.locator('.kind[data-kind="XDatabase"]')).toBeVisible({ timeout: 10000 });
  await expect(page.locator('.kind[data-kind="XDatabase"]')).toBeVisible();

  // and drops onto the canvas as an object-rooted card
  await page.evaluate(() => {
    const row = document.querySelector('.kind[data-kind="XDatabase"]');
    const cw = document.getElementById('cw');
    const r = cw.getBoundingClientRect();
    const dt = new DataTransfer();
    row.dispatchEvent(new DragEvent('dragstart', { bubbles: true, cancelable: true, dataTransfer: dt }));
    const at = { clientX: r.left + 500, clientY: r.top + 380 };
    cw.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt, ...at }));
    cw.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt, ...at }));
  });
  await expect(page.locator('.node[data-kind="XDatabase"], .node[data-id*="xdatabase"]')).toBeVisible();

  // the doc declares the crds source and the resource references it
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
  expect(doc.spec.sources.some(s => s.crds === 'crds/xdatabase-crd.yaml')).toBeTruthy();
  const res = doc.spec.resources.find(r => r.kind === 'XDatabase');
  expect(res.provider).toBe('crds/xdatabase-crd.yaml');
});
