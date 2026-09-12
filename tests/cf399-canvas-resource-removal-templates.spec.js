// tests/cf399-canvas-resource-removal-templates.spec.js
import { test, expect } from '@playwright/test';
import helpers from './helpers.js';
import { findDownstreamRefs, cleanDownstreamRefs, isResourceRef } from '../web-proto/js/regions/canvas/resource-ref.js';

const { resetDoc, guardPageErrors, ENGINE } = helpers;

guardPageErrors();

test.describe('CF-399 — Canvas resource removal ignores spec.templates downstream references', () => {
  test('findDownstreamRefs and cleanDownstreamRefs detect and prune spec.templates references', () => {
    const doc = {
      spec: {
        templates: {
          'queue-arn': '{{ (index .observed.resources "queue-a").resource.status.atProvider.arn }}',
          'unrelated': '{{ .name }}'
        },
        resources: [
          { name: 'queue-a', fields: {} },
          { name: 'queue-b', fields: { qArn: { template: 'queue-arn' } } }
        ]
      }
    };

    expect(isResourceRef(doc.spec.templates['queue-arn'], 'queue-a')).toBe(true);

    const downstream = findDownstreamRefs(doc.spec.resources, 'queue-a', doc.spec.templates);
    const templateDep = (downstream || []).find(d => d.name === 'template:queue-arn' || (d.templates && d.templates.includes('queue-arn')));
    expect(templateDep).toBeDefined();

    cleanDownstreamRefs(doc.spec.resources, 'queue-a', doc.spec.templates);
    expect(doc.spec.templates['queue-arn']).toBeUndefined();
    expect(doc.spec.templates['unrelated']).toBeDefined();
  });

  test('cleanDownstreamRefs unwires fields referencing deleted templates and prunes empty templates and conventions', () => {
    const doc = {
      spec: {
        templates: {
          'queue-arn': '{{ (index .observed.resources "queue-a").resource.status.atProvider.arn }}',
        },
        conventions: [
          { match: 'arn', template: 'queue-arn' },
          { match: 'keep', template: 'keep-tpl' },
        ],
        resources: [
          { name: 'queue-a', fields: {} },
          {
            name: 'queue-b',
            fields: {
              qArn: { template: 'queue-arn' },
              keepField: { from: 'params.env' }
            },
            envelope: {
              envArn: { template: 'queue-arn' }
            },
            annotations: {
              annArn: { template: 'queue-arn' }
            }
          }
        ]
      }
    };

    const downstream = findDownstreamRefs(doc.spec.resources, 'queue-a', doc.spec.templates, doc.spec.conventions);
    expect(downstream.find(d => d.name === 'template:queue-arn')).toBeDefined();
    expect(downstream.find(d => d.name === 'convention:queue-arn')).toBeDefined();
    const resBDep = downstream.find(d => d.name === 'queue-b');
    expect(resBDep).toBeDefined();
    expect(resBDep.fields).toContain('qArn');
    expect(resBDep.fields).toContain('envelope.envArn');
    expect(resBDep.fields).toContain('annotations.annArn');

    cleanDownstreamRefs(doc.spec.resources, 'queue-a', doc.spec.templates, doc);
    expect(doc.spec.templates).toBeUndefined();
    expect(doc.spec.conventions).toHaveLength(1);
    expect(doc.spec.conventions[0].template).toBe('keep-tpl');
    const b = doc.spec.resources.find(r => r.name === 'queue-b');
    expect(b.fields.qArn).toBeUndefined();
    expect(b.fields.keepField).toBeDefined();
    expect(b.envelope).toBeUndefined();
    expect(b.annotations).toBeUndefined();
  });

  test('detects template references across dotted, index, hasKey, dig, and getComposedResource expressions', () => {
    const doc = {
      spec: {
        templates: {
          'tpl-dotted': '{{ .observed.resources.target.resource.status.arn }}',
          'tpl-index': '{{ (index $observed.resources "target").resource.status.url }}',
          'tpl-haskey': '{{ hasKey $observed.resources "target" }}',
          'tpl-dig': '{{ hasKey (dig "resources" "target" "resource" "status" dict $.observed) "url" }}',
          'tpl-composed': '{{ getComposedResource $observed "target" }}',
          'tpl-safe': '{{ .name }}',
        },
        resources: [
          { name: 'target', fields: {} }
        ]
      }
    };

    const downstream = findDownstreamRefs(doc.spec.resources, 'target', doc.spec.templates);
    const templateNames = downstream.filter(d => d.type === 'template').map(d => d.name);
    expect(templateNames).toContain('template:tpl-dotted');
    expect(templateNames).toContain('template:tpl-index');
    expect(templateNames).toContain('template:tpl-haskey');
    expect(templateNames).toContain('template:tpl-dig');
    expect(templateNames).toContain('template:tpl-composed');
    expect(templateNames).not.toContain('template:tpl-safe');

    cleanDownstreamRefs(doc.spec.resources, 'target', doc.spec.templates, doc);
    expect(doc.spec.templates['tpl-dotted']).toBeUndefined();
    expect(doc.spec.templates['tpl-index']).toBeUndefined();
    expect(doc.spec.templates['tpl-haskey']).toBeUndefined();
    expect(doc.spec.templates['tpl-dig']).toBeUndefined();
    expect(doc.spec.templates['tpl-composed']).toBeUndefined();
    expect(doc.spec.templates['tpl-safe']).toBeDefined();
  });
});

