// CF-417 — Native kinds must not render a status.atProvider.id output port
// A kind whose schema declares no status (or no atProvider status) must not present
// status output ports on its card; whatever the canvas offers as a wire source must
// be a source the engine will accept.
const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, dropKind, canvasSettled, clickWire } = require('./helpers');

test.describe('CF-417 — Native kinds render no status.atProvider.id output port', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('native kinds (ServiceAccount, ConfigMap, Secret, Service) do not grow status output ports', async ({ page }) => {
    await page.goto('/');

    // Drop native kinds onto canvas
    await dropKind(page, 'ServiceAccount', 'v1', 400, 150);
    await dropKind(page, 'ConfigMap', 'v1', 400, 300);
    await dropKind(page, 'Secret', 'v1', 400, 450);
    await dropKind(page, 'Service', 'v1', 400, 600);

    const nativeIds = ['service-account', 'config-map', 'secret', 'service'];

    for (const id of nativeIds) {
      const card = page.locator(`.node[data-id="${id}"]`);
      await expect(card).toBeVisible();

      // No status.atProvider.id port
      await expect(card.locator('.port[data-path="status.atProvider.id"]')).toHaveCount(0);

      // No status output ports at all
      await expect(card.locator('.port.status')).toHaveCount(0);

      // No outputs section header
      await expect(card.locator('.node-grp', { hasText: 'outputs' })).toHaveCount(0);
    }

    // Managed resource (work-queue) with status schema still exposes its status outputs
    const wqCard = page.locator('.node[data-id="work-queue"]');
    await expect(wqCard).toBeVisible();
    await expect(wqCard.locator('.port[data-path="status.atProvider.id"]')).toBeVisible();
    await expect(wqCard.locator('.node-grp', { hasText: 'outputs' })).toBeVisible();
  });
});

