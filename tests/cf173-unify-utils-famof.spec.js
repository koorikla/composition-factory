// CF-173 — web-proto duplicates utility functions and palette.js famOf diverges from canvas.js colors
// Issue #58: Share mapResourceCoordinates, slug, and uniqueResourceName from a common helper
// module (web-proto/js/utils.js), and unify famOf so palette and canvas use consistent provider family
// color classifications (GCP -> "gcp", Azure -> "azure", Helm -> "helm", AWS -> "aws", K8s -> "k8s").

const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors, canvasSettled } = require('./helpers');

guardPageErrors();

test.describe('CF-173: Unified web-proto utils and famOf provider classification', () => {
  test.beforeEach(async ({ request }) => {
    await resetDoc(request);
  });

  test('shared utils.js exports famOf, slug, uniqueResourceName, mapResourceCoordinates, and COLORS', async ({ page }) => {
    await page.goto('/');
    await canvasSettled(page);

    // Import from the shared utils module
    const utils = await page.evaluate(async () => {
      const mod = await import('/js/utils.js');
      return {
        hasFamOf: typeof mod.famOf === 'function',
        hasSlug: typeof mod.slug === 'function',
        hasUniqueResourceName: typeof mod.uniqueResourceName === 'function',
        hasMapResourceCoordinates: typeof mod.mapResourceCoordinates === 'function',
        hasColors: typeof mod.COLORS === 'object' && mod.COLORS !== null,
        gcpFam: mod.famOf({
          group: 'compute.gcp.upbound.io',
          provider: 'xpkg.upbound.io/upbound/provider-gcp-compute',
        }),
        azureFam: mod.famOf({
          group: 'compute.azure.upbound.io',
          provider: 'xpkg.upbound.io/upbound/provider-azure-compute',
        }),
        helmFam: mod.famOf({
          group: 'helm.crossplane.io',
          provider: 'xpkg.upbound.io/crossplane-contrib/provider-helm',
        }),
        awsFam: mod.famOf({
          group: 's3.aws.upbound.io',
          provider: 'xpkg.upbound.io/upbound/provider-aws-s3',
        }),
        k8sFam: mod.famOf({
          group: 'apps',
          provider: 'xpkg.upbound.io/crossplane-contrib/provider-kubernetes',
        }),
        clusterFam: mod.famOf({
          provider: 'cluster',
        }),
        slugResult: mod.slug('BucketPolicy'),
        uniqueName1: mod.uniqueResourceName({ spec: { resources: [] } }, 'Bucket'),
        uniqueName2: mod.uniqueResourceName({ spec: { resources: [{ name: 'bucket' }] } }, 'Bucket'),
        coordResult: mod.mapResourceCoordinates('spec.resources[0].field', {
          spec: { resources: [{ name: 'my-res', kind: 'Bucket' }] },
        }),
        colorsGcp: mod.COLORS && mod.COLORS.gcp,
        colorsAzure: mod.COLORS && mod.COLORS.azure,
        colorsHelm: mod.COLORS && mod.COLORS.helm,
        colorsAws: mod.COLORS && mod.COLORS.aws,
        colorsCluster: mod.COLORS && mod.COLORS.cluster,
      };
    });

    expect(utils.hasFamOf).toBe(true);
    expect(utils.hasSlug).toBe(true);
    expect(utils.hasUniqueResourceName).toBe(true);
    expect(utils.hasMapResourceCoordinates).toBe(true);
    expect(utils.hasColors).toBe(true);

    // famOf classifications
    expect(utils.gcpFam).toBe('gcp');
    expect(utils.azureFam).toBe('azure');
    expect(utils.helmFam).toBe('helm');
    expect(utils.awsFam).toBe('aws');
    expect(utils.k8sFam).toBe('k8s');
    expect(utils.clusterFam).toBe('cluster');

    // utility function outputs
    expect(utils.slugResult).toBe('bucket-policy');
    expect(utils.uniqueName1).toBe('bucket');
    expect(utils.uniqueName2).toBe('bucket-2');
    expect(utils.coordResult).toBe("resource 'my-res' (spec.resources[0]).field");

    // COLORS dictionary values
    expect(utils.colorsGcp).toBe('#ea4335');
    expect(utils.colorsAzure).toBe('#0078d4');
    expect(utils.colorsHelm).toBe('#0f1689');
    expect(utils.colorsCluster).toBe('#06b6d4');
  });

  test('palette classifies GCP and Azure kinds as gcp and azure rather than aws', async ({ page }) => {
    // Intercept /api/kinds to provide multi-cloud kinds
    await page.route('**/api/kinds*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          kinds: [
            {
              kind: 'GCPComputeInstance',
              apiVersion: 'compute.gcp.upbound.io/v1beta1',
              group: 'compute.gcp.upbound.io',
              provider: 'xpkg.upbound.io/upbound/provider-gcp-compute',
              namespaced: true,
              scope: 'Namespaced',
              fields: 10,
              required: 1,
            },
            {
              kind: 'AzureVirtualNetwork',
              apiVersion: 'network.azure.upbound.io/v1beta1',
              group: 'network.azure.upbound.io',
              provider: 'xpkg.upbound.io/upbound/provider-azure-network',
              namespaced: true,
              scope: 'Namespaced',
              fields: 8,
              required: 1,
            },
            {
              kind: 'AWSQueue',
              apiVersion: 'sqs.aws.upbound.io/v1beta1',
              group: 'sqs.aws.upbound.io',
              provider: 'xpkg.upbound.io/upbound/provider-aws-sqs',
              namespaced: true,
              scope: 'Namespaced',
              fields: 12,
              required: 1,
            },
          ],
        }),
      });
    });

    await page.goto('/');
    await canvasSettled(page);

    const gcpRow = page.locator('#lrail .kind[data-kind="GCPComputeInstance"]');
    await expect(gcpRow).toBeVisible({ timeout: 5000 });
    // Verify GCP kind receives data-fam="gcp" rather than "aws"
    await expect(gcpRow).toHaveAttribute('data-fam', 'gcp');

    const azureRow = page.locator('#lrail .kind[data-kind="AzureVirtualNetwork"]');
    await expect(azureRow).toBeVisible({ timeout: 5000 });
    // Verify Azure kind receives data-fam="azure" rather than "aws"
    await expect(azureRow).toHaveAttribute('data-fam', 'azure');

    const awsRow = page.locator('#lrail .kind[data-kind="AWSQueue"]');
    await expect(awsRow).toBeVisible({ timeout: 5000 });
    await expect(awsRow).toHaveAttribute('data-fam', 'aws');

    // Verify GCP swatch background is GCP red (#ea4335 / rgb(234, 67, 53))
    const gcpSwatch = gcpRow.locator('.sw');
    const gcpSwatchBg = await gcpSwatch.evaluate((el) => window.getComputedStyle(el).backgroundColor);
    expect(gcpSwatchBg).toBe('rgb(234, 67, 53)');

    // Verify Azure swatch background is Azure blue (#0078d4 / rgb(0, 120, 212))
    const azureSwatch = azureRow.locator('.sw');
    const azureSwatchBg = await azureSwatch.evaluate((el) => window.getComputedStyle(el).backgroundColor);
    expect(azureSwatchBg).toBe('rgb(0, 120, 212)');
  });
});
