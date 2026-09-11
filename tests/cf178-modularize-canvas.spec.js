// CF-178 — canvas.js couples pure dependency-tree layout with 600 lines of drag-to-wire DOM logic
// Issue #63: Extract pure layout into canvas/layout.js and drag-to-wire into canvas/drag-to-wire.js

const { test, expect } = require('@playwright/test');
const path = require('path');
const { resetDoc, guardPageErrors, canvasSettled, settledBox, ENGINE } = require('./helpers');

guardPageErrors();

// Section 1: Standalone unit tests (pure layout algorithm, runnable in Node without DOM)
test.describe('CF-178: Standalone unit tests for canvas/layout.js', () => {
  let layoutMod;

  test.beforeAll(async () => {
    const layoutPath = path.resolve(__dirname, '../web-proto/js/regions/canvas/layout.js');
    layoutMod = await import(`file://${layoutPath}`);
  });

  test('layout module exports dependencyLayers and computeDependencyLayout', () => {
    expect(typeof layoutMod.dependencyLayers).toBe('function');
    expect(typeof layoutMod.computeDependencyLayout).toBe('function');
  });

  test('dependencyLayers computes topological layer depths from status wires', () => {
    const doc = {
      spec: {
        resources: [
          {
            name: 'queue',
            kind: 'Queue',
            fields: {},
          },
          {
            name: 'topic',
            kind: 'Topic',
            fields: {
              topicArn: { from: 'resources.queue.status.arn' },
            },
          },
          {
            name: 'subscription',
            kind: 'Subscription',
            fields: {
              endpoint: { from: 'resources.topic.status.topicArn' },
            },
          },
          {
            name: 'independent',
            kind: 'Bucket',
            fields: {
              acl: { from: 'params.bucketAcl' },
            },
          },
        ],
      },
    };

    const layers = layoutMod.dependencyLayers(doc);
    // queue has no status inputs -> layer 1
    expect(layers['queue']).toBe(1);
    // topic depends on queue status -> layer 2
    expect(layers['topic']).toBe(2);
    // subscription depends on topic status -> layer 3
    expect(layers['subscription']).toBe(3);
    // independent has no status dependencies -> layer 1
    expect(layers['independent']).toBe(1);
  });

  test('dependencyLayers safely guards against cyclic dependencies', () => {
    const doc = {
      spec: {
        resources: [
          {
            name: 'nodeA',
            kind: 'A',
            fields: {
              f: { from: 'resources.nodeB.status.id' },
            },
          },
          {
            name: 'nodeB',
            kind: 'B',
            fields: {
              f: { from: 'resources.nodeA.status.id' },
            },
          },
        ],
      },
    };

    // Must not crash or stack overflow; cycle guard returns 1
    const layers = layoutMod.dependencyLayers(doc);
    expect(layers['nodeA']).toBeGreaterThanOrEqual(1);
    expect(layers['nodeB']).toBeGreaterThanOrEqual(1);
  });

  test('computeDependencyLayout calculates positions purely without DOM', () => {
    const doc = {
      spec: {
        xrd: { parameters: { region: { type: 'string' } } },
        environment: { stage: { type: 'string' } },
        resources: [
          {
            name: 'vpc',
            kind: 'VPC',
            fields: {},
          },
          {
            name: 'subnet',
            kind: 'Subnet',
            fields: {
              vpcId: { from: 'resources.vpc.status.atProvider.id' },
            },
          },
        ],
      },
    };

    const dummySizes = {
      xrd: { width: 200, height: 100 },
      environment: { width: 200, height: 80 },
      vpc: { width: 220, height: 160 },
      subnet: { width: 220, height: 160 },
    };

    const result = layoutMod.computeDependencyLayout(doc, {
      getSize: (id) => dummySizes[id] || { width: 220, height: 160 },
      getPosition: () => null,
      onlyUnplaced: false,
    });

    expect(result).toBeDefined();
    expect(result.positions).toBeDefined();

    // XR is at (40, 40)
    expect(result.positions['xrd']).toEqual({ x: 40, y: 40 });
    // Environment is stacked below XR at (40, 40 + 100 + 24) = (40, 164)
    expect(result.positions['environment']).toEqual({ x: 40, y: 164 });

    // Layer 1 (vpc) starts to the right of source cards:
    // source width is 200, so x = 40 + 200 + 60 = 300
    expect(result.positions['vpc'].x).toBe(300);
    expect(result.positions['vpc'].y).toBe(40);

    // Layer 2 (subnet) starts to the right of layer 1:
    // x = 300 + 220 + 60 = 580
    expect(result.positions['subnet'].x).toBe(580);
    expect(result.positions['subnet'].y).toBe(40);
  });
});

