# Pre-filled Fields, Essentials Form and Manifest Editor — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** A dropped native kind lands with a valid, good-practice starter; every kind opens in the inspector with a typed essentials form (expose-as-parameter, env repeater) on top and a schema-validated manifest-style YAML editor at the bottom, with a field search that works in both views.

**Architecture:** One frontend module (`web-proto/js/profiles.js`) owns per-kind starters and essentials so drop, the Scaffold button and the inspector cannot disagree. A new Go package (`internal/manifest`) converts between the flat `fields` map and nested manifest YAML using the kind's schema tree as the authority for dot / `[i]` / `[key]` grammar, served by two routes and two MCP tools. The old Required / Set / All list survives behind a Manifest / Fields toggle so 28 existing Playwright specs keep running.

**Tech Stack:** Go 1.22+ (`net/http` ServeMux, `gopkg.in/yaml.v3` Node API), vanilla ES-module JavaScript (no build step), Playwright, the design in `docs/superpowers/specs/2026-09-12-prefilled-fields-design.md`.

**Working rules (from AGENTS.md and memory):** work only in `/Users/kaurkallas/compositionfactory/.worktrees/prefill` (branch `prefill-fields`); never `git add -A`; no AI attribution trailers in commits; run `make test-e2e` from the worktree (it derives its own port); never touch port 8080; the e2e job's canvas drag tests are flaky in CI, rerun once before treating as a regression.

---

## Slice 0: issues

### Task 0: File the three backlog issues

The backlog is GitHub Issues, one per landable slice, `CF-NNN — <sentence>`. Next free id is 466 (max over issue titles 465, docs/tasks 404, archive 129).

**Step 1: Create the issues**

```bash
cd /Users/kaurkallas/compositionfactory/.worktrees/prefill
gh issue create --title "CF-466 — Dropping a Deployment scaffolds no container and writes labels as raw JSON; starters should be valid, good-practice minimums in map-entry grammar" --label "severity:P2,scale:ux,verified" --body-file docs/tasks/CF-466-starters.md
gh issue create --title "CF-467 — Inspector needs a per-kind essentials form with expose-as-parameter and an env-variable repeater; a second env entry cannot be added from the GUI" --label "severity:P2,scale:ux,verified" --body-file docs/tasks/CF-467-essentials.md
gh issue create --title "CF-468 — Inspector bottom becomes a schema-validated manifest editor with field search; 848-row list demoted behind a Manifest/Fields toggle" --label "severity:P2,scale:ux,verified" --body-file docs/tasks/CF-468-manifest-editor.md
```

