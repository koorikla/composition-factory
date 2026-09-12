const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, canvasSettled } = require('./helpers');

test.describe('CF-402 — cleanMemberRefs cleans templates, conventions, and template-referencing fields', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('deleting object parameter member property removes referencing templates and conventions', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const result = await page.evaluate(async () => {
      const { cleanMemberRefs } = await import('/js/regions/inspector/xrd.js');

      const draft = {
        apiVersion: 'factory.crossplane.io/v1alpha1',
        kind: 'Blueprint',
        metadata: {
          name: 'test-member-template-clean'
        },
        spec: {
          xrd: {
            parameters: {
              cfg: {
                type: 'object',
                properties: {
                  region: { type: 'string' },
                  zone: { type: 'string' }
                }
              }
            }
          },
          templates: {
            queueEndpoint: 'https://sqs.{{ .spec.cfg.region }}.amazonaws.com/12345/queue',
            zoneEndpoint: 'https://sqs.{{ .spec.cfg.zone }}.amazonaws.com/12345/queue'
          },
          conventions: [
            { field: 'regionEndpoint', template: 'queueEndpoint' },
            { field: 'zoneEndpoint', template: 'zoneEndpoint' }
          ],
          resources: [
            {
              name: 'q',
              type: 'queue.sqs.aws.upbound.io/v1beta1',
              fields: {
                endpoint: { template: 'queueEndpoint' },
                zoneField: { template: 'zoneEndpoint' },
                directRef: { from: 'params.cfg.region' }
              },
              annotations: {
                'anno/region': { template: 'queueEndpoint' },
                'anno/zone': { template: 'zoneEndpoint' }
              },
              envelope: {
                'env/region': { template: 'queueEndpoint' },
                'env/zone': { template: 'zoneEndpoint' }
              }
            }
          ]
        }
      };

      cleanMemberRefs(draft, 'cfg', 'region');

      return {
        hasRegionProp: !!(draft.spec.xrd.parameters.cfg.properties && draft.spec.xrd.parameters.cfg.properties.region),
        hasZoneProp: !!(draft.spec.xrd.parameters.cfg.properties && draft.spec.xrd.parameters.cfg.properties.zone),
        templates: draft.spec.templates,
        conventions: draft.spec.conventions,
        fields: draft.spec.resources[0].fields,
        annotations: draft.spec.resources[0].annotations,
        envelope: draft.spec.resources[0].envelope
      };
    });

    expect(result.hasRegionProp).toBe(false);
    expect(result.hasZoneProp).toBe(true);

    expect(result.templates).toEqual({
      zoneEndpoint: 'https://sqs.{{ .spec.cfg.zone }}.amazonaws.com/12345/queue'
    });

    expect(result.conventions).toEqual([
      { field: 'zoneEndpoint', template: 'zoneEndpoint' }
    ]);

    expect(result.fields).toEqual({
      zoneField: { template: 'zoneEndpoint' }
    });

    expect(result.annotations).toEqual({
      'anno/zone': { template: 'zoneEndpoint' }
    });

    expect(result.envelope).toEqual({
      'env/zone': { template: 'zoneEndpoint' }
    });
  });
});
