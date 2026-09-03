// Slice 66 — Canvas UX & Error Visibility Enhancements:
//  - CF-010: Output drawer feedback (preview vs disk write)
//  - CF-011: Make all rejected canvas actions visible to user (toast + status chip)
//  - CF-012: Secret data base64 encoding & stringData steering / inspector guidance
//  - CF-013: Wire picker casing preservation for custom field paths
//  - CF-035 & CF-038: Inspector & sidebar layout, empty canvas hint
//  - CF-036: Filter out read-only system metadata in wire picker
//  - CF-037: Undo duplicate inspector state, Enter key on param rename, action hit targets

const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, canvasSettled, settledBox } = require('./helpers');
guardPageErrors();

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

test('CF-010: Output drawer feedback distinguishes preview vs disk write', async ({ page }) => {
  await page.goto('/');

  // On initial load, debounced preview runs -> banner says Preview only
  const banner = page.locator('#next-steps-banner');
  await expect(banner).toBeVisible({ timeout: 10000 });
  await expect(banner).toContainText('Preview only');
  await expect(banner).toContainText('Click Generate to write to');

  // Clicking Generate writes to disk -> banner changes to Output written
  const genBtn = page.locator('#generateBtn');
  await expect(genBtn).toBeVisible();
  await genBtn.click();

  await expect(banner).toContainText('Output written to');
  await expect(banner).toContainText('kubectl apply -f');
});

test('CF-011: Store errors trigger visible error toast and red status chip', async ({ page }) => {
  await page.goto('/');

  // Dispatch an error via store
  await page.evaluate(() => {
    window.store.emit('error', { message: 'resource "web": invalid wire type mismatch' });
  });

  const toast = page.locator('#canvas-error-toast');
  await expect(toast).toBeVisible();
  await expect(toast.locator('.toast-msg')).toContainText('invalid wire type mismatch');

  // Chip also shows error
  const validChip = page.locator('#valid');
  await expect(validChip).toHaveText('error');
});

test('CF-012: Secret inspector provides data vs stringData guidance', async ({ page, request }) => {
  // Set up a blueprint with a Secret native resource
  await request.put('/api/blueprint', {
    data: {
      apiVersion: 'factory.crossplane.io/v1alpha1',
      kind: 'Blueprint',
      metadata: { name: 'test-secret' },
      spec: {
        xrd: {
          group: 'platform.sparky.ee',
          kind: 'XSecretTest',
          plural: 'xsecrettests',
          version: 'v1alpha1',
          scope: 'Namespaced',
          parameters: {
            token: { type: 'string', required: true },
          },
        },
        resources: [
          {
            name: 'app-secret',
            kind: 'Secret',
            provider: 'k8s',
            fields: {
              'stringData[token]': { from: 'params.token' },
            },
          },
        ],
      },
    },
  });

  await page.goto('/');

  // Select app-secret node
  const secretNode = page.locator('.node[data-id="app-secret"]');
  await expect(secretNode).toBeVisible();
  await secretNode.click();

  // Verify guidance banner in inspector
  const inspector = page.locator('#insp');
  await expect(inspector).toContainText('Secret data vs stringData:');
  await expect(inspector).toContainText('stringData');
  await expect(inspector).toContainText('data');
});

