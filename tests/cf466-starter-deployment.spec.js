const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, dropKind } = require('./helpers');

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

async function resourceNamed(request, name) {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
  return (doc.spec.resources || []).find(r => r.name === name) || null;
}

test.describe('CF-466 — starters are valid good-practice minimums in map-entry grammar', () => {
  test('dropping a Deployment writes the full starter with no raw JSON', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await expect(page.locator('.node[data-id="deployment"]')).toBeVisible();

    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      if (!r || !r.fields) return null;
      const f = r.fields;
      return {
        replicas: f['spec.replicas'] && f['spec.replicas'].value,
        selector: f['spec.selector.matchLabels[app]'] && f['spec.selector.matchLabels[app]'].value,
        labels: f['spec.template.metadata.labels[app]'] && f['spec.template.metadata.labels[app]'].value,
        cName: f['spec.template.spec.containers[0].name'] && f['spec.template.spec.containers[0].name'].value,
        image: f['spec.template.spec.containers[0].image'] && f['spec.template.spec.containers[0].image'].value,
        port: f['spec.template.spec.containers[0].ports[0].containerPort'] && f['spec.template.spec.containers[0].ports[0].containerPort'].value,
        cpu: f['spec.template.spec.containers[0].resources.requests[cpu]'] && f['spec.template.spec.containers[0].resources.requests[cpu]'].value,
        mem: f['spec.template.spec.containers[0].resources.requests[memory]'] && f['spec.template.spec.containers[0].resources.requests[memory]'].value,
        memLimit: f['spec.template.spec.containers[0].resources.limits[memory]'] && f['spec.template.spec.containers[0].resources.limits[memory]'].value,
        anyRaw: Object.values(f).some(e => e && e.raw),
        oldRawSelector: !!f['spec.selector.matchLabels'],
      };
    }).toEqual({
      replicas: '2', selector: 'deployment', labels: 'deployment', cName: 'deployment',
      image: 'nginx:1.27', port: '80', cpu: '100m', mem: '128Mi', memLimit: '256Mi',
      anyRaw: false, oldRawSelector: false,
    });
  });

  test('the starter generates a container and passes render validation', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await expect(page.locator('.node[data-id="deployment"]')).toBeVisible();
    await expect.poll(async () => !!(await resourceNamed(request, 'deployment'))).toBe(true);

    const gen = await (await request.post(ENGINE + '/api/generate', { data: { write: false } })).json();
    const comp = gen.outputs.find(o => o.path.includes('compositions/'));
    expect(comp.body).toContain("image: 'nginx:1.27'");
    expect(comp.body).toContain('containerPort: 80');
    expect(comp.body).toContain("cpu: '100m'");

    const render = await (await request.post(ENGINE + '/api/render')).json();
    expect(render.ok).toBe(true);
  });

  test('Service starter targets app label with bracket grammar and ClusterIP', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Service', 'v1', 400, 300);
    await expect(page.locator('.node[data-id="service"]')).toBeVisible();
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'service');
      if (!r || !r.fields) return null;
      const f = r.fields;
      return {
        sel: f['spec.selector[app]'] && f['spec.selector[app]'].value,
        port: f['spec.ports[0].port'] && f['spec.ports[0].port'].value,
        target: f['spec.ports[0].targetPort'] && f['spec.ports[0].targetPort'].value,
        type: f['spec.type'] && f['spec.type'].value,
        anyRaw: Object.values(f).some(e => e && e.raw),
      };
    }).toEqual({ sel: 'service', port: '80', target: '80', type: 'ClusterIP', anyRaw: false });
  });

  test('Sync on the workload card writes bracket entries, not raw JSON', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    const card = page.locator('.workload-card');
    await expect(card).toBeVisible();
    await card.locator('input[data-wl-app]').fill('web-prod_v1');
    await card.locator('button[data-wl-sync-app]').click();
    await expect(card.locator('.chip-ok')).toContainText('Selectors Aligned');
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      return r && r.fields['spec.selector.matchLabels[app]'] && r.fields['spec.selector.matchLabels[app]'].value;
    }).toBe('web-prod_v1');
  });

  test('Scaffold on a legacy blueprint supersedes raw whole-map labels so Generate accepts the doc', async ({ page, request }) => {
    // A blueprint written by the pre-fix drop: labels as raw JSON, no container.
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    doc.spec.resources.push({
      name: 'legacy-deploy',
      kind: 'Deployment',
      provider: 'k8s',
      fields: {
        'spec.selector.matchLabels': { raw: '{"app":"legacy-deploy"}' },
        'spec.template.metadata.labels': { raw: '{"app":"legacy-deploy"}' },
      },
    });
    expect((await request.put(ENGINE + '/api/blueprint', { data: doc })).status()).toBe(200);

    await page.goto('/');
    await page.click('.node[data-id="legacy-deploy"] .node-h');
    const btn = page.locator('#insp [data-scaffold-required="legacy-deploy"]');
    await expect(btn).toBeVisible();
    await btn.click();

    await expect.poll(async () => {
      const r = await resourceNamed(request, 'legacy-deploy');
      if (!r || !r.fields) return null;
      const f = r.fields;
      return {
        image: f['spec.template.spec.containers[0].image'] && f['spec.template.spec.containers[0].image'].value,
        bracketBesideRaw: !!f['spec.selector.matchLabels[app]'] || !!f['spec.template.metadata.labels[app]'],
        rawKept: !!f['spec.selector.matchLabels'] && !!f['spec.template.metadata.labels'],
      };
    }).toEqual({ image: 'nginx:1.27', bracketBesideRaw: false, rawKept: true });

    const gen = await request.post(ENGINE + '/api/generate', { data: { write: false } });
    expect(gen.status()).toBe(200);
    const comp = (await gen.json()).outputs.find(o => o.path.includes('compositions/'));
    expect(comp.body).toContain("image: 'nginx:1.27'");
  });

  test('Service quick-match writes the [app] selector and keeps the starter port', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 200, 200);
    await expect(page.locator('.node[data-id="deployment"]')).toBeVisible();
    await dropKind(page, 'Service', 'v1', 500, 320);
    await expect(page.locator('.node[data-id="service"]')).toBeVisible();

    await page.click('.node[data-id="service"] .node-h');
    const card = page.locator('.service-card');
    await expect(card).toBeVisible();
    await card.locator('button[data-svc-match-wl]').first().click();

    await expect.poll(async () => {
      const r = await resourceNamed(request, 'service');
      if (!r || !r.fields) return null;
      const f = r.fields;
      return {
        sel: f['spec.selector[app]'] && f['spec.selector[app]'].value,
        port: f['spec.ports[0].port'] && f['spec.ports[0].port'].value,
        target: f['spec.ports[0].targetPort'] && f['spec.ports[0].targetPort'].value,
        anyRaw: Object.values(f).some(e => e && e.raw),
      };
    }).toEqual({ sel: 'deployment', port: '80', target: '80', anyRaw: false });
  });

  test('a whole-raw pod spec satisfies the required check at any depth', async ({ page, request }) => {
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    doc.spec.resources.push({
      name: 'raw-pod-deploy',
      kind: 'Deployment',
      provider: 'k8s',
      fields: {
        'spec.selector.matchLabels[app]': { value: 'raw-pod-deploy' },
        'spec.template.metadata.labels[app]': { value: 'raw-pod-deploy' },
        'spec.template.spec': { raw: "{containers: [{name: raw-pod-deploy, image: 'nginx:1.27'}]}" },
      },
    });
    expect((await request.put(ENGINE + '/api/blueprint', { data: doc })).status()).toBe(200);
    // The engine accepts this grammar, so the inspector must not call it incomplete.
    expect((await request.post(ENGINE + '/api/generate', { data: { write: false } })).status()).toBe(200);

    await page.goto('/');
    await page.click('.node[data-id="raw-pod-deploy"] .node-h');
    await expect(page.locator('#insp .workload-card')).toBeVisible();
    await expect(page.locator('#insp [data-scaffold-required]')).toHaveCount(0);
  });
});