Write each `docs/tasks/CF-NNN-*.md` brief first (repro from the design doc's Problem section, acceptance test = the spec file the slice adds, contract = the matching design section). Commit the briefs:

```bash
git add docs/tasks/CF-466-starters.md docs/tasks/CF-467-essentials.md docs/tasks/CF-468-manifest-editor.md
git commit -m "docs: briefs for CF-466, CF-467, CF-468 (starters, essentials, manifest editor)"
```

---

## Slice 1: starters and the raw-to-bracket migration (CF-466)

### Task 1: Failing spec for the Deployment starter

**Files:**
- Create: `tests/cf466-starter-deployment.spec.js`

**Step 1: Write the failing test**

```js
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
    await card.locator('input[data-wl-app]').fill('web:prod,v1');
    await card.locator('button[data-wl-sync-app]').click();
    await expect(card.locator('.chip-ok')).toContainText('Selectors Aligned');
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      return r && r.fields['spec.selector.matchLabels[app]'] && r.fields['spec.selector.matchLabels[app]'].value;
    }).toBe('web:prod,v1');
  });
});
```

Note: `generate` output entries carry `path` and `body` (verified against the live engine on 2026-09-12: keys `existingFiles, outDir, outputs, written`; each output has `path` and `body`). If `body` is absent when you run this, check `internal/api/generate.go`'s `generateOutput` struct and use its json tag.

**Step 2: Run it to verify it fails**

```bash
npx playwright test tests/cf466-starter-deployment.spec.js
```

Expected: FAIL on the first test (`image` undefined, `oldRawSelector` true).

### Task 2: `profiles.js` with starters, wired into `scaffoldResourceFields`

**Files:**
- Create: `web-proto/js/profiles.js`
- Modify: `web-proto/js/regions/canvas.js:1609-1730` (`scaffoldResourceFields`)

**Step 1: Create `web-proto/js/profiles.js`**

```js
/**
 * Kind profiles: what a drop writes onto the canvas (starter) and what the
 * inspector shows first (essentials). Drop, the Scaffold button and the
 * essentials form all read this module, so the three cannot disagree.
 *
 * Starter values follow the Kubernetes references consulted in the design
 * doc: pinned image tag (never :latest), containerPort matching the image,
 * cpu+memory requests, a memory limit only, replicas >= 2.
 */

export const STARTER_IMAGE = "nginx:1.27";
export const STARTER_PORT = "80";

const WORKLOADS = ["Deployment", "StatefulSet", "DaemonSet"];
const BATCH = ["Job", "CronJob"];

export function isWorkloadKind(kind) {
  return WORKLOADS.indexOf(kind) !== -1 || BATCH.indexOf(kind) !== -1;
}

/** Path prefix of the first container for a kind. */
export function containerPrefix(kind) {
  return kind === "CronJob"
    ? "spec.jobTemplate.spec.template.spec.containers[0]"
    : "spec.template.spec.containers[0]";
}

/** Path prefix of the pod template for a kind. */
export function podTemplatePrefix(kind) {
  return kind === "CronJob" ? "spec.jobTemplate.spec.template" : "spec.template";
}

function containerStarter(kind, name) {
  const c = containerPrefix(kind);
  const f = {};
  f[c + ".name"] = { value: name };
  f[c + ".image"] = { value: STARTER_IMAGE };
  f[c + ".ports[0].containerPort"] = { value: STARTER_PORT };
  f[c + ".resources.requests[cpu]"] = { value: "100m" };
  f[c + ".resources.requests[memory]"] = { value: "128Mi" };
  f[c + ".resources.limits[memory]"] = { value: "256Mi" };
  return f;
}

function starterFor(kind, name) {
  const f = {};
  if (WORKLOADS.indexOf(kind) !== -1) {
    if (kind !== "DaemonSet") f["spec.replicas"] = { value: "2" };
    f["spec.selector.matchLabels[app]"] = { value: name };
    f["spec.template.metadata.labels[app]"] = { value: name };
    if (kind === "StatefulSet") f["spec.serviceName"] = { value: name };
    Object.assign(f, containerStarter(kind, name));
    return f;
  }
  if (kind === "Job") {
    f["spec.template.metadata.labels[app]"] = { value: name };
    Object.assign(f, containerStarter(kind, name));
    delete f[containerPrefix(kind) + ".ports[0].containerPort"];
    f["spec.template.spec.restartPolicy"] = { value: "Never" };
    return f;
  }
  if (kind === "CronJob") {
    f["spec.schedule"] = { value: "*/5 * * * *" };
    Object.assign(f, containerStarter(kind, name));
    delete f[containerPrefix(kind) + ".ports[0].containerPort"];
    f["spec.jobTemplate.spec.template.spec.restartPolicy"] = { value: "Never" };
    return f;
  }
  if (kind === "Service") {
    f["spec.selector[app]"] = { value: name };
    f["spec.ports[0].port"] = { value: "80" };
    f["spec.ports[0].targetPort"] = { value: "80" };
    f["spec.type"] = { value: "ClusterIP" };
    return f;
  }
  return null;
}

/**
 * Essentials rows. kind: "app-label" (writes selector + template labels),
 * "field" (one path), "env" (repeater over <prefix>[i].name/.value).
 * `param` is the XRD parameter name "expose" creates.
 */
function essentialsFor(kind) {
  const c = containerPrefix(kind);
  if (WORKLOADS.indexOf(kind) !== -1 || BATCH.indexOf(kind) !== -1) {
    const rows = [];
    if (WORKLOADS.indexOf(kind) !== -1) rows.push({ kind: "app-label", label: "App label" });
    if (kind === "Deployment" || kind === "StatefulSet") {
      rows.push({ kind: "field", path: "spec.replicas", label: "Replicas", type: "integer", param: "replicas" });
    }
    if (kind === "CronJob") {
      rows.push({ kind: "field", path: "spec.schedule", label: "Schedule", type: "string", param: "schedule" });
    }
    rows.push({ kind: "field", path: c + ".image", label: "Image", type: "string", param: "image", placeholder: STARTER_IMAGE });
    rows.push({ kind: "field", path: c + ".name", label: "Container name", type: "string" });
    if (WORKLOADS.indexOf(kind) !== -1) {
      rows.push({ kind: "field", path: c + ".ports[0].containerPort", label: "Port", type: "integer", param: "containerPort" });
    }
    rows.push({ kind: "field", path: c + ".resources.requests[cpu]", label: "CPU request", type: "string", param: "cpuRequest" });
    rows.push({ kind: "field", path: c + ".resources.requests[memory]", label: "Memory request", type: "string", param: "memoryRequest" });
    rows.push({ kind: "field", path: c + ".resources.limits[memory]", label: "Memory limit", type: "string", param: "memoryLimit" });
    rows.push({ kind: "env", prefix: c + ".env", label: "Environment variables" });
    return rows;
  }
  if (kind === "Service") {
    return [
      { kind: "service-selector", label: "Target app" },
      { kind: "field", path: "spec.ports[0].port", label: "Port", type: "integer", param: "servicePort" },
      { kind: "field", path: "spec.ports[0].targetPort", label: "Target port", type: "integer", param: "targetPort" },
      { kind: "field", path: "spec.type", label: "Type", type: "string", enum: ["ClusterIP", "NodePort", "LoadBalancer"] },
    ];
  }
  return null;
}

/** @returns {{starter:(name:string)=>Object, essentials:Array}|null} */
export function profileFor(kind, provider) {
  if (provider && provider !== "k8s") return null;
  const ess = essentialsFor(kind);
  const hasStarter = starterFor(kind, "x") !== null;
  if (!ess && !hasStarter) return null;
  return {
    starter: function (name) { return starterFor(kind, name) || {}; },
    essentials: ess || [],
  };
}
```

**Step 2: Replace the native block in `scaffoldResourceFields`**

In `web-proto/js/regions/canvas.js`, add the import next to the others at the top:

```js
import { profileFor } from "../profiles.js";
```

Then replace the whole "1. Native Kubernetes Workloads" block (the `if (kind === "Deployment") { ... } else if (kind === "Service") { ... }` chain) with:

```js
  // 1. Native kinds: the profile's starter. Drop and the Scaffold button
  // write the same thing; isExplicit no longer gates the container block.
  const profile = profileFor(kind, res && res.provider);
  if (profile) Object.assign(fields, profile.starter(name));
```

In the "2. Required branches from schema" block, change the two branch fallbacks so they never write raw JSON:

```js
  branches.forEach(function (b) {
    if (b.path === "spec.selector") {
      if (!fields["spec.selector.matchLabels[app]"] && !fields["spec.selector[app]"]) {
        fields["spec.selector.matchLabels[app]"] = { value: name };
      }
    } else if (b.path === "spec.template") {
      if (!fields["spec.template.metadata.labels[app]"]) {
        fields["spec.template.metadata.labels[app]"] = { value: name };
      }
      if (!fields["spec.template.spec.containers[0].name"]) {
        fields["spec.template.spec.containers[0].name"] = { value: name };
      }
      if (!fields["spec.template.spec.containers[0].image"]) {
        fields["spec.template.spec.containers[0].image"] = { value: "nginx:1.27" };
      }
    }
  });
```

Keep the signature `scaffoldResourceFields(res, flds, _doc, isExplicit)`; rename the last parameter to `_isExplicit` so eslint's unused-var rule passes, and delete every remaining use of it.

**Step 3: Run the first three tests**

```bash
npx playwright test tests/cf466-starter-deployment.spec.js
```

Expected: tests 1–3 PASS, test 4 (Sync) FAILS (Sync still writes raw JSON).

### Task 3: Migrate readers and writers of the app label to bracket grammar

**Files:**
- Modify: `web-proto/js/regions/inspector/events.js:786-803` (Sync handler), `:449-460` (quick match)
- Modify: `web-proto/js/regions/inspector.js:610-760` (`workloadPresetHtml` readers), `:764-801` (`checkMissingRequired`)
- Modify: `internal/examples/k8s-app.cf.yaml:102-103,116`
- Modify: `tests/cf439-auto-scaffold-drop.spec.js:39,47,105,113`

**Step 1: Sync handler** — replace the body of the `if (t.hasAttribute("data-wl-app"))` branch's `replaceDoc` mutator with:

```js
          r.fields = r.fields || {};
          delete r.fields["spec.selector.matchLabels"];
          delete r.fields["spec.template.metadata.labels"];
          delete r.fields["spec.selector.matchLabels.app"];
          delete r.fields["spec.template.metadata.labels.app"];
          r.fields["spec.selector.matchLabels[app]"] = { value: wlAppVal };
          r.fields["spec.template.metadata.labels[app]"] = { value: wlAppVal };
```

**Step 2: Service quick-match** (`data-svc-match-wl` handler around events.js:449, and the `data-svc-app` entry in `wlSimpleFieldMap`): write `spec.selector[app]` with `{ value }` and delete `spec.selector` / `spec.selector.app`. Change the map entry to `"data-svc-app": "spec.selector[app]"`.

**Step 3: Readers.** In `inspector.js` `workloadPresetHtml`, make `extractApp` prefer the bracket key and keep the raw fallback for blueprints on disk:

```js
    const selAppF = fields["spec.selector.matchLabels[app]"] || fields["spec.selector.matchLabels"] || fields["spec.selector.matchLabels.app"];
    const tmplAppF = fields["spec.template.metadata.labels[app]"] || fields["spec.template.metadata.labels"] || fields["spec.template.metadata.labels.app"];
```

and for Service: `fields["spec.selector[app]"] || fields["spec.selector"] || fields["spec.selector.app"]`. In the Service quick-match candidate loop, read `cwFields["spec.selector.matchLabels[app]"]` first. `checkMissingRequired` already accepts bracket keys through `hasField`'s `startsWith(p + "[")`; add a unit-free sanity check by running the CF-439 inspector tests in Step 6.

**Step 4: Example.** In `internal/examples/k8s-app.cf.yaml` replace

```yaml
        spec.selector.matchLabels: {raw: "{app: worker}"}
        spec.template.metadata.labels: {raw: "{app: worker}"}
```
with
```yaml
        spec.selector.matchLabels[app]: {value: worker}
        spec.template.metadata.labels[app]: {value: worker}
```
and `spec.selector: {raw: "{app: worker}"}` with `spec.selector[app]: {value: worker}`.

Prove the change is emit-neutral: before editing, `go run ./cmd/cf gen --blueprint internal/examples/k8s-app.cf.yaml --out /tmp/k8s-app-before` (check `cf gen --help` for the exact flags); after editing, generate to `/tmp/k8s-app-after` and `diff -r`. Expected: only quoting differences inside the two label values, or none. Record the result in the commit body.

**Step 5: Update `tests/cf439-auto-scaffold-drop.spec.js`** — the four `hasSelector` / `hasLabels` assertions become

```js
        hasSelector: !!r.fields['spec.selector.matchLabels[app]'],
        hasLabels: !!r.fields['spec.template.metadata.labels[app]'],
```

**Step 6: Run**

```bash
npx playwright test tests/cf466-starter-deployment.spec.js tests/cf439-auto-scaffold-drop.spec.js tests/slice63-selectors-functions.spec.js tests/slice65-authoring-ux-enhancements.spec.js
go test ./internal/examples/ ./internal/emit/ -count=1
npm run lint:js && npm run typecheck
```

Expected: all PASS.

**Step 7: Commit**

```bash
git add web-proto/js/profiles.js web-proto/js/regions/canvas.js web-proto/js/regions/inspector.js web-proto/js/regions/inspector/events.js internal/examples/k8s-app.cf.yaml tests/cf466-starter-deployment.spec.js tests/cf439-auto-scaffold-drop.spec.js
git commit -m "feat(canvas): starters are good-practice minimums in map-entry grammar (CF-466)

Drop and Scaffold read one kind profile. Deployment lands with a pinned
image, port, requests and a memory limit; labels use [app] entries so
Generate stops warning about raw templates. k8s-app example migrated;
generated output unchanged (diff recorded above)."
```

---

## Slice 2: essentials form (CF-467)

### Task 4: Failing spec for the essentials form

**Files:**
- Create: `tests/cf467-essentials-form.spec.js`

**Step 1: Write the failing test**

```js
const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, dropKind } = require('./helpers');

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

async function resourceNamed(request, name) {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
  return (doc.spec.resources || []).find(r => r.name === name) || null;
}

const IMG = 'spec.template.spec.containers[0].image';
const ENV = 'spec.template.spec.containers[0].env';

test.describe('CF-467 — essentials form', () => {
  test('Deployment opens with typed essentials rows and editing image commits a value', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    const ess = page.locator('#insp .essentials');
    await expect(ess).toBeVisible();
    await expect(ess.locator('[data-ess-row="spec.replicas"] input[type="number"]')).toHaveValue('2');
    const img = ess.locator(`[data-ess-row="${IMG}"] input.val`);
    await expect(img).toHaveValue('nginx:1.27');
    await img.fill('ghcr.io/acme/web:1.4.2');
    await img.press('Enter');
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      return r && r.fields[IMG] && r.fields[IMG].value;
    }).toBe('ghcr.io/acme/web:1.4.2');
  });

  test('expose creates an XRD parameter with the literal as default and wires the field', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.locator(`#insp [data-ess-row="${IMG}"] button[data-expose]`).click();
    await expect.poll(async () => {
      const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
      const p = doc.spec.xrd.parameters.image;
      const r = doc.spec.resources.find(x => x.name === 'deployment');
      return { pType: p && p.type, pDefault: p && p.default, from: r && r.fields[IMG] && r.fields[IMG].from };
    }).toEqual({ pType: 'string', pDefault: 'nginx:1.27', from: 'params.image' });
    await expect(page.locator(`#insp [data-ess-row="${IMG}"] .bound`)).toContainText('params.image');
    await expect(page.locator('.node[data-id="xrd"], .node.xrd').first()).toContainText('image');
  });

  test('env repeater adds a second variable and delete renumbers', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    const ess = page.locator('#insp .essentials');
    await ess.locator(`button[data-env-row-add="${ENV}"]`).click();
    await ess.locator(`button[data-env-row-add="${ENV}"]`).click();
    await expect(ess.locator('[data-env-row]')).toHaveCount(2);
    const name1 = ess.locator(`input[data-v="${ENV}[1].name"]`);
    await name1.fill('DB_NAME');
    await name1.press('Enter');
    const val1 = ess.locator(`input[data-v="${ENV}[1].value"]`);
    await val1.fill('orders');
    await val1.press('Enter');
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      return r && r.fields[ENV + '[1].name'] && r.fields[ENV + '[1].name'].value + '=' + (r.fields[ENV + '[1].value'] || {}).value;
    }).toBe('DB_NAME=orders');

    await ess.locator(`button[data-env-row-del="${ENV}|0"]`).click();
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      const f = r.fields;
      return { n0: f[ENV + '[0].name'] && f[ENV + '[0].name'].value, has1: !!f[ENV + '[1].name'] };
    }).toEqual({ n0: 'DB_NAME', has1: false });
  });

  test('a provider kind without a profile shows required leaves plus set fields only', async ({ page, request }) => {
    await page.goto('/');
    await page.click('.node[data-id="work-queue"] .node-h');
    const ess = page.locator('#insp .essentials');
    await expect(ess).toBeVisible();
    await expect(ess.locator('[data-ess-row="region"]')).toHaveCount(1);
    const rows = await ess.locator('[data-ess-row]').count();
    expect(rows).toBeLessThan(10);
  });
});
```

Check the XRD card selector before relying on it: `grep -n 'data-id="xrd"' web-proto/js/regions/canvas.js`. If the XRD card uses a different id, adjust the last `expect` in test 2.

**Step 2: Run it to verify it fails**

```bash
npx playwright test tests/cf467-essentials-form.spec.js
```

Expected: FAIL (`#insp .essentials` not found).

### Task 5: Render the essentials form

**Files:**
- Modify: `web-proto/js/regions/inspector.js` — replace `workloadPresetHtml` (lines 604-760) with `essentialsHtml`; call site at line ~1009 (`var wlHtml = workloadPresetHtml(...)`)

**Step 1: Add the import** at the top of `inspector.js`:

```js
import { profileFor, containerPrefix } from "../profiles.js";
```

**Step 2: Write `essentialsHtml`** (replace `workloadPresetHtml` entirely; keep `metadataConventionsHtml`):