test.describe('CF-395 — Canvas drops wires for forEach loop bounds referencing XRD parameters or sibling status', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('listWires emits wire descriptors for forEach referencing params, env, and status', async () => {
    const { listWires } = await import('../web-proto/js/wires.js');
    const doc = {
      spec: {
        xrd: {
          parameters: {
            replicas: { type: 'integer', default: 2 }
          }
        },
        environment: {
          COUNT: { type: 'integer' }
        },
        resources: [
          { name: 'res-param', type: 'Queue', forEach: 'params.replicas' },
          { name: 'res-env', type: 'Queue', forEach: 'env.COUNT' },
          { name: 'res-status', type: 'Queue', forEach: 'resources.res-param.status.atProvider.maxMessageSize' },
          { name: 'res-obj', type: 'Queue', forEach: { over: 'params.replicas' } }
        ]
      }
    };

    const wires = listWires(doc);

    const paramWire = wires.find(w => w.resource === 'res-param' && w.path === 'forEach');
    expect(paramWire).toBeDefined();
    expect(paramWire.kind).toBe('param');
    expect(paramWire.param).toBe('replicas');
    expect(paramWire.from).toBe('params.replicas');

    const envWire = wires.find(w => w.resource === 'res-env' && w.path === 'forEach');
    expect(envWire).toBeDefined();
    expect(envWire.kind).toBe('env');
    expect(envWire.envKey).toBe('COUNT');
    expect(envWire.from).toBe('env.COUNT');

    const statusWire = wires.find(w => w.resource === 'res-status' && w.path === 'forEach');
    expect(statusWire).toBeDefined();
    expect(statusWire.kind).toBe('status');
    expect(statusWire.srcResource).toBe('res-param');
    expect(statusWire.srcPath).toBe('atProvider.maxMessageSize');

    const objWire = wires.find(w => w.resource === 'res-obj' && w.path === 'forEach');
    expect(objWire).toBeDefined();
    expect(objWire.kind).toBe('param');
    expect(objWire.param).toBe('replicas');
  });

  test('XRD parameter forEach loop renders input port and connects wire, deleted via deleteWire', async ({ page, request }) => {
    const pristine = require('./fixtures/pristine-doc.json');
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.replicas = {
      type: 'integer',
      required: false,
      default: 2
    };
    doc.spec.resources[0].forEach = 'params.replicas';

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 1. Resource node card renders an input port for forEach
    const wqCard = page.locator('.node[data-id="work-queue"]');
    await expect(wqCard).toBeVisible();
    const port = wqCard.locator('.port[data-owner="work-queue"][data-path="forEach"]');
    await expect(port).toBeVisible();
    await expect(port.locator('.d.in')).toBeVisible();

    // 2. SVG canvas renders visible wire connecting XRD parameter to forEach port
    const wirePath = page.locator('svg.wires path.wire-path[title*="forEach"]');
    await expect(wirePath).toHaveCount(1);
    await expect(wirePath).toBeVisible();

    // 3. Click the wire to select it
    const idx = await page.locator('svg.wires path.wire-path[title*="forEach"]').getAttribute('data-wire-idx');
    await clickWire(page, Number(idx));
    await expect(page.locator('svg.wires path.wire-path.wire-selected[title*="forEach"]')).toBeVisible();

    // 4. Delete the wire via Backspace
    await page.keyboard.press('Backspace');

    // 5. deleteWire removes forEach from blueprint and canvas
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updated = await res.json();
      const r = updated.spec.resources.find(res => res.name === 'work-queue');
      return r ? r.forEach : undefined;
    }).toBeUndefined();

    await expect(page.locator('svg.wires path.wire-path[title*="forEach"]')).toHaveCount(0);
    await expect(wqCard.locator('.port[data-path="forEach"]')).toHaveCount(0);
  });

  test('Sibling status forEach loop renders input port and connects status wire, deleted via deleteWire', async ({ page, request }) => {
    const pristine = require('./fixtures/pristine-doc.json');
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.resources[1].forEach = 'resources.work-queue.status.atProvider.maxMessageSize';

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 1. Resource node card renders an input port for forEach
    const dlCard = page.locator('.node[data-id="dead-letter"]');
    await expect(dlCard).toBeVisible();
    const port = dlCard.locator('.port[data-owner="dead-letter"][data-path="forEach"]');
    await expect(port).toBeVisible();
    await expect(port.locator('.d.in')).toBeVisible();

    // 2. SVG canvas renders status wire connecting work-queue status to dead-letter forEach port
    const wirePath = page.locator('svg.wires path.wire-path[title*="forEach"]');
    await expect(wirePath).toHaveCount(1);
    await expect(wirePath).toBeVisible();
    await expect(page.locator('svg.wires path.wire-path.wire-status[title*="forEach"]')).toHaveCount(1);

    // 3. Click and delete wire
    const idx = await page.locator('svg.wires path.wire-path[title*="forEach"]').getAttribute('data-wire-idx');
    await clickWire(page, Number(idx));
    await expect(page.locator('svg.wires path.wire-path.wire-selected[title*="forEach"]')).toBeVisible();
    await page.keyboard.press('Delete');

    // 4. deleteWire removes forEach from dead-letter
    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updated = await res.json();
      const r = updated.spec.resources.find(res => res.name === 'dead-letter');
      return r ? r.forEach : undefined;
    }).toBeUndefined();

    await expect(page.locator('svg.wires path.wire-path[title*="forEach"]')).toHaveCount(0);
  });

  test('EnvironmentConfig forEach loop renders input port and connects shared wire', async ({ page, request }) => {
    const pristine = require('./fixtures/pristine-doc.json');
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.environment = {
      SCALE: { type: 'integer', default: 3 }
    };
    doc.spec.resources[1].forEach = 'env.SCALE';

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 1. Resource node card renders an input port for forEach
    const dlCard = page.locator('.node[data-id="dead-letter"]');
    await expect(dlCard).toBeVisible();
    const port = dlCard.locator('.port[data-owner="dead-letter"][data-path="forEach"]');
    await expect(port).toBeVisible();
    await expect(port.locator('.d.in')).toBeVisible();

    // 2. SVG canvas renders shared wire connecting EnvironmentConfig to dead-letter forEach port
    const wirePath = page.locator('svg.wires path.wire-path.wire-shared[title*="forEach"]');
    await expect(wirePath).toHaveCount(1);
    await expect(wirePath).toBeVisible();
  });
});