// Section 2: Standalone unit tests for canvas/drag-to-wire.js helpers
test.describe('CF-178: Standalone unit tests for canvas/drag-to-wire.js', () => {
  let dragMod;

  test.beforeAll(async () => {
    const dragPath = path.resolve(__dirname, '../web-proto/js/regions/canvas/drag-to-wire.js');
    dragMod = await import(`file://${dragPath}`);
  });

  test('drag-to-wire module exports drag handlers and field picker helpers', () => {
    expect(typeof dragMod.initDragToWire).toBe('function');
    expect(typeof dragMod.onWireDragDown).toBe('function');
    expect(typeof dragMod.closeWirePicker).toBe('function');
    expect(typeof dragMod.isFieldPickerTypeMatch).toBe('function');
    expect(typeof dragMod.isReadOnlyMetadataField).toBe('function');
    expect(typeof dragMod.buildFieldPickerCandidates).toBe('function');
  });

  test('isFieldPickerTypeMatch matches compatible and numeric types', () => {
    expect(dragMod.isFieldPickerTypeMatch('string', 'string')).toBe(true);
    expect(dragMod.isFieldPickerTypeMatch('integer', 'number')).toBe(true);
    expect(dragMod.isFieldPickerTypeMatch('number', 'integer')).toBe(true);
    expect(dragMod.isFieldPickerTypeMatch('map', 'object')).toBe(true);
    expect(dragMod.isFieldPickerTypeMatch('boolean', 'string')).toBe(false);
  });

  test('isReadOnlyMetadataField filters read-only k8s metadata', () => {
    expect(dragMod.isReadOnlyMetadataField('metadata.uid')).toBe(true);
    expect(dragMod.isReadOnlyMetadataField('metadata.resourceVersion')).toBe(true);
    expect(dragMod.isReadOnlyMetadataField('metadata.managedFields')).toBe(true);
    expect(dragMod.isReadOnlyMetadataField('metadata.creationTimestamp')).toBe(true);
    expect(dragMod.isReadOnlyMetadataField('metadata.name')).toBe(false);
    expect(dragMod.isReadOnlyMetadataField('metadata.labels')).toBe(false);
  });

  test('buildFieldPickerCandidates scores and ranks matching fields', () => {
    const specFields = [
      { path: 'spec.forProvider.topicArn', type: 'string', required: true },
      { path: 'spec.forProvider.unrelated', type: 'string', required: false },
    ];
    const envelopeFields = [];
    const ctx = {
      srcOwner: 'queue',
      srcPath: 'status.topicArn',
      srcType: 'string',
      resource: { name: 'sub', kind: 'Subscription' },
    };

    const candidates = dragMod.buildFieldPickerCandidates(specFields, envelopeFields, '', ctx);
    expect(candidates.length).toBeGreaterThanOrEqual(2);
    // topicArn should rank highest due to name and type match
    expect(candidates[0].path).toBe('spec.forProvider.topicArn');
    expect(candidates[0].suggested).toBe(true);
  });
});

// Section 3: Browser e2e integration verifying canvas coordinator
test.describe('CF-178: Browser e2e coordination of layout and drag-to-wire', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('canvas loads and coordinator delegates tidy layout and drag-to-wire correctly', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    // Verify canvas cards are rendered
    const cards = page.locator('#canvas .node');
    await expect(cards.first()).toBeVisible();

    // Verify tidy layout button triggers modular layout without errors
    const layoutBtn = page.locator('#layout-btn');
    await expect(layoutBtn).toBeVisible();
    await layoutBtn.click();
    await page.waitForTimeout(300);

    // Verify wires are intact after tidy
    const wires = page.locator('svg.wires path.wire-path');
    expect(await wires.count()).toBeGreaterThanOrEqual(3);

    // Verify wire picker opens when dragging from a port to an opposing card
    const xrdPort = page.locator('.node[data-id="xrd"] .port[data-path="region"] .d.out');
    await expect(xrdPort).toBeVisible();

    const targetNode = page.locator('.node[data-id="work-queue"]');
    await expect(targetNode).toBeVisible();

    const xrdBox = await xrdPort.boundingBox();
    const targetBox = await targetNode.boundingBox();

    // Drag from XRD output port to target node body to open field picker
    await page.mouse.move(xrdBox.x + xrdBox.width / 2, xrdBox.y + xrdBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(targetBox.x + targetBox.width / 2, targetBox.y + targetBox.height / 2, { steps: 5 });
    await page.mouse.up();

    // Verify wire picker modal opened
    const picker = page.locator('#wire-picker');
    await expect(picker).toBeVisible({ timeout: 3000 });

    // Press Escape to verify closeWirePicker works
    await page.keyboard.press('Escape');
    await expect(picker).toBeHidden();
  });

  test('type conversion in field picker clears enum and default to maintain backend schema validity', async ({ page, request }) => {
    page.on('dialog', async d => await d.accept());
    await page.goto('/');
    await canvasSettled(page);

    // Grab $region parameter port dot
    const paramPort = page.locator('.port[data-owner="xrd"][data-path="region"] .d');
    const paramBox = await settledBox(paramPort);

    // Target dead-letter card header
    const card = page.locator('.node[data-id="dead-letter"] .node-h');
    const cardBox = await settledBox(card);

    await page.mouse.move(paramBox.x + paramBox.width / 2, paramBox.y + paramBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(cardBox.x + cardBox.width / 2, cardBox.y + cardBox.height / 2, { steps: 5 });
    await page.mouse.up();

    const picker = page.locator('#wire-picker');
    await expect(picker).toBeVisible();

    const search = page.locator('#wire-picker-search');
    await search.fill('max');
    const items = picker.locator('.wire-picker-item');
    await expect(items.first()).toContainText('maxMessageSize');

    // Click maxMessageSize (triggers type conversion confirm)
    await items.first().click();
    await expect(picker).toBeHidden();

    // Verify wire was committed to doc and parameter type was converted to number with enum cleared
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const dlq = doc.spec.resources.find(r => r.name === 'dead-letter');
      return dlq?.fields?.maxMessageSize?.from;
    }).toBe('params.region');

    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    expect(doc.spec.xrd.parameters.region.type).toBe('number');
    expect(doc.spec.xrd.parameters.region.enum).toBeUndefined();
  });
});

