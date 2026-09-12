const { test, expect } = require('@playwright/test');
const path = require('path');
const { resetDoc, guardPageErrors, ENGINE } = require('./helpers');

guardPageErrors();

test.describe('CF-412: Standalone unit tests for isResourceRef and downstream refs', () => {
  let refMod;

  test.beforeAll(async () => {
    const refPath = path.resolve(__dirname, '../web-proto/js/regions/canvas/resource-ref.js');
    refMod = await import(`file://${refPath}`);
  });

  function getIsResourceRef() {
    return refMod.isResourceRef;
  }

  test('repro: isResourceRef detects raw index expression on observed.resources', () => {
    const isResourceRef = getIsResourceRef();
    const raw = '{{ (index $observed.resources "queue").resource.status.url }}';
    expect(isResourceRef(raw, 'queue')).toBe(true);
  });

  test('isResourceRef detects index expressions across quotes and scopes', () => {
    const isResourceRef = getIsResourceRef();
    expect(isResourceRef('{{ (index $observed.resources "queue").resource.status.url }}', 'queue')).toBe(true);
    expect(isResourceRef('(index .observed.resources "queue")', 'queue')).toBe(true);
    expect(isResourceRef('{{ index $.observed.resources "queue" }}', 'queue')).toBe(true);
    expect(isResourceRef('{{ index observed.resources "queue" }}', 'queue')).toBe(true);
    expect(isResourceRef('{{ index resources "queue" }}', 'queue')).toBe(true);
    expect(isResourceRef("{{ index .observed.resources 'queue' }}", 'queue')).toBe(true);
    expect(isResourceRef('{{ index .observed.resources `queue` }}', 'queue')).toBe(true);

    // Prefix sharing should not match
    expect(isResourceRef('{{ (index $observed.resources "queue-dlq").resource.status.url }}', 'queue')).toBe(false);
  });

  test('isResourceRef detects hasKey expressions across quotes and scopes', () => {
    const isResourceRef = getIsResourceRef();
    expect(isResourceRef('{{ hasKey $observed.resources "queue" }}', 'queue')).toBe(true);
    expect(isResourceRef('{{- if hasKey $.observed.resources "queue" }}ready{{ end }}', 'queue')).toBe(true);
    expect(isResourceRef("{{ hasKey .observed.resources 'queue' }}", 'queue')).toBe(true);
    expect(isResourceRef('{{ hasKey observed.resources `queue` }}', 'queue')).toBe(true);
    expect(isResourceRef('{{ hasKey resources "queue" }}', 'queue')).toBe(true);

    // Prefix sharing should not match
    expect(isResourceRef('{{ hasKey $observed.resources "queue-dlq" }}', 'queue')).toBe(false);
  });

  test('isResourceRef detects dig expressions across quotes and scopes', () => {
    const isResourceRef = getIsResourceRef();
    expect(isResourceRef('{{ hasKey (dig "resources" "queue" "resource" "status" dict $.observed) "url" }}', 'queue')).toBe(true);
    expect(isResourceRef('{{ (dig "resources" "queue" "resource" "status" dict $.observed) }}', 'queue')).toBe(true);
    expect(isResourceRef("{{ dig 'resources' 'queue' 'status' dict $.observed }}", 'queue')).toBe(true);
    expect(isResourceRef('{{ dig `resources` `queue` `status` dict $.observed }}', 'queue')).toBe(true);
    expect(isResourceRef('{{ dig "observed" "resources" "queue" dict $ }}', 'queue')).toBe(true);

    // Prefix sharing should not match
    expect(isResourceRef('{{ hasKey (dig "resources" "queue-dlq" "resource" "status" dict $.observed) "url" }}', 'queue')).toBe(false);
  });

  test('isResourceRef detects getComposedResource expressions across quotes and contexts', () => {
    const isResourceRef = getIsResourceRef();
    expect(isResourceRef('{{ getComposedResource $observed "queue" }}', 'queue')).toBe(true);
    expect(isResourceRef('{{ (getComposedResource . "queue").status.id }}', 'queue')).toBe(true);
    expect(isResourceRef("{{ (getComposedResource $ 'queue').status.id }}", 'queue')).toBe(true);
    expect(isResourceRef('{{ (getComposedResource $. "queue").status.id }}', 'queue')).toBe(true);
    expect(isResourceRef('{{ (getComposedResource $item `queue`).status.id }}', 'queue')).toBe(true);
    expect(isResourceRef('{{ (getResourceCondition "Ready" (getComposedResource . "queue")).Status }}', 'queue')).toBe(true);

    // Prefix sharing should not match
    expect(isResourceRef('{{ (getComposedResource . "queue-dlq").status.id }}', 'queue')).toBe(false);
  });

  test('isResourceRef maintains existing dotted resource references', () => {
    const isResourceRef = getIsResourceRef();
    expect(isResourceRef('resources.queue.status.atProvider.url', 'queue')).toBe(true);
    expect(isResourceRef('resources.queue', 'queue')).toBe(true);
    expect(isResourceRef('{{ .observed.resources.queue.resource.status.url }}', 'queue')).toBe(true);
    expect(isResourceRef('{{ $.observed.resources.queue.resource.status.url }}', 'queue')).toBe(true);
    expect(isResourceRef('{{ $observed.resources.queue.resource.status.url }}', 'queue')).toBe(true);

    // Prefix sharing
    expect(isResourceRef('resources.queue-dlq.status', 'queue')).toBe(false);
    expect(isResourceRef('unrelated.string', 'queue')).toBe(false);
  });

  test('findDownstreamRefs and cleanDownstreamRefs identify and unwire raw function references', () => {
    expect(refMod).toBeDefined();
    expect(typeof refMod.findDownstreamRefs).toBe('function');
    expect(typeof refMod.cleanDownstreamRefs).toBe('function');

    const resources = [
      {
        name: 'queue',
        kind: 'Queue',
        fields: {},
      },
      {
        name: 'worker',
        kind: 'Worker',
        fields: {
          queueUrl: { raw: '{{ (index $observed.resources "queue").resource.status.url }}' },
          hasQueue: { raw: '{{ hasKey $observed.resources "queue" }}' },
          digRef: { raw: '{{ hasKey (dig "resources" "queue" "resource" "status" dict $.observed) "url" }}' },
          composedRef: { raw: '{{ getComposedResource $observed "queue" }}' },
          safeField: { value: 'keep-me' },
        },
        envelope: {
          envQueue: { raw: '{{ (getComposedResource . "queue").status.id }}' },
        },
        annotations: {
          annQueue: { raw: '{{ hasKey $.observed.resources "queue" }}' },
        },
      },
    ];

    const downstream = refMod.findDownstreamRefs(resources, 'queue');
    expect(downstream).toHaveLength(1);
    expect(downstream[0].name).toBe('worker');
    expect(downstream[0].fields).toContain('queueUrl');
    expect(downstream[0].fields).toContain('hasQueue');
    expect(downstream[0].fields).toContain('digRef');
    expect(downstream[0].fields).toContain('composedRef');
    expect(downstream[0].fields).toContain('envelope.envQueue');
    expect(downstream[0].fields).toContain('annotations.annQueue');
    expect(downstream[0].fields).not.toContain('safeField');

    refMod.cleanDownstreamRefs(resources, 'queue');
    const worker = resources.find(r => r.name === 'worker');
    expect(worker.fields.queueUrl).toBeUndefined();
    expect(worker.fields.hasQueue).toBeUndefined();
    expect(worker.fields.digRef).toBeUndefined();
    expect(worker.fields.composedRef).toBeUndefined();
    expect(worker.fields.safeField).toEqual({ value: 'keep-me' });
    expect(worker.envelope).toBeUndefined();
    expect(worker.annotations).toBeUndefined();
  });
});

