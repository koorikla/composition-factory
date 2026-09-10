# UX run 2026-09-11 - M2 The status wire: IRSA

Repo HEAD 6efa086 (as dispatched; the canvas header read `v0.10.0-69-g1234f14-dirty` in session 1 and
`v0.10.0-101-g64120e5` in session 2 with the same `bin/cf` binary - `cf serve` serves `web-proto/` live, so
the shared tree moved under me between sessions). Engine: `bin/cf serve --addr 127.0.0.1:19220`, blank start
(no doc file beforehand). Driver: **throwaway Playwright script** (chromium headless 1440x900), acting by
visible labels/positions only; console errors, pageerrors, dialogs and failed responses collected. A scripted
driver cannot feel hesitation, so the P2 findings below are weaker than a live-pane run would give.
Scratch: `S=/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/526c8b42-4bc5-470e-8d9d-150d5dea6a73/scratchpad/journeys2/m2`
(screenshots in `$S/shots/`, event log `$S/events.log`, per-step results `$S/res/`).

## Mission and outcome

**completed.** Wall-clock from engine start to first green Validate: **~8 min 25 s** (start 22:43:48Z, chip read
`valid · 2 resources` ~22:52:13Z; Validate itself took 2.4 s). Total run incl. round-trip and a second
verification session: 12 min 37 s. Zero uncaught page errors (`PAGEERROR` count 0). One detour was needed
(placeholder annotation value, see F1) but it was a UI path, not the API or the file, so not "with workaround".

Oracle: ServiceAccount annotation `eks.amazonaws.com/role-arn` is bound to `resources.role.status.atProvider.arn`;
the generated template guards it with
`{{- if hasKey (dig "resources" "role" "resource" "status" "atProvider" dict $.observed) "arn" }}`; the Role is
emitted before the ServiceAccount in the template and the ⍋ auto-layout places Role left of ServiceAccount;
Validate green (`$S/out/compositions/xapps.platform.example.org.yaml`, shots 18, 27).

## Narrative

Arrival on a blank document put a modal "STARTER BLUEPRINTS - Choose an Example Blueprint" in front of me, and the
first card was literally "AWS IRSA (IAM Role + EKS ServiceAccount)" (shot 01). That is the mission. A person would
click it; I closed it to do the build by hand, but note that the starter exists and is the first thing shown.
Closing it took me two goes only because my first "×" locator hit a parameter-delete button behind the modal - the
modal's own × at top-right worked (shot 02).

I went looking for a provider. The canvas hint card said "2. ADD CLOUD PROVIDERS IN SOURCES", and my first click on
the word SOURCES landed on that hint text, not the tab; the tab itself at the top of the left rail worked (shots
03, 04). SOURCES has a "Search OSS providers…" box; typing `iam` put `provider-aws-iam` first, followed by eight
GCP providers that do not contain "iam" (shot 05). The Add button sits *under* the provider name, not beside it.
Add completed in about a second (cache was warm) and a toast said "Installed provider-aws-iam:v2.7.0 (schemas
loaded) — Open KINDS" (shot 06). Good feedback; on a cold cache I could not judge whether it stays informative.

KINDS search `role` showed *two* IAM `Role`s: one under `IAM.AWS.M.UPBOUND.IO`, one under
`IAM.AWS.UPBOUND.IO · CLUSTER-SCOPED` (shot 08). Nothing says what `.M.` means; I guessed "namespaced, matches my
Namespaced XApp" and dragged the first. The drop worked first time and the card immediately showed an
**outputs** list with `arn` on top (shot 10). That is how I learned a status field can be a source - the card
told me before I asked. The Role inspector header said "19 leaf fields · 1 required" while the Required filter
below it said "No fields match this filter" and the KINDS row said "0 req" (shot 10). Then I dragged
`ServiceAccount` on; its card was empty apart from `v1` - no fields, nothing to drop onto (shot 12).