```js
function essRowHead(label, path, type, m, extra) {
  return '<div class="fld-h"><span class="lbl" style="flex:0 0 auto">' + esc(label) + '</span>' +
    '<span class="n" style="font-size:10px;color:var(--faint)" title="' + esc(path) + '">' + esc(path) + '</span>' +
    (type ? '<span class="t">' + esc(type) + '</span>' : '') +
    modeButtons(path, m, false) + (extra || '') + '</div>';
}

function essValueControl(res, path, type, row, params, otherResources, otherStatusMap, env) {
  var entry = entryOf(res, path);
  var dm = docMode(entry);
  var m = uiMode[path] || dm;
  var wired = m === "w" && dm === "w" && !uiMode[path] && entry;
  if (m === "w") {
    if (wired) {
      var isStatus = entry.from.indexOf("resources.") === 0, isEnv = entry.from.indexOf("env.") === 0;
      var col = isStatus ? "var(--wire-status)" : (isEnv ? "var(--shared)" : "var(--wire-xrd)");
      return '<div class="bound' + (isEnv ? ' shared' : '') + '"><span style="color:' + col + '">&#8592;</span>' +
        '<span class="src" style="color:' + col + '">' + esc(entry.from) + '</span>' +
        '<span class="x" role="button" tabindex="0" data-unwire="' + esc(path) + '" title="Remove wire">&#215;</span></div>';
    }
    return wireSelectHtml(path, type, params, otherResources, otherStatusMap, false, false, entry && entry.from, env);
  }
  if (m === "r") return rawEditorHtml(path, (dm === "r" && entry) ? entry.raw : "", false, res, params, otherResources, otherStatusMap);
  var val = (dm === "v" && entry) ? entry.value : "";
  if (row.enum) {
    return '<select class="val tsel" data-v="' + esc(path) + '">' + row.enum.map(function (o) {
      return '<option value="' + esc(o) + '"' + (val === o ? ' selected' : '') + '>' + esc(o) + '</option>';
    }).join('') + '</select>';
  }
  if (type === "boolean") {
    return '<select class="val tsel" data-v="' + esc(path) + '">' +
      '<option value=""' + (val === "" ? " selected" : "") + '>unset</option>' +
      '<option value="true"' + (val === "true" ? " selected" : "") + '>true</option>' +
      '<option value="false"' + (val === "false" ? " selected" : "") + '>false</option></select>';
  }
  var inputType = (type === "integer" || type === "number") ? 'type="number" ' : '';
  return '<input class="val" ' + inputType + 'data-v="' + esc(path) + '" value="' + esc(val) + '" placeholder="' + esc(row.placeholder || "") + '">';
}

function exposeButton(res, path, row) {
  if (!row.param) return '';
  var entry = entryOf(res, path);
  if (entry && entry.from) return '';
  return '<button class="btn sm" data-expose="' + esc(path) + '" data-expose-name="' + esc(row.param) + '" data-expose-type="' + esc(row.type || "string") + '" title="Expose as XRD parameter (default = current value) and wire this field to it">&#8599; param</button>';
}

function envRepeaterHtml(res, prefix, params, otherResources, otherStatusMap, env) {
  var fields = res.fields || {};
  var idx = {};
  Object.keys(fields).forEach(function (k) {
    var mm = k.indexOf(prefix + "[") === 0 && /^\[(\d+)\]\.(name|value)$/.exec(k.slice(prefix.length));
    if (mm) idx[mm[1]] = true;
  });
  var rows = Object.keys(idx).map(Number).sort(function (a, b) { return a - b; });
  var h = '<div class="ess-env" data-env-repeater="' + esc(prefix) + '">' +
    '<div class="fld-h"><span class="lbl">Environment variables</span><span class="sp"></span>' +
    '<button class="btn sm" data-env-row-add="' + esc(prefix) + '">+ add variable</button></div>';
  rows.forEach(function (i) {
    var np = prefix + "[" + i + "].name", vp = prefix + "[" + i + "].value";
    var nEntry = entryOf(res, np);
    h += '<div class="frow ess-env-row" data-env-row="' + i + '" style="align-items:flex-start">' +
      '<input class="val tin" data-v="' + esc(np) + '" value="' + esc(nEntry ? nEntry.value : "") + '" placeholder="NAME" style="flex:0 0 38%">' +
      '<div style="flex:1;min-width:0">' + essValueControl(res, vp, "string", {}, params, otherResources, otherStatusMap, env) + '</div>' +
      modeButtons(vp, uiMode[vp] || docMode(entryOf(res, vp)), false) +
      '<button class="del" data-env-row-del="' + esc(prefix) + '|' + i + '" title="Remove variable">&#215;</button></div>';
  });
  if (!rows.length) h += '<div class="g" style="padding:2px 0 4px">none — values can be wired from another resource\'s status</div>';
  return h + '</div>';
}

function essentialsHtml(res, flds, doc, params, otherResources, otherStatusMap, env) {
  var profile = profileFor(res.kind, res.provider);
  var rows;
  if (profile && profile.essentials.length) {
    rows = profile.essentials;
  } else {
    // No profile (every provider resource): chain-required leaves, region, set fields.
    var seen = {}; rows = [];
    ((flds && flds.fields) || []).forEach(function (f) {
      var setHere = !!entryOf(res, f.path);
      if (f.requiredChain || f.path === "region" || setHere) {
        seen[f.path] = true;
        rows.push({ kind: "field", path: f.path, label: f.path.split(".").pop(), type: f.type, enum: f.enum || null });
      }
    });
    Object.keys(res.fields || {}).forEach(function (p) {
      if (!seen[p]) rows.push({ kind: "field", path: p, label: p.split(".").pop(), type: "string" });
    });
  }
  if (!rows.length) return "";
  var isWl = res.provider === "k8s" && (res.kind === "Deployment" || res.kind === "StatefulSet" || res.kind === "DaemonSet");
  var cls = "insp-sec essentials" + (isWl ? " workload-card" : "") + (res.kind === "Service" ? " service-card" : "");
  var h = '<div class="' + cls + '" style="margin:10px 0;padding:10px 12px;border:1px solid var(--wire-xrd);background:var(--surface-2);border-radius:6px">' +
    '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:6px">' +
    '<span style="font-size:11px;font-weight:600;color:var(--wire-xrd);text-transform:uppercase;letter-spacing:0.5px">Essentials</span>' +
    (isWl ? appLabelBadge(res) : '') + '</div>';
  rows.forEach(function (row) {
    if (row.kind === "app-label") { h += appLabelRowHtml(res); return; }
    if (row.kind === "service-selector") { h += serviceSelectorRowHtml(res, doc); return; }
    if (row.kind === "env") { h += envRepeaterHtml(res, row.prefix, params, otherResources, otherStatusMap, env); return; }
    var m = uiMode[row.path] || docMode(entryOf(res, row.path));
    h += '<div class="ess-row" data-ess-row="' + esc(row.path) + '" style="margin-bottom:6px">' +
      essRowHead(row.label, row.path, row.type, m, exposeButton(res, row.path, row)) +
      essValueControl(res, row.path, row.type, row, params, otherResources, otherStatusMap, env) + '</div>';
  });
  return h + '</div>';
}
```

Carry over the existing app-label row (the `data-wl-app` input, Sync button, "Selectors Aligned" / "Sync Required" badge) into `appLabelRowHtml(res)` and `appLabelBadge(res)`, and the Service selector row with its quick-match buttons into `serviceSelectorRowHtml(res, doc)`, reading `[app]` keys first (Task 3). Delete the replicas/image/name/port inputs that used `data-wl-*` from those helpers: the generic rows replace them. Keep `wlSimpleFieldMap` in events.js for `data-svc-*` only.

Update the call site:

```js
    var wlHtml = essentialsHtml(res, flds, doc, params, otherResources, otherStatusMap, env);
```

**Step 3: Handlers in `events.js`** — add to `boxClickActions`:

```js
  {
    selector: "[data-expose]",
    needsDoc: true,
    run: function (btn) {
      var path = btn.getAttribute("data-expose");
      var base = btn.getAttribute("data-expose-name") || path.split(/[.[]/).pop().replace(/]$/, "");
      var type = btn.getAttribute("data-expose-type") || "string";
      var res = state.selectedResource();
      if (!res) return;
      var entry = state.entryOf(res, path);
      var literal = entry && entry.value ? entry.value : "";
      var doc = state.store.state.doc;
      var params = (doc.spec.xrd && doc.spec.xrd.parameters) || {};
      var name = base;
      if (params[name]) {
        var suffix = res.name.split(/[^a-zA-Z0-9]+/).filter(Boolean).map(function (s) { return s[0].toUpperCase() + s.slice(1); }).join("");
        name = base + suffix;
      }
      state.op(function () { return state.store.addParameter(name, { type: type, required: false, default: literal }); })
        .then(function (d) {
          if (d === null) return null;
          return state.setField(path, { from: "params." + name, value: "", raw: "" });
        });
    }
  },
  {
    selector: "[data-env-row-add]",
    needsDoc: true,
    run: function (btn) {
      var prefix = btn.getAttribute("data-env-row-add");
      var res = state.selectedResource();
      if (!res) return;
      var n = 0;
      Object.keys(res.fields || {}).forEach(function (k) {
        var mm = k.indexOf(prefix + "[") === 0 && /^\[(\d+)\]\./.exec(k.slice(prefix.length));
        if (mm) n = Math.max(n, parseInt(mm[1], 10) + 1);
      });
      state.setField(prefix + "[" + n + "].name", { value: "VAR_" + (n + 1), from: "", raw: "" });
    }
  },
  {
    selector: "[data-env-row-del]",
    needsDoc: true,
    run: function (btn) {
      var parts = btn.getAttribute("data-env-row-del").split("|");
      var prefix = parts[0], del = parseInt(parts[1], 10);
      var rname = state.store.state.selectedResource;
      state.op(function () {
        return state.store.replaceDoc(function (d) {
          var r = (d.spec.resources || []).find(function (x) { return x.name === rname; });
          if (!r || !r.fields) return;
          var next = {};
          Object.keys(r.fields).forEach(function (k) {
            var mm = k.indexOf(prefix + "[") === 0 && /^\[(\d+)\](\..*)$/.exec(k.slice(prefix.length));
            if (!mm) { next[k] = r.fields[k]; return; }
            var i = parseInt(mm[1], 10);
            if (i === del) return;
            next[prefix + "[" + (i > del ? i - 1 : i) + "]" + mm[2]] = r.fields[k];
          });
          r.fields = next;
        });
      });
    }
  },
```

Check `addParameter`'s server validation accepts `default: "2"` for `type: integer` (POST `/api/blueprint/parameters` with that body against the running engine). If it demands a number, coerce in the expose action: `default: type === "integer" || type === "number" ? Number(literal) : literal`.

**Step 4: Run**

```bash
npx playwright test tests/cf467-essentials-form.spec.js tests/cf466-starter-deployment.spec.js tests/slice63-selectors-functions.spec.js tests/slice65-authoring-ux-enhancements.spec.js tests/cf439-auto-scaffold-drop.spec.js
npm run lint:js && npm run typecheck
```

Expected: all PASS.

**Step 5: Commit**

```bash
git add web-proto/js/profiles.js web-proto/js/regions/inspector.js web-proto/js/regions/inspector/events.js tests/cf467-essentials-form.spec.js
git commit -m "feat(inspector): essentials form per kind with expose-as-parameter and env repeater (CF-467)"
```

