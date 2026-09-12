const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors, canvasSettled } = require('./helpers');
const pristine = require('./fixtures/pristine-doc.json');

test.describe('CF-358 — fanOut raw parameter references in resource fields and connection secret', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('pure fanOut and fanOutMap count parameter references in raw fields, envelope, annotations, connectionSecret', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const counts = await page.evaluate(async () => {
      const { fanOut, fanOutMap } = await import('/js/wires.js');

      const doc = {
        apiVersion: 'factory.crossplane.io/v1alpha1',
        kind: 'Blueprint',
        metadata: { name: 'test-raw-fanout' },
        spec: {
          xrd: {
            parameters: {
              policy: { type: 'string' },
              region: { type: 'string' },
              db: { type: 'object', properties: { host: { type: 'string' } } },
              secretKey: { type: 'string' },
              annoParam: { type: 'string' },
              envParam: { type: 'string' }
            }
          },
          resources: [
            {
              name: 'work-queue',
              type: 'Queue',
              fields: {
                policy: { raw: '{{ $spec.policy }}' },
                region: { raw: '{{ .spec.region }}' },
                dbHost: { raw: '{{ $spec.db.host }}' }
              },
              envelope: {
                target: { raw: '{{ $spec.envParam }}' }
              },
              annotations: {
                'example.com/anno': { raw: '{{ $params.policy }}' },
                'example.com/other': { raw: '{{ $spec.annoParam }}' }
              },
              connectionSecret: {
                name: '{{ $spec.secretKey }}'
              }
            }
          ]
        }
      };

      const m = fanOutMap(doc);
      return {
        policy: fanOut(doc, 'policy'),
        region: fanOut(doc, 'region'),
        db: fanOut(doc, 'db'),
        dbHost: fanOut(doc, 'db.host'),
        secretKey: fanOut(doc, 'secretKey'),
        annoParam: fanOut(doc, 'annoParam'),
        envParam: fanOut(doc, 'envParam'),
        map: m
      };
    });

    expect(counts.policy).toBe(2); // 1 in fields, 1 in annotations
    expect(counts.region).toBe(1);
    expect(counts.db).toBe(1);
    expect(counts.dbHost).toBe(1);
    expect(counts.secretKey).toBe(1);
    expect(counts.annoParam).toBe(1);
    expect(counts.envParam).toBe(1);
  });

  test('parameter referenced in resource raw field reflects fan-out count >= 1 and prompts unwire on delete instead of 409 abort', async ({ page, request }) => {
    // 1. Put blueprint where a parameter policy is referenced in a resource raw field work-queue.fields.policy = { raw: "{{ $spec.policy }}" }
    const doc = JSON.parse(JSON.stringify(pristine));
    doc.spec.xrd.parameters.policy = {
      type: 'string',
      default: 'standard'
    };
    doc.spec.resources[0].fields.policy = {
      raw: '{{ $spec.policy }}'
    };

    const putRes = await request.put(`${ENGINE}/api/blueprint`, { data: doc });
    expect(putRes.ok()).toBeTruthy();

    await page.goto('/');
    await canvasSettled(page);

    // 2. Select XRD card to open inspector
    const xrdCard = page.locator('.node[data-id="xrd"]');
    await xrdCard.locator('.node-h').click();

    // 3. Observe fan-out badge for policy reflects x1
    const paramRow = page.locator('#region-inspector .fld:has(input[data-pn="policy"])');
    await expect(paramRow).toBeVisible();
    const fanBadge = paramRow.locator('.fan');
    await expect(fanBadge).toHaveText('×1');

    // Also verify Palette Shared rail reflects 1 bound
    await page.click('#rtabs button[data-r="shared"]');
    const paramCard = page.locator('.card:has([data-param-del="policy"])');
    await expect(paramCard).toBeVisible();
    await expect(paramCard.locator('.bind')).toHaveText('1 bound');

    // Switch back to XRD inspector
    await xrdCard.locator('.node-h').click();

    // 4. Click delete button for policy; verify prompt appears
    let dialogMessage = '';
    page.on('dialog', async (d) => {
      dialogMessage = d.message();
      await d.accept();
    });

    await paramRow.locator('button[data-pd="policy"]').click();
    await page.waitForTimeout(500);

    expect(dialogMessage).toBe('Parameter "policy" is wired into 1 field. Delete it and unwire all referencing fields?');

    // 5. Parameter deleted and work-queue.fields.policy removed, no error banner
    await expect(page.locator('#region-inspector .warnbar, #region-palette .warnbar, #region-canvas .warnbar')).toHaveCount(0);

    await expect.poll(async () => {
      const res = await request.get(`${ENGINE}/api/blueprint`);
      const updated = await res.json();
      const p = updated.spec.xrd.parameters || {};
      const wq = updated.spec.resources.find(r => r.name === 'work-queue');
      return {
        hasParam: 'policy' in p,
        hasRawField: wq && wq.fields && 'policy' in wq.fields
      };
    }).toEqual({
      hasParam: false,
      hasRawField: false
    });
  });
});
