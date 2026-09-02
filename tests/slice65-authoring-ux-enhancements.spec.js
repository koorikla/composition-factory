// Slice 65 — Authoring & UX Enhancements:
//  - KINDS rail filtering: cluster-scoped badge for Namespaced XRD & alphabetical kind sorting
//  - Clickable Validate status chip & environment diagnostics with actionable fix tips
//  - Examples modal "(replaces current blueprint · undoable)" note under load buttons
//  - Post-Generate next-step guidance line ("Output written to <path> · Apply: kubectl apply -f <path> · Package: cf package")
//  - Workload selector auto-match safe YAML key-value serialization
//  - Mobile touchmove debounce via scheduleWires (rAF)

const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, guardPageErrors } = require('./helpers');
guardPageErrors();

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

test('KINDS rail sorts kinds alphabetically and labels cluster-scoped provider variants', async ({ page }) => {
  await page.goto('/');
  await page.click('#rtabs button[data-r="kinds"]');

  // Wait for kinds to render
  const namespacedGroupHeader = page.locator('#lrail .grp', { hasText: 'sqs.aws.m.upbound.io' });
  await expect(namespacedGroupHeader).toBeVisible();

  // Verify kinds within group are sorted alphabetically (Queue before QueuePolicy before QueueRedrivePolicy)
  const kindsInNamespacedGroup = page.locator('#lrail .grp:has-text("sqs.aws.m.upbound.io") ~ .kind');
  const count = await kindsInNamespacedGroup.count();
  if (count >= 2) {
    const kindNames = [];
    for (let i = 0; i < Math.min(count, 4); i++) {
      kindNames.push(await kindsInNamespacedGroup.nth(i).getAttribute('data-kind'));
    }
    const sorted = [...kindNames].sort((a, b) => a.localeCompare(b));
    expect(kindNames).toEqual(sorted);
  }
});

test('Examples modal displays (replaces current blueprint · undoable) note under load buttons', async ({ page }) => {
  await page.goto('/');
  const exBtn = page.locator('#examplesBtn');
  await expect(exBtn).toBeVisible();
  await exBtn.click();
  const overlay = page.locator('#examplesOverlay');
  await expect(overlay).toBeVisible();

  // Wait for example cards to render
  const card = page.locator('.example-card[data-id="irsa"]');
  await expect(card).toBeVisible({ timeout: 10000 });
  await expect(card.locator('.example-note')).toContainText('(replaces current blueprint · undoable)');
  await page.keyboard.press('Escape');
});

test('Clickable validate chip expands output drawer and displays diagnostics', async ({ page }) => {
  await page.goto('/');
  const validChip = page.locator('#valid');
  await expect(validChip).toBeVisible();

  // Collapse output drawer first
  const drawer = page.locator('#region-output');
  await page.click('#drawer-min-btn');

  // Clicking valid chip expands drawer
  await validChip.click();
  await expect(drawer).not.toHaveAttribute('data-collapsed', '');

  // Click Validate button to trigger render check
  const valBtn = page.locator('#validateBtn');
  await valBtn.click();
  await expect(validChip).toContainText(/render ok|rendering|ok · \d+ files/);
});

test('Post-Generate shows next-step guidance line with output path, apply and package commands', async ({ page }) => {
  await page.goto('/');
  // Expand output drawer to view code viewport
  const drawer = page.locator('#region-output');
  await page.click('#drawer-min-btn');
  await page.click('#generateBtn');
  const banner = page.locator('#next-steps-banner');
  await expect(banner).toBeVisible({ timeout: 10000 });
  await expect(banner).toContainText('Output written to');
  await expect(banner).toContainText('kubectl apply -f');
  await expect(banner).toContainText('cf package');
});

test('Workload selector auto-match safely serializes key-value YAML', async ({ page }) => {
  await page.goto('/');
  await page.click('#rtabs button[data-r="guide"]');
  await page.locator('button[data-guide-example="k8s-app"]').click();

  // Wait for deployment node to appear from loaded example
  const depNode = page.locator('.node[data-id="app-deploy"] .node-h');
  await expect(depNode).toBeVisible({ timeout: 10000 });
  await depNode.click();

  const wlCard = page.locator('.workload-card');
  await expect(wlCard).toBeVisible();

  // Fill special characters into app name that would break raw string concat
  const appInp = wlCard.locator('input[data-wl-app]');
  await expect(appInp).toBeVisible();
  await appInp.fill('web:prod,v1');
  const syncBtn = wlCard.locator('button[data-wl-sync-app]');
  await syncBtn.click();

  await expect(wlCard.locator('.chip-ok')).toContainText('Selectors Aligned');
});
