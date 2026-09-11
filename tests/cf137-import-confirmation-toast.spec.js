const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

test.describe('CF-137 — Import confirmation, toast, and change summary', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  const COMPOSITION_ROUNDTRIP = `apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xapps.platform.example.org
spec:
  compositeTypeRef:
    apiVersion: platform.example.org/v1alpha1
    kind: XApp
  mode: Pipeline
  pipeline:
    - step: patch-and-transform
      functionRef:
        name: function-patch-and-transform
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          - name: role
            base:
              apiVersion: iam.aws.m.upbound.io/v1beta1
              kind: Role
              spec:
                forProvider:
                  assumeRolePolicyDocument: '{}'
    - step: auto-ready
      functionRef:
        name: function-auto-ready
    - step: custom-env
      functionRef:
        name: function-environment-configs
`;

  test('Import button tooltip names the served blueprint file and indicates replacement is undoable', async ({ page }) => {
    await page.goto('/');

    const importBtn = page.locator('#importBtn');
    await expect(importBtn).toBeVisible();

    // Must name the served blueprint file being replaced (doc.cf.yaml) and note undoable
    await expect(importBtn).toHaveAttribute('title', /doc\.cf\.yaml/);
    await expect(importBtn).toHaveAttribute('title', /undoable/);
  });

  test('Import prompts confirmation before replacing; dismissing cancels the import', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);

    let dialogMessage = '';
    let dialogShown = false;
    page.on('dialog', async (dialog) => {
      dialogShown = true;
      dialogMessage = dialog.message();
      await dialog.dismiss();
    });

    // Hand a file to #importFile
    await page.setInputFiles('#importFile', {
      name: 'composition.yaml',
      mimeType: 'application/yaml',
      buffer: Buffer.from(COMPOSITION_ROUNDTRIP),
    });

    expect(dialogShown).toBe(true);
    expect(dialogMessage).toContain('doc.cf.yaml');
    expect(dialogMessage).toContain('undoable');

    // Document was not replaced because dialog was dismissed
    await expect(page.locator('.node')).toHaveCount(3);
    await expect(page.locator('.node[data-id="role"]')).toHaveCount(0);
  });

  test('Import on accept shows toast naming changed fields and provides discoverable Undo escape hatch', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.node')).toHaveCount(3);

    page.on('dialog', async (dialog) => {
      await dialog.accept();
    });

    // Hand file to #importFile
    await page.setInputFiles('#importFile', {
      name: 'composition.yaml',
      mimeType: 'application/yaml',
      buffer: Buffer.from(COMPOSITION_ROUNDTRIP),
    });

    // Toast appears
    const toast = page.locator('#import-toast, .toast-bar:has-text("Imported"), .toast-bar:has-text("Adopted")');
    await expect(toast).toBeVisible({ timeout: 10000 });

    // Toast names what changed (e.g. name / metadata.name, pipeline)
    const toastText = await toast.textContent();
    expect(toastText).toMatch(/name/i);
    expect(toastText).toMatch(/pipeline/i);

    // Toast provides an Undo link
    const undoLink = toast.locator('.toast-link, button:has-text("Undo")');
    await expect(undoLink).toBeVisible();

    // The new resource is on canvas
    await expect(page.locator('.node[data-id="role"]')).toBeVisible();

    // Clicking Undo in the toast restores previous document
    await undoLink.click();
    await expect(page.locator('.node[data-id="role"]')).toHaveCount(0);
    await expect(page.locator('.node')).toHaveCount(3);
  });

  test('Importing a composition with uncarryable fields names dropped items in feedback', async ({ page }) => {
    const LOSSY = `apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xqueues.example.org
spec:
  group: example.org
  names: {kind: XQueue, plural: xqueues}
  claimNames: {kind: QueueClaim, plural: queueclaims}
  connectionSecretKeys: [endpoint]
  versions:
    - name: v1alpha1
      served: true
      referenceable: true
      schema:
        openAPIV3Schema:
          type: object
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: adopted-lossy
spec:
  compositeTypeRef:
    apiVersion: example.org/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
    - step: render
      functionRef:
        name: function-patch-and-transform
`;

    await page.goto('/');
    page.on('dialog', async (dialog) => {
      await dialog.accept();
    });

    await page.setInputFiles('#importFile', {
      name: 'lossy.yaml',
      mimeType: 'application/yaml',
      buffer: Buffer.from(LOSSY),
    });

    // Toast or warnbar names the uncarryable fields (e.g. claimNames, connectionSecretKeys)
    const toast = page.locator('#import-toast, .toast-bar:has-text("Adopted")');
    await expect(toast).toBeVisible({ timeout: 10000 });
    const toastText = await toast.textContent();
    expect(toastText).toContain('claimNames');
    expect(toastText).toContain('connectionSecretKeys');

    // Undo link is present and working
    const undoLink = toast.locator('.toast-link, button:has-text("Undo")');
    await expect(undoLink).toBeVisible();
    await undoLink.click();
    await expect(page.locator('.node')).toHaveCount(3);
  });
});

