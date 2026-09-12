const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, canvasSettled } = require('./helpers');

test.describe('CF-404 — deleteEnvKeyFromDoc removes orphan function-environment-configs from pipeline', () => {
  guardPageErrors();

  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('deleting last environment key removes function-environment-configs step and cleans empty pipeline', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    const result = await page.evaluate(async () => {
      const { deleteEnvKeyFromDoc } = await import('/js/utils.js');

      // Test case 1: Doc with environment key and only function-environment-configs in pipeline
      const doc1 = {
        apiVersion: 'factory.crossplane.io/v1alpha1',
        kind: 'Blueprint',
        spec: {
          environment: {
            clusterName: { type: 'string' }
          },
          environmentConfigs: [{ type: 'Reference', ref: { name: 'default' } }],
          pipeline: [
            {
              name: 'environment-configs',
              functionRef: 'function-environment-configs',
              package: 'xpkg.crossplane.io/crossplane-contrib/function-environment-configs:v0.4.0',
              position: 'before'
            }
          ]
        }
      };

      deleteEnvKeyFromDoc(doc1, 'clusterName');

      // Test case 2: Doc with multiple keys, deleting one retains environment and pipeline
      const doc2 = {
        apiVersion: 'factory.crossplane.io/v1alpha1',
        kind: 'Blueprint',
        spec: {
          environment: {
            clusterName: { type: 'string' },
            region: { type: 'string' }
          },
          pipeline: [
            {
              name: 'environment-configs',
              functionRef: 'function-environment-configs'
            }
          ]
        }
      };

      deleteEnvKeyFromDoc(doc2, 'clusterName');

      // Test case 3: Doc with multiple pipeline steps; only environment-configs step removed
      const doc3 = {
        apiVersion: 'factory.crossplane.io/v1alpha1',
        kind: 'Blueprint',
        spec: {
          environment: {
            clusterName: { type: 'string' }
          },
          pipeline: [
            {
              name: 'environment-configs',
              functionRef: 'function-environment-configs'
            },
            {
              name: 'auto-ready',
              functionRef: 'function-auto-ready'
            }
          ]
        }
      };

      deleteEnvKeyFromDoc(doc3, 'clusterName');

      return {
        doc1HasEnv: !!doc1.spec.environment,
        doc1HasConfigs: !!doc1.spec.environmentConfigs,
        doc1HasPipeline: !!doc1.spec.pipeline,
        doc2HasEnv: !!doc2.spec.environment,
        doc2EnvKeys: Object.keys(doc2.spec.environment || {}),
        doc2PipelineLen: (doc2.spec.pipeline || []).length,
        doc3HasEnv: !!doc3.spec.environment,
        doc3PipelineSteps: (doc3.spec.pipeline || []).map((s) => s.name)
      };
    });

    expect(result.doc1HasEnv).toBe(false);
    expect(result.doc1HasConfigs).toBe(false);
    expect(result.doc1HasPipeline).toBe(false);

    expect(result.doc2HasEnv).toBe(true);
    expect(result.doc2EnvKeys).toEqual(['region']);
    expect(result.doc2PipelineLen).toBe(1);

    expect(result.doc3HasEnv).toBe(false);
    expect(result.doc3PipelineSteps).toEqual(['auto-ready']);
  });
});