test.describe('CF-399: Browser e2e canvas removal warns and cleans spec.templates references', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('deleting resource prompts warning for dependent templates and prunes them upon confirmation', async ({ page, request }) => {
    // 1. Prepare doc with work-queue and a template referencing work-queue
    const res = await request.get(ENGINE + '/api/blueprint');
    const doc = await res.json();
    doc.spec.templates = Object.assign({}, doc.spec.templates, {
      'queue-arn': '{{ (index .observed.resources "work-queue").resource.status.atProvider.arn }}',
      'unrelated': '{{ .name }}'
    });
    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc });
    expect(putRes.ok()).toBe(true);

    await page.goto('/');

    await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible();

    // 2. Select work-queue and press Delete
    let dialogMessage = '';
    page.on('dialog', dialog => {
      dialogMessage = dialog.message();
      dialog.accept();
    });

    await page.click('.node[data-id="work-queue"] .node-h');
    await page.keyboard.press('Delete');

    // 3. Confirmation prompt must warn about template:queue-arn
    expect(dialogMessage).toContain('Remove "work-queue"?');
    expect(dialogMessage).toContain('template:queue-arn');

    // 4. Verify work-queue is removed from canvas
    await expect(page.locator('#canvas-error-toast')).toHaveCount(0);
    await expect(page.locator('.node[data-id="work-queue"]')).toHaveCount(0);

    // 5. Verify server blueprint: work-queue deleted, queue-arn template pruned, unrelated preserved
    const updatedDoc = await (await request.get(ENGINE + '/api/blueprint')).json();
    expect(updatedDoc.spec.resources.map(r => r.name)).not.toContain('work-queue');
    expect(updatedDoc.spec.templates['queue-arn']).toBeUndefined();
    expect(updatedDoc.spec.templates['unrelated']).toBeDefined();
  });

  test('canvas deletion prunes spec.templates when all templates are removed', async ({ page, request }) => {
    const res = await request.get(ENGINE + '/api/blueprint');
    const doc = await res.json();
    delete doc.spec.conventions;
    doc.spec.templates = {
      'queue-arn': '{{ (index .observed.resources "work-queue").resource.status.atProvider.arn }}'
    };
    const putRes = await request.put(ENGINE + '/api/blueprint', { data: doc });
    expect(putRes.ok()).toBe(true);

    await page.goto('/');
    await expect(page.locator('.node[data-id="work-queue"]')).toBeVisible();

    page.on('dialog', dialog => dialog.accept());

    await page.click('.node[data-id="work-queue"] .node-h');
    await page.keyboard.press('Delete');

    await expect(page.locator('.node[data-id="work-queue"]')).toHaveCount(0);

    const updatedDoc = await (await request.get(ENGINE + '/api/blueprint')).json();
    expect(updatedDoc.spec.resources.map(r => r.name)).not.toContain('work-queue');
    expect(updatedDoc.spec.templates).toBeUndefined();
  });
});