The ServiceAccount inspector had "ANNOTATIONS" with `prefix/name`, `value` and Add. I typed the key, left value
blank because the value was going to come from the wire, and pressed Add. A red toast and an inspector banner
replied `resource "service-account" annotation "eks.amazonaws.com/role-arn": set exactly one of from, value, raw or
template (got 0)`, and both inputs were emptied (shot 13). I did not know what "from, value, raw or template" meant
yet; I retyped the key with the value `placeholder` and the annotation appeared as a row on the card
(`…amazonaws.com/role-arn · value`, shot 14). Dragging from the green dot beside `arn` to that row drew a straight
green wire on the first attempt; the inspector row changed to `EKS.AMAZONAWS.COM/ROLE-ARN ← resources.role.status…`
(shot 16). The drawer preview already showed the `hasKey` guard - I never asked for it. Validate: 2.4 s to
`valid · 2 resources` (shot 18).

Generate asked via a native `confirm()` ("Generate will write manifests to disk in '…/out', overwriting existing
files. Proceed?") and wrote 4 files (shot 22). Import opened a bare OS file chooser (no dialog, no hint about
what it accepts; shot 23). Picking the generated Composition replaced the whole document with no confirmation and
no toast - the only visible changes were the header filename and "PIPELINE (1 CUSTOM)" (shots 24, 25). Validate
stayed green (1.5 s). I later discovered by accident that ↩ undoes the import (shot 29). ⍋ laid the three cards
out XApp → Role → ServiceAccount (shot 27).

## Round-trip: document before vs after Import

Resources, wire (`annotations.eks.amazonaws.com/role-arn.from: resources.role.status.atProvider.arn`), sources,
XRD group/kind/plural/scope/version and the parameter's `type: string` / `required: true` all came back identical.
Every difference (`diff -u $S/doc-before.yaml $S/doc-after.yaml`, identical on the second import):

1. `metadata.name`: `untitled` → `xapps.platform.example.org` (expected).
2. `spec.pipeline` gained an explicit step that did not exist before:
   `- {name: auto-ready, functionRef: function-auto-ready, package: xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0, position: after}`;
   inspector went from "PIPELINE (DEFAULT) … 2. auto-ready inferred default" to "PIPELINE (1 CUSTOM)" with an
   editable step and an "Input YAML (uncached): apiVersion: … kind: …" box.
3. `spec.xrd.parameters.providerName.description`: `ProviderConfig to reconcile the composed resources against.`
   → `Crossplane ProviderConfig name to use for managed resources`.

Re-generating from the imported document gives a Composition byte-identical to the imported one except the
`# Source:` comment (`diff $S/out/compositions/xapps.platform.example.org.yaml $S/composition-after-import.yaml`).

## Findings (severity-ordered)

**F1 · P2 · Annotation form accepts an empty value, then fails with engine jargon and discards the key.**
Goal: add an annotation whose value will come from a wire. Observed: Add with empty `value` → toast + inspector
banner `resource "service-account" annotation "eks.amazonaws.com/role-arn": set exactly one of from, value, raw or
template (got 0)`; server `PUT /api/blueprint` 400; both inputs cleared. The only way forward is to invent a
placeholder value, which nothing suggests. The toast is also sticky - still on screen ~2 min later across Generate
(shot 22) until its × is clicked. Repro: select ServiceAccount → ANNOTATIONS → type `prefix/name` → Add.
Attempts: 2 to get an annotation to exist. Reproduced twice (shots 13, 19; events 22:50:54Z, 22:52:41Z). Report only.

**F2 · P2 · Import replaces the document silently and names nothing it changed.**
Goal: re-import my own exported Composition. Observed: OS file chooser only; on pick, no confirmation (the starter
chooser says "replaces current blueprint · undoable" - Import says nothing), no toast, no summary; the pipeline was
materialized and the parameter description rewritten without being named (see Round-trip). Undo works but is
undiscoverable as an Import escape hatch. Repro: Import → pick
`$S/out/compositions/xapps.platform.example.org.yaml`. Attempts: 1. Reproduced twice (shots 24/25, 03/04-session2;
diffs above). Report only. Overlaps CF-119 (pipeline half) - not a re-file, the new part is the zero feedback.

**F3 · P2 · Console errors after Import: `404 /api/kinds/autoready.fn.crossplane.io%2Fv1alpha1/AutoReady/fields`.**
Observed: two `Failed to load resource: 404` + `[API ERROR] 404 … kind not found: autoready.fn.crossplane.io/v1alpha1
AutoReady` per import/refresh, from the materialized auto-ready step trying to load input fields; the inspector
shows a placeholder "Input YAML (uncached)". Not an uncaught page error, so not P0. Reproduced three times
(22:53:27Z, 22:54:37Z, 22:56:19Z). Report only.

