const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, canvasSettled } = require('./helpers');

test.describe('CF-400 — rename and delete environment keys updates spec.templates and validates camelCase names', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('renameEnvKeyInDoc rewrites references in spec.templates', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const result = await page.evaluate(async () => {
      const { renameEnvKeyInDoc } = await import('/js/utils.js');

      const draft = {
        apiVersion: 'factory.crossplane.io/v1alpha1',
        kind: 'Blueprint',
        metadata: { name: 'test-env-rename' },
        spec: {
          environment: {
            vpcId: { type: 'string' },
            other: { type: 'string' }
          },
          templates: {
            t1: 'https://{{ $env.vpcId }}.internal',
            t2: 'region-{{ .env.vpcId }}-sub',
            t3: '{{ index $env "vpcId" }}/cluster',
            t4: '{{ index .env "vpcId" }}/cluster',
            tOther: '{{ $env.other }}'
          }
        }
      };

      renameEnvKeyInDoc(draft, 'vpcId', 'networkId');
      return draft;
    });

    expect(result.spec.environment.networkId).toBeDefined();
    expect(result.spec.environment.vpcId).toBeUndefined();
    expect(result.spec.templates.t1).toBe('https://{{ $env.networkId }}.internal');
    expect(result.spec.templates.t2).toBe('region-{{ .env.networkId }}-sub');
    expect(result.spec.templates.t3).toBe('{{ index $env "networkId" }}/cluster');
    expect(result.spec.templates.t4).toBe('{{ index .env "networkId" }}/cluster');
    expect(result.spec.templates.tOther).toBe('{{ $env.other }}');
  });

  test('cleanEnvRefs removes referencing templates, conventions, and template fields', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const result = await page.evaluate(async () => {
      const { cleanEnvRefs } = await import('/js/utils.js');

      const draft = {
        apiVersion: 'factory.crossplane.io/v1alpha1',
        kind: 'Blueprint',
        metadata: { name: 'test-env-clean' },
        spec: {
          environment: {
            vpcId: { type: 'string' },
            other: { type: 'string' }
          },
          templates: {
            tVpc: 'https://{{ $env.vpcId }}.internal',
            tIndex: '{{ index $env "vpcId" }}/cluster',
            tOther: '{{ $env.other }}'
          },
          conventions: [
            { field: 'vpcTmpl', template: 'tVpc' },
            { field: 'otherTmpl', template: 'tOther' }
          ],
          resources: [
            {
              name: 'r1',
              type: 'vpc.aws.upbound.io/v1beta1',
              fields: {
                directEnv: { from: 'env.vpcId' },
                tmplField: { template: 'tVpc' },
                keepField: { template: 'tOther' }
              },
              annotations: {
                'anno/vpc': { template: 'tIndex' },
                'anno/keep': { template: 'tOther' }
              },
              envelope: {
                'env/vpc': { template: 'tVpc' },
                'env/keep': { template: 'tOther' }
              }
            }
          ]
        }
      };

      cleanEnvRefs(draft, 'vpcId');
      return draft;
    });

    // Templates referencing vpcId deleted; tOther kept
    expect(result.spec.templates.tVpc).toBeUndefined();
    expect(result.spec.templates.tIndex).toBeUndefined();
    expect(result.spec.templates.tOther).toBe('{{ $env.other }}');

    // Convention referencing tVpc cleaned; tOther convention kept
    expect(result.spec.conventions).toEqual([
      { field: 'otherTmpl', template: 'tOther' }
    ]);

    // Resource fields referencing vpcId directly or via deleted templates removed
    const r1 = result.spec.resources[0];
    expect(r1.fields.directEnv).toBeUndefined();
    expect(r1.fields.tmplField).toBeUndefined();
    expect(r1.fields.keepField).toEqual({ template: 'tOther' });

    expect(r1.annotations['anno/vpc']).toBeUndefined();
    expect(r1.annotations['anno/keep']).toEqual({ template: 'tOther' });

    expect(r1.envelope['env/vpc']).toBeUndefined();
    expect(r1.envelope['env/keep']).toEqual({ template: 'tOther' });
  });

  test('renameEnvKey rejects hyphens, underscores, and YAML keywords', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const testResults = await page.evaluate(async () => {
      const { store } = await import('/js/store.js');
      const { state } = await import('/js/regions/inspector/state.js');

      // Seed store with an environment key
      await store.replaceDoc(d => {
        d.spec = d.spec || {};
        d.spec.environment = {
          vpcId: { type: 'string' }
        };
      });

      const errors = [];
      store.subscribe('error', err => {
        errors.push(err);
      });

      const invalidNames = ['my-key', 'my_key', 'true', 'false', 'yes', 'no', '123bad'];
      const rejected = [];

      for (const bad of invalidNames) {
        const dummyInput = { value: bad };
        state.renameEnvKey('vpcId', bad, dummyInput);
        if (errors.length > 0 && errors[errors.length - 1].status === 400) {
          rejected.push(bad);
        }
      }

      // Also verify a valid camelCase name does not emit error
      const validName = 'networkId';
      const validDummy = { value: validName };
      const errCountBefore = errors.length;
      await state.renameEnvKey('vpcId', validName, validDummy);
      const validAccepted = errors.length === errCountBefore;

      return {
        rejected,
        expectedCount: invalidNames.length,
        validAccepted
      };
    });

    expect(testResults.rejected.length).toBe(testResults.expectedCount);
    expect(testResults.validAccepted).toBe(true);
  });
});
