// tests/cf138-namespaced-kind-explanation.spec.js
// CF-138 (issue #23) — Two IAM Role kinds are offered with no explanation of the .m. (namespaced) vs cluster-scoped difference
// Contract:
// 1. Identify namespaced groups/kinds (e.g. containing .m. in group/apiVersion or k.namespaced === true or k.scope === "Namespaced").
// 2. Clearly label them with a pill/badge: e.g. "namespaced" or "namespaced (v2)".
// 3. For a Namespaced XRD, mark or rank the namespaced group/kind so that it is clearly designated as matching the Namespaced XRD
//    (or ranked above cluster-scoped counterparts). Conversely, for a Cluster-scoped XRD, cluster-scoped kinds match.
// 4. When searching (e.g. 'role' or 'queue'), both kinds/groups are shown with clear badges so the user understands .m. is the
//    namespaced family and which one matches the composition's XRD scope.

const { test, expect } = require('@playwright/test');
const { resetDoc, guardPageErrors } = require('./helpers');

guardPageErrors();

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

test('for Namespaced XRD, namespaced .m. group and kinds are clearly labeled and ranked above cluster-scoped', async ({ page }) => {
  await page.goto('/');
  await page.click('#rtabs button[data-r="kinds"]');

  // Search for 'queue' to see both namespaced (.m.) and cluster-scoped groups
  const search = page.locator('#psearch');
  await search.fill('queue');

  // Namespaced group header must have a pill clearly labeling namespaced / namespaced (v2) and matching XRD
  const nsGroup = page.locator('#lrail .grp', { hasText: 'sqs.aws.m.upbound.io' });
  await expect(nsGroup).toBeVisible();
  const nsPill = nsGroup.locator('.pill');
  await expect(nsPill).toBeVisible();
  await expect(nsPill).toContainText('namespaced');
  await expect(nsPill).toContainText('matches XRD');

  // Cluster-scoped group header must have a pill labeling cluster-scoped
  const clusterGroup = page.locator('#lrail .grp', { hasText: 'sqs.aws.upbound.io' }).filter({ hasNotText: '.m.' });
  await expect(clusterGroup).toBeVisible();
  const clusterPill = clusterGroup.locator('.pill');
  await expect(clusterPill).toBeVisible();
  await expect(clusterPill).toContainText('cluster-scoped');

  // Matching namespaced group must be ranked above cluster-scoped group
  const allGroups = page.locator('#lrail .grp');
  const groupTexts = await allGroups.allTextContents();
  const nsIndex = groupTexts.findIndex(t => t.includes('sqs.aws.m.upbound.io'));
  const clusterIndex = groupTexts.findIndex(t => t.includes('sqs.aws.upbound.io') && !t.includes('.m.'));
  expect(nsIndex).toBeGreaterThanOrEqual(0);
  expect(clusterIndex).toBeGreaterThanOrEqual(0);
  expect(nsIndex).toBeLessThan(clusterIndex);

  // Both kinds under search results must have clear scope badges
  const nsKind = page.locator('#lrail .kind[data-av*=".m."][data-kind="Queue"]').first();
  await expect(nsKind).toBeVisible();
  const nsKindPill = nsKind.locator('.pill');
  await expect(nsKindPill).toBeVisible();
  await expect(nsKindPill).toContainText('namespaced');

  const clusterKind = page.locator('#lrail .kind[data-av="sqs.aws.upbound.io/v1beta1"][data-kind="Queue"]').first();
  await expect(clusterKind).toBeVisible();
  const clusterKindPill = clusterKind.locator('.pill');
  await expect(clusterKindPill).toBeVisible();
  await expect(clusterKindPill).toContainText('cluster-scoped');
});

test('for Cluster-scoped XRD, cluster-scoped group is designated as matching XRD and ranked above namespaced', async ({ page }) => {
  await page.goto('/');

  // Set XRD scope to Cluster in client-side state and re-render
  await page.evaluate(() => {
    window.store.state.doc.spec.xrd.scope = 'Cluster';
    window.store.emit('doc', window.store.state.doc);
  });

  await page.click('#rtabs button[data-r="kinds"]');

  const search = page.locator('#psearch');
  await search.fill('queue');

  // Cluster-scoped group header must now indicate it matches the Cluster XRD
  const clusterGroup = page.locator('#lrail .grp', { hasText: 'sqs.aws.upbound.io' }).filter({ hasNotText: '.m.' });
  await expect(clusterGroup).toBeVisible();
  const clusterPill = clusterGroup.locator('.pill');
  await expect(clusterPill).toBeVisible();
  await expect(clusterPill).toContainText('cluster-scoped');
  await expect(clusterPill).toContainText('matches XRD');

  // Namespaced group header must still show namespaced label but NOT match XRD
  const nsGroup = page.locator('#lrail .grp', { hasText: 'sqs.aws.m.upbound.io' });
  await expect(nsGroup).toBeVisible();
  const nsPill = nsGroup.locator('.pill');
  await expect(nsPill).toBeVisible();
  await expect(nsPill).toContainText('namespaced');
  await expect(nsPill).not.toContainText('matches XRD');

  // Cluster-scoped group should now be ranked above namespaced group
  const allGroups = page.locator('#lrail .grp');
  const groupTexts = await allGroups.allTextContents();
  const nsIndex = groupTexts.findIndex(t => t.includes('sqs.aws.m.upbound.io'));
  const clusterIndex = groupTexts.findIndex(t => t.includes('sqs.aws.upbound.io') && !t.includes('.m.'));
  expect(clusterIndex).toBeLessThan(nsIndex);
});