---

## Slice 3a: manifest round trip in Go (CF-468)

### Task 6: `internal/manifest` package — failing tests

**Files:**
- Create: `internal/manifest/manifest_test.go`

**Step 1: Write the failing tests**

```go
package manifest

import (
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/schema/k8s"
)

func deploymentNodes(t *testing.T) []*schema.Node {
	t.Helper()
	kinds, err := k8s.Kinds()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range kinds {
		if c.Kind == "Deployment" {
			nodes, err := c.FieldTree()
			if err != nil {
				t.Fatal(err)
			}
			return nodes
		}
	}
	t.Fatal("no Deployment")
	return nil
}

func starter() map[string]blueprint.Field {
	return map[string]blueprint.Field{
		"spec.replicas":                                               {From: "params.replicas"},
		"spec.selector.matchLabels[app]":                              {Value: "web"},
		"spec.template.metadata.labels[app]":                          {Value: "web"},
		"spec.template.spec.containers[0].name":                       {Value: "web"},
		"spec.template.spec.containers[0].image":                      {Value: "nginx:1.27"},
		"spec.template.spec.containers[0].ports[0].containerPort":     {Value: "80"},
		"spec.template.spec.containers[0].resources.requests[cpu]":    {Value: "100m"},
		"spec.template.spec.containers[0].resources.requests[memory]": {Value: "128Mi"},
		"spec.template.spec.containers[0].env[1].name":                {Value: "B"},
		"spec.template.spec.containers[0].env[0].name":                {Value: "A"},
		"spec.template.spec.containers[0].env[0].value":               {From: "resources.queue.status.atProvider.url"},
		"spec.template.spec.containers[0].command":                    {Value: "sh,-c"},
		"metadata.annotations[app.kubernetes.io/part-of]":             {Value: "shop"},
	}
}

func TestRenderNestsFieldsInManifestShape(t *testing.T) {
	out, err := Render(deploymentNodes(t), starter())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"spec:\n", "  replicas: {from: params.replicas}\n", "    matchLabels:\n      app: web\n",
		"      containers:\n        - command: [sh, -c]\n", "          image: nginx:1.27\n",
		"          ports:\n            - containerPort: 80\n", "            requests:\n              cpu: 100m\n",
		"          env:\n            - name: A\n              value: {from: resources.queue.status.atProvider.url}\n            - name: B\n",
		"metadata:\n  annotations:\n    app.kubernetes.io/part-of: shop\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered manifest lacks %q:\n%s", want, out)
		}
	}
}

func TestParseRoundTripsRender(t *testing.T) {
	nodes := deploymentNodes(t)
	in := starter()
	out, err := Render(nodes, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(nodes, out)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, out)
	}
	if len(got) != len(in) {
		t.Fatalf("round trip changed field count: got %d want %d\n%v", len(got), len(in), got)
	}
	for p, f := range in {
		if got[p] != f {
			t.Errorf("%s: got %+v want %+v", p, got[p], f)
		}
	}
}

func TestParseUnknownKeyReportsPathAndLine(t *testing.T) {
	_, err := Parse(deploymentNodes(t), "spec:\n  replicas: 2\n  template:\n    spec:\n      containers:\n        - imagee: x\n")
	var me *Error
	if !asError(err, &me) {
		t.Fatalf("want *Error, got %T %v", err, err)
	}
	if me.Path != "spec.template.spec.containers[0].imagee" || me.Line != 6 {
		t.Errorf("got path %q line %d, want containers[0].imagee at line 6", me.Path, me.Line)
	}
}

func TestParseScalarAtObjectIsAnError(t *testing.T) {
	_, err := Parse(deploymentNodes(t), "spec:\n  selector: '{app: web}'\n")
	var me *Error
	if !asError(err, &me) || me.Path != "spec.selector" || me.Line != 2 {
		t.Fatalf("want error at spec.selector line 2, got %v", err)
	}
	if !strings.Contains(me.Error(), "{raw:") {
		t.Errorf("message should point at the explicit raw form: %s", me.Error())
	}
}

func TestParseExplicitRawAtMapNode(t *testing.T) {
	got, err := Parse(deploymentNodes(t), "spec:\n  selector:\n    matchLabels: {raw: \"{app: web}\"}\n")
	if err != nil {
		t.Fatal(err)
	}
	if got["spec.selector.matchLabels"].Raw != "{app: web}" {
		t.Errorf("got %+v", got)
	}
}

func TestParseWrapperWithTwoKeysIsNotAWrapper(t *testing.T) {
	_, err := Parse(deploymentNodes(t), "spec:\n  replicas: {from: params.a, value: 2}\n")
	if err == nil {
		t.Fatal("want error for a two-key wrapper")
	}
}

func TestParseNullLeavesUnset(t *testing.T) {
	got, err := Parse(deploymentNodes(t), "spec:\n  replicas:\n  paused: false\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["spec.replicas"]; ok {
		t.Error("null scalar must not create a field")
	}
	if got["spec.paused"].Value != "false" {
		t.Errorf("got %+v", got)
	}
}

func TestRenderEmptyFieldsIsEmptyDocument(t *testing.T) {
	out, err := Render(deploymentNodes(t), nil)
	if err != nil || strings.TrimSpace(out) != "{}" {
		t.Fatalf("got %q err %v", out, err)
	}
}
```

Add at the bottom of the test file:

```go
func asError(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}
```

**Step 2: Run to verify it fails**

```bash
go test ./internal/manifest/ -count=1
```

Expected: compile error (package does not exist).

### Task 7: Implement `internal/manifest`

**Files:**
- Create: `internal/manifest/manifest.go`

**Step 1: Write the package**

```go
// Package manifest converts a resource's flat blueprint field map to and
// from nested manifest-shaped YAML. The kind's schema tree is the authority
// for path grammar: a map node's children become [key] entries, an array of
// objects' children become [i] elements, an object's members use dots.
// Leaves carry the blueprint's own wrappers ({value|from|raw|template});
// a plain scalar is a literal.
package manifest

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// Error carries the offending path and YAML line so a UI can highlight it.
type Error struct {
	Path string
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s: %s", e.Line, e.Path, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Msg)
}

var wrapperKeys = map[string]bool{"value": true, "from": true, "raw": true, "template": true}

// ---- path tokenizer -------------------------------------------------------

type seg struct {
	name  string // object member or array/map base name ("" for a bare index/key segment)
	idx   int    // array index when isIdx
	key   string // map key when isKey
	isIdx bool
	isKey bool
}

// tokenize splits "a.b[0].c[app.kubernetes.io/x]" into segments; everything
// inside [...] is one token, dots included.
func tokenize(path string) ([]seg, error) {
	var out []seg
	i := 0
	for i < len(path) {
		j := i
		for j < len(path) && path[j] != '.' && path[j] != '[' {
			j++
		}
		if j > i {
			out = append(out, seg{name: path[i:j]})
		}
		i = j
		for i < len(path) && path[i] == '[' {
			k := strings.IndexByte(path[i:], ']')
			if k < 0 {
				return nil, fmt.Errorf("unterminated [ in %q", path)
			}
			inner := path[i+1 : i+k]
			if n, err := strconv.Atoi(inner); err == nil && inner != "" {
				out = append(out, seg{isIdx: true, idx: n})
			} else {
				out = append(out, seg{isKey: true, key: strings.Trim(inner, `"'`)})
			}
			i += k + 1
		}
		if i < len(path) && path[i] == '.' {
			i++
		}
	}
	return out, nil
}

// ---- Render ---------------------------------------------------------------

type node struct {
	children []*node
	byKey    map[string]*node
	elems    map[int]*node
	kind     string // "obj" | "seq" | leaf ""
	label    string // key name (obj child) or map key
	field    *blueprint.Field
	schema   *schema.Node
	path     string
}

func newNode(label, path string, sn *schema.Node) *node {
	return &node{byKey: map[string]*node{}, elems: map[int]*node{}, label: label, path: path, schema: sn}
}

func childSchema(parent *schema.Node, name string, roots []*schema.Node) *schema.Node {
	list := roots
	if parent != nil {
		list = parent.Children
	}
	for _, c := range list {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Render nests fields into manifest YAML. Unknown paths still render (the
// engine's own validation owns that message); the schema only decides
// scalar tags.
func Render(nodes []*schema.Node, fields map[string]blueprint.Field) (string, error) {
	root := newNode("", "", nil)
	paths := make([]string, 0, len(fields))
	for p := range fields {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		f := fields[p]
		segs, err := tokenize(p)
		if err != nil {
			return "", err
		}
		cur := root
		var sn *schema.Node
		for si, s := range segs {
			var next *node
			switch {
			case s.isIdx:
				next = cur.elems[s.idx]
				if next == nil {
					next = newNode("", cur.path+"["+strconv.Itoa(s.idx)+"]", cur.schema)
					cur.elems[s.idx] = next
					cur.kind = "seq"
				}
			case s.isKey:
				next = cur.byKey[s.key]
				if next == nil {
					next = newNode(s.key, cur.path+"["+s.key+"]", cur.schema)
					cur.byKey[s.key] = next
					cur.children = append(cur.children, next)
					cur.kind = "obj"
				}
			default:
				sn = childSchema(sn, s.name, nodes)
				if s.isIdx || s.isKey {
					sn = cur.schema
				}
				next = cur.byKey[s.name]
				if next == nil {
					np := s.name
					if cur.path != "" {
						np = cur.path + "." + s.name
					}
					next = newNode(s.name, np, sn)
					cur.byKey[s.name] = next
					cur.children = append(cur.children, next)
					cur.kind = "obj"
				}
			}
			if si == len(segs)-1 {
				if next.kind != "" {
					return "", &Error{Path: p, Msg: "sets a whole value that other fields set members of"}
				}
				ff := f
				next.field = &ff
			} else if next.field != nil {
				return "", &Error{Path: p, Msg: fmt.Sprintf("conflicts with %q which sets the whole value", next.path)}
			}
			cur = next
		}
	}
	y := toYAML(root)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(y); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func flowMap(key, val string) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: key},
		{Kind: yaml.ScalarNode, Value: val},
	}}
}

func scalarFor(f *blueprint.Field, sn *schema.Node) *yaml.Node {
	switch {
	case f.From != "":
		return flowMap("from", f.From)
	case f.Raw != "":
		return flowMap("raw", f.Raw)
	case f.Template != "":
		return flowMap("template", f.Template)
	}
	typ := ""
	if sn != nil {
		typ = sn.Type
	}
	if typ == "array" && (sn == nil || len(sn.Children) == 0) {
		items := strings.Split(f.Value, ",")
		n := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, it := range items {
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.TrimSpace(it)})
		}
		return n
	}
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: f.Value}
	switch typ {
	case "integer":
		if _, err := strconv.ParseInt(f.Value, 10, 64); err == nil {
			n.Tag = "!!int"
			return n
		}
	case "number":
		if _, err := strconv.ParseFloat(f.Value, 64); err == nil {
			n.Tag = "!!float"
			return n
		}
	case "boolean":
		if f.Value == "true" || f.Value == "false" {
			n.Tag = "!!bool"
			return n
		}
	}
	n.Tag = "!!str"
	return n
}