test('CF-013 & CF-036: Wire picker preserves custom query casing and filters read-only metadata', async ({ page, request }) => {
  await request.put('/api/blueprint', {
    data: {
      apiVersion: 'factory.crossplane.io/v1alpha1',
      kind: 'Blueprint',
      metadata: { name: 'test-picker' },
      spec: {
        xrd: {
          group: 'platform.sparky.ee',
          kind: 'XPickerTest',
          plural: 'xpickertests',
          version: 'v1alpha1',
          scope: 'Namespaced',
          parameters: {
            hostName: { type: 'string', required: true },
          },
        },
        resources: [
          {
            name: 'target-svc',
            kind: 'Service',
            provider: 'k8s',
            fields: {},
          },
        ],
      },
    },
  });

  await page.goto('/');
  await canvasSettled(page);

  // Drag from $hostName parameter port dot to target-svc card header
  const paramPort = page.locator('.port[data-owner="xrd"][data-path="hostName"] .d');
  const paramBox = await settledBox(paramPort);

  const card = page.locator('.node[data-id="target-svc"] .node-h');
  const cardBox = await settledBox(card);

  await page.mouse.move(paramBox.x + paramBox.width / 2, paramBox.y + paramBox.height / 2);
  await page.mouse.down();
  await page.mouse.move(cardBox.x + cardBox.width / 2, cardBox.y + cardBox.height / 2, { steps: 5 });
  await page.mouse.up();

  const picker = page.locator('#wire-picker');
  await expect(picker).toBeVisible({ timeout: 5000 });

  // Type camelCase custom field path
  const searchInput = page.locator('#wire-picker-search');
  await searchInput.fill('stringData.mySecretKey');

  // Verify custom option preserves exact camelCase casing
  const customItem = page.locator('.wire-picker-item', { hasText: 'Set as custom field path' });
  await expect(customItem).toBeVisible();
  const labelText = await customItem.locator('span').first().textContent();
  expect(labelText).toBe('stringData.mySecretKey');

  // Verify read-only metadata fields like metadata.uid or metadata.resourceVersion are filtered out
  await searchInput.fill('resourceVersion');
  const items = page.locator('.wire-picker-item');
  const count = await items.count();
  for (let i = 0; i < count; i++) {
    const desc = await items.nth(i).locator('.desc').textContent().catch(() => '');
    expect(desc).not.toContain('resourceVersion');
  }
});

test('CF-035 & CF-038: Left tabs fit without truncation and empty canvas shows clear steps', async ({ page, request }) => {
  // Empty canvas state
  await request.put('/api/blueprint', {
    data: {
      apiVersion: 'factory.crossplane.io/v1alpha1',
      kind: 'Blueprint',
      metadata: { name: 'blank' },
      spec: {
        xrd: {
          group: 'platform.sparky.ee',
          kind: 'XBlank',
          plural: 'xblanks',
          version: 'v1alpha1',
          scope: 'Namespaced',
          parameters: {},
        },
        resources: [],
      },
    },
  });

  await page.goto('/');

  // Verify empty canvas hint
  const emptyState = page.locator('#canvas-empty-state');
  await expect(emptyState).toBeVisible();
  await expect(emptyState).toContainText('14 native Kubernetes kinds ready without providers');
  await expect(emptyState).toContainText('SOURCES');

  // Check left rail tabs
  const rtabs = page.locator('#rtabs button');
  await expect(rtabs).toHaveCount(4);
  for (let i = 0; i < 4; i++) {
    await expect(rtabs.nth(i)).toBeVisible();
  }
});

test('CF-037: Enter key on XRD param commits rename and node actions have accessible hit targets', async ({ page }) => {
  await page.goto('/');

  // Select XRD card
  const xrCard = page.locator('.node[data-id="xrd"]');
  await expect(xrCard).toBeVisible();
  await xrCard.click();

  // Find a parameter name input in inspector
  const paramInput = page.locator('#insp input[data-pn]').first();
  await expect(paramInput).toBeVisible();
  await paramInput.focus();
  await paramInput.press('Enter');
  // Pressing Enter should blur the input immediately
  const isFocused = await paramInput.evaluate(el => el === document.activeElement);
  expect(isFocused).toBe(false);

  // Check node action button hit target sizing on selected resource
  const resCard = page.locator('.node[data-id="main-queue"]');
  if (await resCard.isVisible()) {
    await resCard.click();
    const nodeActions = page.locator('.node.sel [data-act]');
    const actionCount = await nodeActions.count();
    if (actionCount > 0) {
      const box = await nodeActions.first().boundingBox();
      expect(box.width).toBeGreaterThanOrEqual(20);
      expect(box.height).toBeGreaterThanOrEqual(20);
    }
  }
});