**F4 · P2 · Required counts disagree (= CF-127, confirm).** Role: header "1 required", Required filter "No fields
match this filter", KINDS row "0 req" (shot 10). ServiceAccount: header "4 required", same empty filter (shots 12,
16). Reproduced twice (two kinds). Not re-filed.

**F5 · P3 · Two `Role` kinds with no explanation of the difference.** `IAM.AWS.M.UPBOUND.IO` vs
`IAM.AWS.UPBOUND.IO · CLUSTER-SCOPED`; nothing says `.m.` = namespaced/v2 or which one a Namespaced XApp wants.
Reproduced twice (shot 08; session 2 kinds search returned the same two groups). Report only.

**F6 · P3 · The status wire's binding is not readable once drawn.** Card row truncates to
`…amazonaws.com/role-arn`; inspector shows `EKS.AMAZONAWS.COM/R… ARN ← resources.role.status….`; the full
source→target pair is not displayed anywhere untruncated. Reproduced twice (shots 16, 27). Report only.

**F7 · P3 · Catalogue search `iam` lists 8 GCP providers without "iam" in the name** (artifact, cloudfunctions,
cloudrun, kms, pubsub, secretmanager, storage, + gcp-iam) after the one real hit (shot 05). Observed once - my
second attempt typed into the `ghcr.io/…/provider-x:vN` box after the catalogue box moved down (shot 02-session2).
Report only.

Not filed: the empty `⚠️ Open in Drawer ×` toast template visible only in the DOM (no user impact); native
`confirm()` on Generate (works, just unstyled).

## Already filed - confirm / refute

- **CF-119** (import loses `required`, rewrites pipeline): pipeline half **confirmed** (inferred auto-ready became a
  pinned custom step, both imports); `required: true` on `providerName` **survived** here - not reproduced with a
  single well-known parameter.
- **CF-108** (adopt without XRD retypes parameters): **not reproduced** - sole parameter came back `string`,
  required; only its description was rewritten (see Round-trip item 3).
- **CF-130** (no GUI for `spec.environment`): **consistent** - no environment control anywhere in the inspector;
  only "+ function-environment-configs" in the pipeline step picker.
- **CF-127** (required counts disagree): **confirmed** (F4).

## Recently fixed - confirm / refute

- **CF-123** (parameter dropped on an output row refused cleanly, no stuck chip): **confirmed fixed** - dropping
  `$providerName` on the Role's `arn` output row raised no error and left no chip; it opened a "Wire $providerName →
  role" field picker instead (shot 21).
- **CF-124** (no false toast after rename): **not confirmable** - triple-click on `providerName`, type
  `providerCfg`, Tab left the name unchanged, `req` box greyed, no toast of any kind (shot 28); the built-in
  parameter appears locked, so the rename path was never exercised.
- **CF-126** (Remove provider discoverable): **confirmed** - an `×` sits on the `provider-aws-iam:v2.7.0` row under
  SOURCES › INSTALLED PROVIDERS (shot 07). Not clicked.

## What worked

- The Role card shows an **outputs** section with `arn` the moment it lands - status-as-source is discoverable
  without docs (shot 10).
- The wire from an output port to an annotation row took on the first drag, and the `hasKey` guard appeared in the
  preview unasked (shots 16, 18).
- Validate: 2.4 s / 1.5 s to a green `valid · N resources` chip; Generate reports "Output written to …" with a
  `kubectl apply` line.
- Provider Add gives an explicit "Installed … (schemas loaded) — Open KINDS" toast; the `.cf.lock` digest is shown
  on the installed row.
- ⍋ auto-layout orders Role before ServiceAccount; ↩ undoes an Import.
- The starter chooser offers an "AWS IRSA" blueprint first - the mission's answer is one click away for anyone
  who takes the modal at its word.

## Cleanup

Engine stopped; port 19220 free. Nothing written inside the repo (one read-only `git rev-parse` at preflight to
record HEAD). No spec files were written per the dispatcher's override; F1-F3 have deterministic repros suitable
for `tests/sliceNN-*.spec.js` if wanted.