func toYAML(n *node) *yaml.Node {
	if n.field != nil {
		return scalarFor(n.field, n.schema)
	}
	if n.kind == "seq" {
		idx := make([]int, 0, len(n.elems))
		for i := range n.elems {
			idx = append(idx, i)
		}
		sort.Ints(idx)
		out := &yaml.Node{Kind: yaml.SequenceNode}
		last := -1
		for _, i := range idx {
			for gap := last + 1; gap < i; gap++ {
				out.Content = append(out.Content, &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle})
			}
			out.Content = append(out.Content, toYAML(n.elems[i]))
			last = i
		}
		return out
	}
	out := &yaml.Node{Kind: yaml.MappingNode}
	if len(n.children) == 0 {
		out.Style = yaml.FlowStyle
	}
	for _, c := range n.children {
		out.Content = append(out.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: c.label}, toYAML(c))
	}
	return out
}

// ---- Parse ----------------------------------------------------------------

// Parse walks manifest YAML against the schema tree and flattens it to
// fields. A mapping whose only key is value/from/raw/template is a wrapper.
func Parse(nodes []*schema.Node, text string) (map[string]blueprint.Field, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, &Error{Path: "", Line: yamlErrLine(err), Msg: err.Error()}
	}
	out := map[string]blueprint.Field{}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return out, nil
	}
	body := doc.Content[0]
	if body.Kind == yaml.ScalarNode && body.Tag == "!!null" {
		return out, nil
	}
	if body.Kind != yaml.MappingNode {
		return nil, &Error{Path: "", Line: body.Line, Msg: "manifest must be a mapping"}
	}
	if err := walkObject(body, nodes, "", out); err != nil {
		return nil, err
	}
	return out, nil
}

func yamlErrLine(err error) int {
	var l int
	if _, e := fmt.Sscanf(err.Error(), "yaml: line %d:", &l); e == nil {
		return l
	}
	return 0
}

func wrapper(n *yaml.Node) (blueprint.Field, bool, *Error) {
	if n.Kind != yaml.MappingNode || len(n.Content) != 2 {
		if n.Kind == yaml.MappingNode && len(n.Content) == 4 && wrapperKeys[n.Content[0].Value] && wrapperKeys[n.Content[2].Value] {
			return blueprint.Field{}, false, &Error{Line: n.Line, Msg: "a wrapper takes exactly one of value, from, raw, template"}
		}
		return blueprint.Field{}, false, nil
	}
	k, v := n.Content[0], n.Content[1]
	if !wrapperKeys[k.Value] || v.Kind != yaml.ScalarNode {
		return blueprint.Field{}, false, nil
	}
	switch k.Value {
	case "value":
		return blueprint.Field{Value: v.Value}, true, nil
	case "from":
		return blueprint.Field{From: v.Value}, true, nil
	case "raw":
		return blueprint.Field{Raw: v.Value}, true, nil
	default:
		return blueprint.Field{Template: v.Value}, true, nil
	}
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func walkObject(m *yaml.Node, children []*schema.Node, prefix string, out map[string]blueprint.Field) error {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		path := join(prefix, k.Value)
		sn := childSchema(nil, k.Value, children)
		if sn == nil {
			return &Error{Path: path, Line: k.Line, Msg: "unknown field (not in the schema)"}
		}
		if err := walkValue(v, sn, path, out); err != nil {
			return err
		}
	}
	return nil
}

func walkValue(v *yaml.Node, sn *schema.Node, path string, out map[string]blueprint.Field) error {
	if v.Kind == yaml.ScalarNode && v.Tag == "!!null" {
		return nil
	}
	if f, ok, werr := wrapper(v); werr != nil {
		werr.Path = path
		return werr
	} else if ok {
		out[path] = f
		return nil
	}
	isObj := sn.Type != "array" && sn.Type != "map" && len(sn.Children) > 0
	switch {
	case isObj:
		if v.Kind != yaml.MappingNode {
			return &Error{Path: path, Line: v.Line, Msg: "expected a mapping; to set the whole object verbatim use {raw: \"...\"}"}
		}
		return walkObject(v, sn.Children, path, out)
	case sn.Type == "map":
		if v.Kind != yaml.MappingNode {
			return &Error{Path: path, Line: v.Line, Msg: "expected a mapping of keys; to set the whole map verbatim use {raw: \"...\"}"}
		}
		for i := 0; i+1 < len(v.Content); i += 2 {
			k, ev := v.Content[i], v.Content[i+1]
			ep := path + "[" + k.Value + "]"
			if ev.Kind == yaml.ScalarNode {
				if ev.Tag != "!!null" {
					out[ep] = blueprint.Field{Value: ev.Value}
				}
				continue
			}
			if f, ok, werr := wrapper(ev); werr != nil {
				werr.Path = ep
				return werr
			} else if ok {
				out[ep] = f
				continue
			}
			return &Error{Path: ep, Line: ev.Line, Msg: "a map entry must be a scalar or a {value|from|raw} wrapper"}
		}
		return nil
	case sn.Type == "array" && len(sn.Children) > 0:
		if v.Kind != yaml.SequenceNode {
			return &Error{Path: path, Line: v.Line, Msg: "expected a list; to set the whole list verbatim use {raw: \"...\"}"}
		}
		for i, item := range v.Content {
			ep := path + "[" + strconv.Itoa(i) + "]"
			if f, ok, werr := wrapper(item); werr != nil {
				werr.Path = ep
				return werr
			} else if ok {
				out[ep] = f
				continue
			}
			if item.Kind != yaml.MappingNode {
				return &Error{Path: ep, Line: item.Line, Msg: "expected a mapping for a list element"}
			}
			if err := walkObject(item, sn.Children, ep, out); err != nil {
				return err
			}
		}
		return nil
	case sn.Type == "array":
		if v.Kind == yaml.SequenceNode {
			parts := make([]string, 0, len(v.Content))
			for _, item := range v.Content {
				if item.Kind != yaml.ScalarNode || strings.Contains(item.Value, ",") {
					return &Error{Path: path, Line: item.Line, Msg: "list entries must be scalars without commas; use {raw: \"[...]\"} otherwise"}
				}
				parts = append(parts, item.Value)
			}
			out[path] = blueprint.Field{Value: strings.Join(parts, ",")}
			return nil
		}
		if v.Kind == yaml.ScalarNode {
			out[path] = blueprint.Field{Value: v.Value}
			return nil
		}
		return &Error{Path: path, Line: v.Line, Msg: "expected a list of scalars"}
	default:
		if v.Kind != yaml.ScalarNode {
			return &Error{Path: path, Line: v.Line, Msg: "expected a scalar or a {value|from|raw|template} wrapper"}
		}
		out[path] = blueprint.Field{Value: v.Value}
		return nil
	}
}
```

Two things to verify while implementing, and fix in place:

1. In `Render`, the `default:` branch computes `sn` before checking `s.isIdx || s.isKey`; simplify so schema lookup happens only for named segments: `sn = childSchema(cur.schema, s.name, nodes)` when `cur.schema != nil || cur == root`, and index/key segments inherit `cur.schema` (already done via `newNode(..., cur.schema)`). Remove the dead `if s.isIdx || s.isKey` line.
2. yaml.v3 renders a `!!str` scalar such as `100m` or `nginx:1.27` plainly and quotes only when needed. Run the Render test and adjust the expected strings to what the encoder actually emits if it quotes `sh` or `-c` (a leading `-` forces quoting). Never weaken the round-trip test.

**Step 2: Run**

```bash
go test ./internal/manifest/ -count=1 -v
```

Expected: PASS.

**Step 3: Commit**

```bash
git add internal/manifest/
git commit -m "feat(manifest): schema-driven round trip between flat fields and manifest YAML (CF-468)"
```

### Task 8: API routes — failing tests

**Files:**
- Create: `internal/api/manifest_test.go`

**Step 1: Write the failing tests**

```go
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/koorikla/compositionfactory/internal/blueprint"
)

func seedDeployment(t *testing.T, h http.Handler) {
	t.Helper()
	res := blueprint.Resource{Name: "web", Kind: "Deployment", Provider: "k8s", Fields: map[string]blueprint.Field{
		"spec.replicas":                          {Value: "2"},
		"spec.selector.matchLabels[app]":         {Value: "web"},
		"spec.template.metadata.labels[app]":     {Value: "web"},
		"spec.template.spec.containers[0].name":  {Value: "web"},
		"spec.template.spec.containers[0].image": {Value: "nginx:1.27"},
	}}
	body, _ := json.Marshal(res)
	if rec := do(t, h, "POST", "/api/blueprint/resources", string(body)); rec.Code != 200 {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body)
	}
}

func TestGetResourceManifestRendersNestedYAML(t *testing.T) {
	h := testHandler(t)
	seedDeployment(t, h)
	rec := do(t, h, "GET", "/api/blueprint/resources/web/manifest", "")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		YAML string `json:"yaml"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.YAML, "      containers:\n        - image: nginx:1.27\n") {
		t.Errorf("unexpected yaml:\n%s", out.YAML)
	}
}

func TestPutResourceManifestReplacesFields(t *testing.T) {
	h, path := testHandlerWithPath(t)
	seedDeployment(t, h)
	y := "spec:\n  replicas: 3\n  selector:\n    matchLabels:\n      app: web\n  template:\n    metadata:\n      labels:\n        app: web\n    spec:\n      containers:\n        - name: web\n          image: nginx:1.27\n          ports:\n            - containerPort: 80\n            - containerPort: 9090\n"
	body, _ := json.Marshal(map[string]string{"yaml": y})
	rec := do(t, h, "PUT", "/api/blueprint/resources/web/manifest", string(body))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	b, err := blueprint.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	f := b.ResourceNamed("web").Fields
	if f["spec.replicas"].Value != "3" || f["spec.template.spec.containers[0].ports[1].containerPort"].Value != "9090" {
		t.Errorf("fields not replaced: %+v", f)
	}
}

func TestPutResourceManifestUnknownKeyIs400WithPathAndLine(t *testing.T) {
	h := testHandler(t)
	seedDeployment(t, h)
	body, _ := json.Marshal(map[string]string{"yaml": "spec:\n  replicas: 3\n  replicaz: 4\n"})
	rec := do(t, h, "PUT", "/api/blueprint/resources/web/manifest", string(body))
	if rec.Code != 400 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var e struct {
		Error string `json:"error"`
		Path  string `json:"path"`
		Line  int    `json:"line"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Path != "spec.replicaz" || e.Line != 3 || !strings.Contains(e.Error, "unknown field") {
		t.Errorf("got %+v", e)
	}
}