test.describe('CF-412: Browser e2e canvas removal unwires downstream raw function expressions', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('canvas deletion of resource prompts confirmation and cleans raw template references', async ({ page, request }) => {
    // 1. Prepare doc with work-queue and dead-letter referencing work-queue via raw expressions
    const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
    const dlq = doc.spec.resources.find(r => r.name === 'dead-letter');
    dlq.fields = {
      region: { from: 'params.region' },
      policy: { raw: '{{ (index $observed.resources "work-queue").resource.status.url }}' },
      tags: { raw: '{{ hasKey $observed.resources "work-queue" }}' },
    };
    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc });
    expect(putRes.ok()).toBe(true);

    await page.goto('/');

    await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible();
    await expect(page.locator('.node[data-id="dead-letter"]')).toBeVisible();

    // 2. Select work-queue and press Delete
    let dialogMessage = '';
    page.on('dialog', dialog => {
      dialogMessage = dialog.message();
      dialog.accept();
    });

    await page.click('.node[data-id="work-queue"] .node-h');
    await page.keyboard.press('Delete');

    // 3. Confirmation prompt must alert about downstream references in dead-letter (policy, tags)
    expect(dialogMessage).toContain('Remove "work-queue"?');
    expect(dialogMessage).toContain('Downstream references in dead-letter (policy, tags) will be unwired.');

    // 4. Verify no error toast appears and work-queue is removed from canvas
    await expect(page.locator('#canvas-error-toast')).toHaveCount(0);
    await expect(page.locator('.node[data-id="work-queue"]')).toHaveCount(0);
    await expect(page.locator('.node[data-id="dead-letter"]')).toBeVisible();

    // 5. Verify server blueprint: work-queue deleted, dead-letter raw fields cleaned, region kept intact
    const updatedDoc = await (await request.get(ENGINE + '/api/blueprint')).json();
    expect(updatedDoc.spec.resources.map(r => r.name)).not.toContain('work-queue');

    const updatedDlq = updatedDoc.spec.resources.find(r => r.name === 'dead-letter');
    expect(updatedDlq).toBeDefined();
    expect(updatedDlq.fields.policy).toBeUndefined();
    expect(updatedDlq.fields.tags).toBeUndefined();
    expect(updatedDlq.fields.region).toBeDefined();
  });
});