func TestPutResourceManifestUnknownResourceIs404(t *testing.T) {
	h := testHandler(t)
	body, _ := json.Marshal(map[string]string{"yaml": "spec: {}\n"})
	if rec := do(t, h, "PUT", "/api/blueprint/resources/nope/manifest", string(body)); rec.Code != 404 {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestPutResourceManifestRunsCRDValidation(t *testing.T) {
	h := testHandler(t)
	body, _ := json.Marshal(map[string]string{"yaml": "region: eu-north-1\nvisibilityTimeoutSeconds: notanumber\n"})
	rec := do(t, h, "PUT", "/api/blueprint/resources/main-queue/manifest", string(body))
	if rec.Code != 400 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}
```

Check the queue fixture's field names first: `grep -n "visibilityTimeout\|region" internal/api/testdata/*.yaml | head`. Use a field the Queue schema types as integer; if `visibilityTimeoutSeconds` is not it, pick one from `queue.fields.json` in the contract fixtures.

**Step 2: Run to verify failure**

```bash
go test ./internal/api/ -run 'ResourceManifest' -count=1
```

Expected: FAIL with 404 / 405 (routes missing).

### Task 9: Implement the routes

**Files:**
- Create: `internal/api/manifest.go`
- Modify: `internal/api/server.go:243` (routes), `internal/api/blueprint.go:276-305` (`mutate` error body)
- Modify: `internal/emit/composition.go:1590` (export a wrapper)

**Step 1: Export the resolver** — in `internal/emit/composition.go` add above `resolveKind`:

```go
// ResolveKind is resolveKind for callers outside emit that need the CRD a
// resource composes against (the manifest endpoints).
func ResolveKind(crds []schema.CRD, r blueprint.Resource, wantNamespaced bool) (schema.CRD, error) {
	return resolveKind(crds, r, wantNamespaced)
}
```

**Step 2: Richer error bodies from `mutate`.** In `blueprint.go`, replace `writeJSONError(w, status, err.Error())` inside `mutate` with `writeMutateError(w, status, err)` and add:

```go
// detailedError lets a mutation return extra JSON fields beside "error"
// (the manifest endpoints add path and line so a UI can highlight them).
type detailedError interface {
	error
	Body() map[string]any
}

func writeMutateError(w http.ResponseWriter, status int, err error) {
	var de detailedError
	if errors.As(err, &de) {
		body := de.Body()
		body["error"] = err.Error()
		writeJSON(w, status, body)
		return
	}
	writeJSONError(w, status, err.Error())
}
```

**Step 3: `internal/api/manifest.go`**

```go
package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/emit"
	"github.com/koorikla/compositionfactory/internal/manifest"
	"github.com/koorikla/compositionfactory/internal/schema"
)

// manifestResponse is GET /api/blueprint/resources/{name}/manifest's body.
type manifestResponse struct {
	YAML string `json:"yaml"`
}

type manifestRequest struct {
	YAML string `json:"yaml"`
}

// manifestErr adapts manifest.Error to the mutate error body contract.
type manifestErr struct{ *manifest.Error }

func (m manifestErr) Body() map[string]any {
	return map[string]any{"path": m.Path, "line": m.Line}
}

func (srv *server) fieldTreeFor(b *blueprint.Blueprint, r blueprint.Resource) ([]*schema.Node, error) {
	crds, err := srv.loadSourceCRDs(b)
	if err != nil {
		return nil, err
	}
	crd, err := emit.ResolveKind(crds, r, b.Spec.XRD.Scope == "Namespaced")
	if err != nil {
		return nil, err
	}
	return crd.FieldTree()
}

// handleGetResourceManifest serves GET /api/blueprint/resources/{name}/manifest.
func (srv *server) handleGetResourceManifest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	b, ok := srv.loadBlueprint(w)
	if !ok {
		return
	}
	res := b.ResourceNamed(name)
	if res == nil {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("resource %q is not declared", name))
		return
	}
	nodes, err := srv.fieldTreeFor(b, *res)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	y, err := manifest.Render(nodes, res.Fields)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, manifestResponse{YAML: y})
}

// handleSetResourceManifest serves PUT /api/blueprint/resources/{name}/manifest:
// replace the resource's fields in full from manifest-shaped YAML, validated
// against the kind's schema (unknown keys fail with path and line) and then
// through the same CRD validation PUT /api/blueprint/resources/{name} runs.
func (srv *server) handleSetResourceManifest(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req manifestRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	srv.mutate(w, r, func(b *blueprint.Blueprint) (int, error) {
		res := b.ResourceNamed(name)
		if res == nil {
			return http.StatusNotFound, fmt.Errorf("resource %q is not declared", name)
		}
		nodes, err := srv.fieldTreeFor(b, *res)
		if err != nil {
			return http.StatusBadRequest, err
		}
		fields, err := manifest.Parse(nodes, req.YAML)
		if err != nil {
			var me *manifest.Error
			if errors.As(err, &me) {
				return http.StatusBadRequest, manifestErr{me}
			}
			return http.StatusBadRequest, err
		}
		next := *res
		next.Fields = fields
		if err := b.SetResource(name, next); err != nil {
			return http.StatusBadRequest, err
		}
		if crds, err := srv.loadSourceCRDs(b); err == nil {
			if err := srv.validateBlueprintAgainstCRDs(b, crds); err != nil {
				return http.StatusBadRequest, err
			}
		}
		return http.StatusOK, nil
	})
}
```

`errors.As` needs `manifestErr` to unwrap: add `func (m manifestErr) Unwrap() error { return m.Error }` and make `writeMutateError` match on `detailedError` (it does, since `manifestErr` implements `Body()` and `error`). Also `blueprint.Resource` may carry `Fields: nil` for a resource with no fields; `SetResource` validates — fine.

**Step 4: Routes** — in `server.go` after the `PUT /api/blueprint/resources/{name}` line:

```go
	mux.HandleFunc("GET /api/blueprint/resources/{name}/manifest", srv.handleGetResourceManifest)
	mux.HandleFunc("PUT /api/blueprint/resources/{name}/manifest", srv.handleSetResourceManifest)
```

**Step 5: Run**

```bash
go test ./internal/api/ -run 'ResourceManifest|Contract' -count=1
gofmt -l internal/ && go vet ./internal/...
```

Expected: manifest tests PASS.

**Step 6: Contract fixture** — create `internal/api/testdata/contract/manifest.json`:

```json
{
  "yaml": "spec:\n  replicas: 2\n"
}
```

and add to `contract_fixtures_test.go`:

```go
func TestContractFixtureManifestRoundTripsKeySet(t *testing.T) {
	checkFixtureKeySetRoundTrips(t, filepath.Join(fixturesDir, "manifest.json"), &manifestResponse{})
}
```

**Step 7: Commit**

```bash
git add internal/api/manifest.go internal/api/manifest_test.go internal/api/server.go internal/api/blueprint.go internal/api/contract_fixtures_test.go internal/api/testdata/contract/manifest.json internal/emit/composition.go
git commit -m "feat(api): GET/PUT resource manifest with schema-validated flatten (CF-468)"
```

### Task 10: MCP tools

**Files:**
- Modify: `internal/mcp/tools.go` (register after `update_resource`), `internal/mcp/server_test.go:245-249` (want list), `docs/mcp.md` tool table

**Step 1: Failing test** — in `server_test.go` add `"get_resource_manifest"` and `"set_resource_manifest"` to the sorted `want` list (alphabetical: after `generate`, `get_blueprint`, `get_kind_fields` comes `get_resource_manifest`; `set_resource_manifest` goes after `require`... place it between `replace_blueprint` and `update_parameter`). Add:

```go
func TestResourceManifestRoundTrip(t *testing.T) {
	s := newStack(t)
	got := s.toolOK(t, "get_resource_manifest", map[string]any{"name": "main-queue"})
	y, _ := got["yaml"].(string)
	if !strings.Contains(y, "region:") {
		t.Fatalf("manifest lacks region:\n%s", y)
	}
	s.toolOK(t, "set_resource_manifest", map[string]any{"name": "main-queue", "yaml": y})
}

func TestSetResourceManifestErrorMatchesHTTP(t *testing.T) {
	s := newStack(t)
	body := `{"yaml":"regionn: x\n"}`
	s.assertToolErrorMatchesHTTP(t,
		"set_resource_manifest", map[string]any{"name": "main-queue", "yaml": "regionn: x\n"},
		http.MethodPut, "/api/blueprint/resources/main-queue/manifest", body)
}
```

Check the MCP test blueprint's resource name (`grep -n "name:" internal/mcp/server_test.go | head`) and substitute for `main-queue`.

Run: `go test ./internal/mcp/ -run 'ListTools|ResourceManifest' -count=1` — expected FAIL.

**Step 2: Register the tools** in `tools.go` after the `update_resource` registration:

```go
	sdk.AddTool(srv, &sdk.Tool{
		Name: "get_resource_manifest",
		Description: "One composed resource's set fields rendered as nested manifest-shaped YAML. " +
			"Literals are plain scalars; wires and raw values appear as {from: ...} / {raw: ...} wrappers.",
	}, s.getResourceManifest)

	sdk.AddTool(srv, &sdk.Tool{
		Name: "set_resource_manifest",
		Description: "Replace a composed resource's fields IN FULL from manifest-shaped YAML. The kind's schema " +
			"decides map/array/object grammar; unknown keys fail with path and line. Same CRD validation as update_resource.",
	}, s.setResourceManifest)
```

and the handlers:

```go
type getResourceManifestInput struct {
	Name string `json:"name" jsonschema:"The declared composed resource."`
}

func (s *server) getResourceManifest(_ context.Context, _ *sdk.CallToolRequest, in getResourceManifestInput) (*sdk.CallToolResult, any, error) {
	if in.Name == "" {
		return nil, nil, errors.New("name is required")
	}
	return s.bridge(http.MethodGet, "/api/blueprint/resources/"+url.PathEscape(in.Name)+"/manifest", nil)
}

type setResourceManifestInput struct {
	Name string `json:"name" jsonschema:"The declared composed resource."`
	YAML string `json:"yaml" jsonschema:"Manifest-shaped YAML of the resource's fields (see get_resource_manifest)."`
}

func (s *server) setResourceManifest(_ context.Context, _ *sdk.CallToolRequest, in setResourceManifestInput) (*sdk.CallToolResult, any, error) {
	if in.Name == "" {
		return nil, nil, errors.New("name is required")
	}
	body, err := json.Marshal(struct {
		YAML string `json:"yaml"`
	}{YAML: in.YAML})
	if err != nil {
		return nil, nil, fmt.Errorf("encode request: %w", err)
	}
	return s.bridge(http.MethodPut, "/api/blueprint/resources/"+url.PathEscape(in.Name)+"/manifest", body)
}
```

**Step 3: Docs** — add two rows to the tool table in `docs/mcp.md` under `update_resource`:

```
| `get_resource_manifest` | `GET /api/blueprint/resources/{name}/manifest` | The resource's set fields as nested manifest-shaped YAML; wires and raw values as `{from: …}` / `{raw: …}` wrappers. |
| `set_resource_manifest` | `PUT /api/blueprint/resources/{name}/manifest` | Replace the resource's fields in full from manifest-shaped YAML; the kind's schema decides map/array/object grammar, unknown keys fail with `path` and `line`. |
```

**Step 4: Run and commit**

```bash
go test ./internal/mcp/ -count=1
git add internal/mcp/tools.go internal/mcp/server_test.go docs/mcp.md
git commit -m "feat(mcp): get_resource_manifest and set_resource_manifest bridge the manifest routes (CF-468)"
```

---

## Slice 3b: manifest editor, view toggle and search in the canvas (CF-468)

### Task 11: Failing spec

**Files:**
- Create: `tests/cf468-manifest-editor.spec.js`
- Modify: `playwright.config.js:25` (`use`)

**Step 1: Seed the Fields view for the existing suite.** In `playwright.config.js` change `use: { baseURL },` to:

```js
  use: {
    baseURL,
    // The inspector defaults to the Manifest view; the 28 specs that drive the
    // Required/Set/All field list get that view seeded here instead of each
    // clicking the toggle. cf468 opts out with an empty storageState.
    storageState: { cookies: [], origins: [{ origin: baseURL, localStorage: [{ name: 'cf-insp-view', value: 'fields' }] }] },
  },
```

**Step 2: Write the failing test**

```js
const { test, expect } = require('@playwright/test');
const { resetDoc, ENGINE, dropKind } = require('./helpers');

test.use({ storageState: { cookies: [], origins: [] } });

test.beforeEach(async ({ request }) => {
  await resetDoc(request);
});

async function resourceNamed(request, name) {
  const doc = await (await request.get(ENGINE + '/api/blueprint')).json();
  return (doc.spec.resources || []).find(r => r.name === name) || null;
}

test.describe('CF-468 — manifest editor, view toggle and search', () => {
  test('Manifest view is the default and nests the starter', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await expect(page.locator('#vseg button[data-view="manifest"]')).toHaveAttribute('aria-pressed', 'true');
    const view = page.locator('#insp pre.manifest-view');
    await expect(view).toBeVisible();
    await expect(view).toContainText('containers:');
    await expect(view).toContainText('image: nginx:1.27');
    await expect(page.locator('#fseg')).toBeHidden();
    await expect(page.locator('#insp .fld')).toHaveCount(0);
  });

  test('edit, apply and add a second ports item', async ({ page, request }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    await expect(ta).toBeVisible();
    let text = await ta.inputValue();
    text = text.replace('replicas: 2', 'replicas: 3').replace('- containerPort: 80\n', '- containerPort: 80\n            - containerPort: 9090\n');
    await ta.fill(text);
    await page.click('#insp button[data-manifest-apply]');
    await expect(ta).toBeHidden();
    await expect.poll(async () => {
      const r = await resourceNamed(request, 'deployment');
      const f = r.fields;
      return { rep: f['spec.replicas'] && f['spec.replicas'].value, p1: f['spec.template.spec.containers[0].ports[1].containerPort'] && f['spec.template.spec.containers[0].ports[1].containerPort'].value };
    }).toEqual({ rep: '3', p1: '9090' });
  });

  test('an unknown key keeps the editor open and names the line', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#insp button[data-manifest-edit]');
    const ta = page.locator('#insp textarea[data-manifest-editor]');
    await ta.fill('spec:\n  replicas: 2\n  replicaz: 4\n');
    await page.click('#insp button[data-manifest-apply]');
    await expect(ta).toBeVisible();
    await expect(page.locator('#insp .manifest-err')).toContainText('line 3');
    await expect(page.locator('#insp .manifest-err')).toContainText('spec.replicaz');
  });

  test('Fields toggle restores the old list and search filters it', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.click('#vseg button[data-view="fields"]');
    await expect(page.locator('#fseg')).toBeVisible();
    await page.click('#fseg button[data-f="all"]');
    await expect.poll(() => page.locator('#insp .fld').count()).toBeGreaterThan(100);
    await page.fill('#insp-search', 'imagePullPolicy');
    await expect.poll(() => page.locator('#insp .fld').count()).toBeLessThan(10);
    await expect(page.locator('#insp .fld').first()).toContainText('imagePullPolicy');
  });

  test('search in Manifest view lists schema hits and a click opens Fields view filtered', async ({ page }) => {
    await page.goto('/');
    await dropKind(page, 'Deployment', 'apps/v1', 400, 300);
    await page.click('.node[data-id="deployment"] .node-h');
    await page.fill('#insp-search', 'terminationGracePeriod');
    const hit = page.locator('#insp .search-hit').first();
    await expect(hit).toContainText('terminationGracePeriodSeconds');
    await hit.click();
    await expect(page.locator('#vseg button[data-view="fields"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#insp .fld').first()).toContainText('terminationGracePeriodSeconds');
  });
});
```

**Step 3: Run to verify failure**

```bash
npx playwright test tests/cf468-manifest-editor.spec.js
```

Expected: FAIL (`#vseg` missing).

### Task 12: API client, store, and error detail

**Files:**
- Modify: `web-proto/js/api.js:119-135` (attach `detail`), append two functions
- Modify: `web-proto/js/store.js` (`_paramOp` error, new method)

**Step 1: `api.js`** — where `request` builds the error for `!res.ok`, after `err.status = res.status` add `err.detail = data;` (the parsed JSON body, so `err.detail.line` / `.path` reach the UI). Append:

```js
/** GET /api/blueprint/resources/{name}/manifest → {yaml} */
export function getResourceManifest(name) {
  return request("GET", "/api/blueprint/resources/" + encodeURIComponent(name) + "/manifest");
}

/** PUT /api/blueprint/resources/{name}/manifest — replaces fields in full; returns the doc. */
export function putResourceManifest(name, yamlText) {
  return request("PUT", "/api/blueprint/resources/" + encodeURIComponent(name) + "/manifest", { yaml: yamlText });
}
```

**Step 2: `store.js`** — read `_paramOp` (around line 280) and make the emitted error carry `detail: e.detail`. Add next to `importBlueprint`:

```js
  /**
   * PUT /api/blueprint/resources/{name}/manifest — one undo step; the
   * server's {error,path,line} reaches the "error" topic as `detail`.
   * @param {string} name
   * @param {string} yamlText
   * @returns {Promise<Blueprint|null>}
   */
  async setResourceManifest(name, yamlText) {
    return this._paramOp("setResourceManifest", function () { return api.putResourceManifest(name, yamlText); });
  },
```

If `types.js` declares the store or ApiError shape for `tsc`, add `detail?: any` there so `npm run typecheck` passes.

### Task 13: Header toggle and search markup, `highlight` shared

**Files:**
- Modify: `web-proto/index.html:79-88`
- Modify: `web-proto/js/regions/output.js:296-312` (move `highlight` to utils), `web-proto/js/utils.js`

**Step 1: `index.html`** — inside `.pane-h` of `#region-inspector`, before `<div class="seg" id="fseg">`:

```html
        <input class="tin" id="insp-search" placeholder="search fields…" aria-label="Search fields" style="flex:0 1 140px;height:22px;font-size:11px">
        <div class="seg" id="vseg" title="Manifest: YAML editor · Fields: schema list">
          <button data-view="manifest" aria-pressed="true">Manifest</button>
          <button data-view="fields" aria-pressed="false">Fields</button>
        </div>
```

**Step 2: Move `highlight`** to `utils.js` as `export function highlight(text)` (it needs `esc`; import it from `./dom.js`). In `output.js` delete the local function and `import { highlight } from "../utils.js";`.

### Task 14: Inspector view state, manifest block, search

**Files:**
- Create: `web-proto/js/regions/inspector/manifest.js`
- Modify: `web-proto/js/regions/inspector.js` (state, `renderResource`, `init`), `web-proto/js/regions/inspector/state.js`, `web-proto/js/regions/inspector/events.js`

**Step 1: State.** In `state.js` add `view: "manifest", search: "", manifestDraft: null` with comments. In `inspector.js` add module vars `var view = readView(); var search = ""; var manifestDraft = null;` where

```js
function readView() {
  try { return localStorage.getItem("cf-insp-view") === "fields" ? "fields" : "manifest"; } catch (_) { return "manifest"; }
}
```

and expose them through the `Object.defineProperties(state, …)` block like the others.

**Step 2: `manifest.js`**

```js
/** Manifest-style editor for the selected resource's fields (CF-468). */
import { esc } from "../../dom.js";
import { highlight } from "../../utils.js";
import { state } from "./state.js";
import { buildSnippets } from "./preview.js";

export function manifestHtml(res, yamlText, loadErr, params, otherResources, otherStatusMap) {
  var draft = state.manifestDraft && state.manifestDraft.res === res.name ? state.manifestDraft : null;
  var h = '<div class="insp-sec manifest-sec" style="margin-top:12px;border-top:1px solid var(--rule)">' +
    '<div style="display:flex;align-items:center;gap:8px;padding:8px 12px 4px">' +
    '<span style="font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:0.5px">Manifest</span>' +
    '<span class="dg" style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis">' +
    (res.provider === "k8s" ? "the composed object" : "spec.forProvider of " + esc(res.kind)) + '</span>' +
    (draft ? '' : '<button class="btn sm" data-manifest-edit title="Edit as YAML — applied through the same schema gate as PUT; invalid YAML never lands">edit</button>') +
    '</div>';
  if (loadErr) return h + '<div class="warnbar">' + esc(loadErr) + '</div></div>';
  if (!draft) {
    return h + '<pre class="manifest-view" style="margin:0;padding:6px 12px 10px;font-family:var(--mono);font-size:11px;line-height:1.45;white-space:pre;overflow-x:auto">' +
      highlight(yamlText || "{}") + '</pre></div>';
  }
  var groups = buildSnippets(res, "", params, otherResources, otherStatusMap, state.store.state.doc);
  h += '<textarea class="manifest-editor" data-manifest-editor spellcheck="false" style="display:block;width:100%;box-sizing:border-box;min-height:220px;resize:vertical;font-family:var(--mono);font-size:11px;line-height:1.45;padding:6px 12px;border:0;border-top:1px solid var(--rule);background:var(--sunk);color:inherit;outline:none">' + esc(draft.text) + '</textarea>' +
    '<div class="manifest-bar" style="display:flex;gap:6px;align-items:center;padding:6px 12px;border-top:1px solid var(--rule)">' +
    '<button class="btn sm pri" data-manifest-apply>Apply</button>' +
    '<button class="btn sm" data-manifest-cancel>Cancel</button>' +
    '<select class="tsel" data-manifest-snippet aria-label="Insert snippet" style="flex:1;min-width:0"><option value="">+ insert wire…</option>';
  groups.forEach(function (g) {
    h += '<optgroup label="' + esc(g.label) + '">';
    g.items.forEach(function (it) {
      var from = /^\{\{\s*\$spec\.([\w.]+)\s*\}\}$/.exec(it.snippet);
      var val = from ? "{from: params." + from[1] + "}" : (it.label.indexOf("resources.") === 0 ? "{from: " + it.label + "}" : "{raw: '" + it.snippet + "'}");
      h += '<option value="' + esc(val) + '">' + esc(it.label) + '</option>';
    });
    h += '</optgroup>';
  });
  h += '</select><span class="dg">⌘⏎ apply · esc cancel</span></div>' +
    '<div class="manifest-err warnbar"' + (draft.err ? '' : ' hidden') + '>' + esc(draft.err || "") + '</div></div>';
  return h;
}

export function openManifestEditor(res, yamlText) {
  state.manifestDraft = { res: res.name, text: yamlText || "", err: null };
  state.render();
}

export function closeManifestEditor() {
  state.manifestDraft = null;
  state.render();
}

export function applyManifest(box) {
  var draft = state.manifestDraft;
  var ta = box.querySelector("textarea[data-manifest-editor]");
  if (!draft || !ta) return;
  draft.text = ta.value;
  var detail = null;
  var un = state.store.subscribe("error", function (e) { detail = e; });
  state.store.setResourceManifest(draft.res, draft.text).then(function (doc) {
    un();
    if (doc) { state.manifestDraft = null; state.render(); return; }
    var msg = (detail && detail.message) || "apply failed";
    draft.err = msg;
    state.render();
    var line = detail && detail.detail && detail.detail.line;
    var ta2 = box.querySelector("textarea[data-manifest-editor]");
    if (ta2 && line) selectLine(ta2, line);
  });
}

function selectLine(ta, line) {
  var lines = ta.value.split("\n");
  var start = 0;
  for (var i = 0; i < line - 1 && i < lines.length; i++) start += lines[i].length + 1;
  var end = start + ((lines[line - 1] || "").length);
  ta.focus();
  ta.setSelectionRange(start, end);
}

/** Tab inserts two spaces; ⌘/Ctrl+Enter applies; Escape cancels. */
export function onManifestKeydown(e, box) {
  var t = e.target;
  if (!t || !t.matches || !t.matches("textarea[data-manifest-editor]")) return false;
  if (e.key === "Tab") {
    e.preventDefault();
    var s = t.selectionStart, en = t.selectionEnd;
    t.value = t.value.slice(0, s) + "  " + t.value.slice(en);
    t.selectionStart = t.selectionEnd = s + 2;
    return true;
  }
  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); applyManifest(box); return true; }
  if (e.key === "Escape") { e.preventDefault(); closeManifestEditor(); return true; }
  return false;
}
```

**Step 3: Events.** In `events.js` import `{ openManifestEditor, closeManifestEditor, applyManifest, onManifestKeydown }` and add click actions:

```js
  { selector: "[data-manifest-edit]", needsDoc: true, run: function () { var r = state.selectedResource(); if (r) openManifestEditor(r, state.manifestYAML); } },
  { selector: "[data-manifest-apply]", needsDoc: true, run: function (btn) { applyManifest(btn.closest("#insp") || state.box); } },
  { selector: "[data-manifest-cancel]", needsDoc: true, run: function () { closeManifestEditor(); } },
  { selector: "[data-search-hit]", needsDoc: false, run: function (el) { setView("fields"); } },
```

In `bindInspectorEvents`'s keydown listener, first line: `if (onManifestKeydown(e, box)) return;`. In `onBoxChange`, handle `select[data-manifest-snippet]`: insert `t.value` at the textarea cursor via `insertSnippetIntoTextarea(box.querySelector("textarea[data-manifest-editor]"), t.value)` then reset `t.value = ""`. Add a `setView(v)` helper (exported from events.js or inspector.js) that sets `state.view`, persists to localStorage, updates `#vseg` aria-pressed, toggles `#fseg` hidden, and renders. Bind `#vseg` clicks and `#insp-search` input (debounced 150 ms: `state.search = value; state.render()`) in `bindInspectorEvents` (extend its signature to `(box, fseg, vseg, searchEl)` and pass them from `init`).

**Step 4: `renderResource` changes** in `inspector.js`:

- After `flds`/`detail` are loaded and before building rows, when `view === "manifest"` and `!manifestDraft`: `var mf = await api.getResourceManifest(res.name).catch(function (e) { return { error: e.message }; }); state.manifestYAML = mf.yaml || "";` (guard `t !== renderToken` afterwards). Keep `state.manifestYAML` updated so the edit button opens with the current text; while a draft is open, skip the fetch.
- Replace the `var body = branchRows + fields.map(...)` block with:

```js
    if (view === "manifest") {
      h += manifestHtml(res, state.manifestYAML, mf && mf.error, params, otherResources, otherStatusMap);
      if (search) h += searchHitsHtml(fields, search);
    } else {
      var q = search.toLowerCase();
      var visible = q ? fields.filter(function (f) {
        return f.path.toLowerCase().indexOf(q) !== -1 || (f.description || "").toLowerCase().indexOf(q) !== -1;
      }) : fields;
      var body = (q ? "" : branchRows) +
        visible.map(function (f) { return fieldRow(res, f, params, otherResources, otherStatusMap, env); }).join("");
      h += body || '<div class="empty">No fields match this filter.</div>';
    }
```

with

```js
function searchHitsHtml(fields, q) {
  var ql = q.toLowerCase();
  var hits = fields.filter(function (f) {
    return f.path.toLowerCase().indexOf(ql) !== -1 || (f.description || "").toLowerCase().indexOf(ql) !== -1;
  }).slice(0, 30);
  if (!hits.length) return '<div class="empty">No schema field matches “' + esc(q) + '”.</div>';
  return '<div class="insp-sec" style="padding:6px 12px"><div class="lbl" style="margin-bottom:4px">Schema matches (' + hits.length + ')</div>' +
    hits.map(function (f) {
      return '<div class="search-hit" data-search-hit="' + esc(f.path) + '" role="button" tabindex="0" style="padding:4px 0;border-bottom:1px solid var(--rule);cursor:pointer">' +
        '<div style="display:flex;gap:6px;align-items:baseline"><code style="font-family:var(--mono);font-size:11px;min-width:0;overflow-wrap:anywhere">' + esc(f.path) + '</code><span class="t" style="color:var(--faint);font-size:9.5px">' + esc(f.type) + '</span></div>' +
        (f.description ? '<div class="fld-d" style="margin:0">' + esc(f.description.slice(0, 140)) + '</div>' : '') + '</div>';
    }).join("") + '</div>';
}
```

- In `init`, after `fseg = root.querySelector("#fseg")`: `vseg = root.querySelector("#vseg"); searchEl = root.querySelector("#insp-search"); setView(view)` so the header reflects the persisted view on load. On `selection` change: `manifestDraft = null` (an open draft belongs to the previous resource) but keep `search`.
- Update `tour.js:52` to: "Select a card and the inspector shows its essentials on top and a manifest-style YAML editor below (Manifest view); switch to Fields for the full schema list — Required / Set / All — with search in both views. Val / Wire / Raw toggles switch a field between literal value, wire and raw template."

**Step 5: Run**

```bash
npx playwright test tests/cf468-manifest-editor.spec.js
npm run lint:js && npm run typecheck
```

Expected: PASS. Then the list-driving specs with the seeded view:

```bash
npx playwright test tests/slice1-core-loop.spec.js tests/slice44-map-entry-wires.spec.js tests/cf146-inspector-mode-buttons-clipped.spec.js tests/cf153-wire-select-keystroke.spec.js
```

Expected: PASS (they start in Fields view via storageState).

**Step 6: Commit**

```bash
git add web-proto/index.html web-proto/js/api.js web-proto/js/store.js web-proto/js/types.js web-proto/js/utils.js web-proto/js/regions/output.js web-proto/js/regions/inspector.js web-proto/js/regions/inspector/state.js web-proto/js/regions/inspector/events.js web-proto/js/regions/inspector/manifest.js web-proto/js/tour.js playwright.config.js tests/cf468-manifest-editor.spec.js
git commit -m "feat(inspector): manifest editor, Manifest/Fields toggle and field search (CF-468)"
```

### Task 15: Docs

**Files:**
- Modify: `docs/dsl.md` (after "### Map-Entry Bracket Grammar"), `docs/guide.md` (inspector section), `web-proto/README.md` (module table), `docs/superpowers/specs/2026-09-12-prefilled-fields-design.md` (status line)

**Step 1:** In `docs/dsl.md` add:

```markdown
### Manifest view of a resource

The canvas inspector and the `get_resource_manifest` / `set_resource_manifest` tools show a
resource's `fields` as nested manifest-shaped YAML. A plain scalar is a `value:`; a mapping
whose only key is `value`, `from`, `raw` or `template` is that wrapper; everything else
recurses. The kind's schema decides the grammar: a map node's children become `[key]`
entries, an array-of-objects' items become `[i]`, object members use dots. Arrays of scalars
read as a flow list (`command: [sh, -c]`) and store as a comma-separated `value:`. A whole
object or map set verbatim is always written explicitly as `{raw: "..."}`. A genuine literal
map that happens to be exactly `{from: x}` is written `{from: {value: x}}`.
```

**Step 2:** `docs/guide.md`: describe Essentials, expose-as-parameter, the env repeater, the Manifest / Fields toggle and search in the inspector section. `web-proto/README.md`: add `js/profiles.js` and `js/regions/inspector/manifest.js` to the module table. Set the design doc status to "implemented on branch prefill-fields".

**Step 3: Commit**

```bash
git add docs/dsl.md docs/guide.md web-proto/README.md docs/superpowers/specs/2026-09-12-prefilled-fields-design.md
git commit -m "docs: manifest view grammar, essentials form and inspector views (CF-468)"
```

### Task 16: Full gates

```bash
make lint
make test-race
make test-e2e
```

Expected: all green. `make test-e2e` runs the whole Playwright suite (176 specs + 3 new) against the worktree's own port; if `slice59-select-delete-wire` or `slice60-drag-to-card-picker` fail on canvas drag, rerun those two once before investigating (documented CI flake). Any other failure is a regression to fix before pushing.

Then push the branch only (Kaur lands via `land.sh` or asks for "push to main"):

```bash
git push -u origin prefill-fields
```

Report: the three commits per slice, gate results verbatim, and the diff result from Task 3 Step 4.
