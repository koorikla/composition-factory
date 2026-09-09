# UX run j2 — XPostgres (RDS Instance + native Secret) on the canvas, cf v0.10.0

Engine: `bin/cf serve --addr 127.0.0.1:19120` (blank start, HEAD binary, not rebuilt). Browser driven by a **scripted Playwright driver** (chromium headless, 1440x900) — `$S/driver.js`, commands sent one step at a time so each step could be looked at before the next; screenshots in `$S/shots/` (98 files, numbered in order). Console errors, `pageerror` events, failed responses and dialogs were collected on every step. Because the driver is scripted, hesitation is inferred from attempt counts, so the P2 findings are weaker than they would be from a live run. **No uncaught page error occurred in the whole session** (the only console entries are `[API ERROR]` warnings for 4xx responses, listed under the findings that caused them). `S` below = `/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/526c8b42-4bc5-470e-8d9d-150d5dea6a73/scratchpad/journeys/j2`.

## Mission status per phase

| Phase | Status | Notes |
|---|---|---|
| Add provider-aws-rds from SOURCES catalogue ("rds") | completed | 1.4 s (cache hit), digest + "44 kinds" shown at once |
| Drag Instance + Secret from KINDS | completed | first drag landed on the namespaced `rds.aws.m.upbound.io` Instance |
| Parameters region / dbName / instanceClass | completed | `providerName` cannot be renamed or removed (F8), so the three were added next to it |
| Wire params to Instance fields | completed | port-to-port for `region`; card-header drop + picker for `instanceClass`/`dbName` |
| Status wire RDS `address` -> Secret `stringData[host]` | completed | drag output dot -> Secret row |
| **Status wire RDS `address` -> XR status** | **blocked** | no gesture, menu or inspector control exists (F2); a hand-written `xrd.status` block in the Edit tab is silently dropped (F5) |
| Validate until green | completed | first green at **11.0 min** wall-clock from first open (`shots/01` 17:32:51 -> `shots/55` 17:43:53; includes driver latency) |
| Generate | completed | confirm dialog, "written · 4 files", files named after the stale plural `xapps.…` (F3) |
| Round-trip (Import generated composition) | completed with loss | resources 2/2, wires 4/4, envelope value kept; `required` flags on 3 params lost and pipeline changed (F1) |
| Undo/redo after example load | completed | undo -> redo -> undo, server blueprint byte-identical to the pre-load snapshot |
| Edit-blueprint tab: hand-edit sources, save | completed with workaround | sources edit applied, but the floated editor is unusable (F4), had to stay in the 80 px docked editor |
| Remove a provider that is in use | completed | refused with a clear inline message; control is hidden ~1300 px down (F9) |
| Switch starter examples twice + SOURCES vs `/api/providers` | completed | known stale-list issue confirmed (below) |

## Known issue — confirm/refute

**Confirmed.** After loading "Cloud-Agnostic Web Workload" the SOURCES tab still listed `provider-aws-rds:v2.7.0 · sha256:16c8fb42f66c · 44 kinds` while `curl /api/providers` returned `{"providers":[]}` (`shots/86`); after loading "AWS IRSA" it listed rds while the doc used provider-aws-iam (`shots/61`). It is also stale after an Edit-blueprint Apply that added `provider-aws-s3` (API listed s3+rds, tab showed rds only, `shots/70`). Not re-filed.

## Narrative

The first open put a "Choose an Example Blueprint" modal over a blank canvas; I closed it and the layout explained itself: KINDS/SHARED/SOURCES/GUIDE on the left, a default `XApp` XRD card on the canvas, inspector on the right, generated files at the bottom. My first click on the word "SOURCES" hit the hint card in the canvas, not the tab (1 wasted attempt). The catalogue search "rds" found provider-aws-rds v2.7.0; "Add" installed it in 1.4 s with digest and kind count — no waiting, no doubt.

KINDS search "Instance" showed two groups (namespaced `rds.aws.m.upbound.io` and cluster-scoped); the first one matched my Namespaced XRD. Dragging it onto the canvas produced a card with `region*` and an "outputs" list. Renaming the XRD to XPostgres in the inspector worked, but the subtitle under it stayed `xapps.platform.example.org` and I had no way to change it — I only realised at Generate time that every output file is named `xapps.platform.example.org.yaml` (F3).

Parameters were the first real friction. Renaming the pre-existing `providerName` to `region` was reverted with a toast telling me to "run cf serve without --blueprint" — advice for a terminal, shown in a browser (F8). Each parameter I added then produced a red toast *and* a persistent inspector banner `rename parameter: "newParam" is not declared` even though the rename had succeeded (F7); I checked the server three times before trusting the UI.

Wiring was the best part. Dragging the `region` port onto the Instance's `region` port drew the wire and the inspector immediately warned "optional param into required field" — which is exactly why Validate failed the first time, so the hint paid for itself. For fields not on the card, the GUIDE said "drag parameter dots onto resource cards"; my first drop landed on an *output* row and produced a server 400 with a developer-facing message and the header chip stuck on "error" (F6). Dropping on the card header instead opened a "Wire $instanceClass -> instance" picker with a SUGGESTED MATCHES row — one click. The Secret needed a `stringData` key: "+ Add key" (fourth of four identical buttons, far down a 30-field list), then dragging the `address` output dot onto the new `stringData[host]` row gave the green status wire.

The XR status wire never happened. I dropped the `address` dot on the XRD card's rows, on its header (twice, once with fresh coordinates), single-clicked and right-clicked the dot (menu: Duplicate / Rename… / Delete), and clicked the entries under STATUS OUTPUTS in the inspector — nothing, and nothing said "you can't". Three strikes. As a determined user I then loaded the "AWS IRSA" example (advertised "Status Wire") to learn the idiom, read its blueprint in the Edit tab — every status wire there is resource->resource — undid back to my doc (byte-identical), and typed an `xrd.status` block by hand in the Edit tab. Apply accepted the file, applied my other change (a new source), and silently dropped the block (F5). Blocked.

Validate took 3 s and pointed at `line 50 … missing required field "spec.forProvider.region"` with an "Open in Drawer" button; ticking `req` on the three params made it green: "valid · 2 resources". Generate asked to confirm and reported "written · 4 files". Import is a native file chooser (no modal, no preview); feeding it the generated composition replaced the document instantly. Resources and all four wires came back, but the three `required` flags were false again and the pipeline showed "1 CUSTOM" with a console 404 for a kind called AutoReady (F1).

The Edit tab itself: the docked editor is an 80 px-tall textarea for an 80-line document, opened scrolled to the last line. The obvious "⛶" next to it floats the pane into a window whose textarea is 24 px wide — one character per line (F4). Removing the in-use provider took three failed gestures on the SOURCES row before I found "Remove provider" 1300 px below the fold, under the provider's 44-kind list; it asked "Remove … from the cache?" and then correctly refused inline: still referenced by resources "instance" (F9). Hand-deleting the source line in the Edit tab was refused the same way, with a message that says what to do.

## Findings (severity-ordered; each reproduced twice)

### F1 — P1 — Round-tripping the generated Composition loses the parameters' `required` flags and rewrites the pipeline
- **Wrong behaviour:** importing the file that Generate just wrote changes the document: `dbName`, `instanceClass`, `region` come back optional and the inferred `auto-ready` default becomes an explicit custom step, with a console 404.
- **Repro:** Validate (green) -> Generate -> Import -> pick `$S/out/compositions/xapps.platform.example.org.yaml`.
- **Observed (literal):** inspector `req` checkboxes unticked for the three params; `/api/blueprint` `required:false` for `dbName`, `instanceClass`, `region` (was `true`); inspector `PIPELINE (1 CUSTOM)` with a row `auto-ready | after render | function-auto-ready | xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0 | Input YAML (uncached):` (before import: `PIPELINE (DEFAULT) … 2. auto-ready inferred default`); console `[API ERROR] 404 /api/kinds/autoready.fn.crossplane.io%2Fv1alpha1/AutoReady/fields kind not found: autoready.fn.crossplane.io/v1alpha1 AutoReady`. Resources (2), wires (4) and `writeConnectionSecretToRef.name = xpostgres-conn` survived. Header after import: `blueprints/xapps.platform.example.org.cf.yaml`.
- **Screens:** `shots/79-import-after-choose.png`, `shots/80-imported-canvas.png`, `shots/98-import-repro2.png`. Comparison files: `$S/bp-before-example.json` vs `$S/bp-after-import.json`.
- **Attempts:** 2 imports, identical result both times.

### F2 — P1 — There is no way in the UI to expose a composed resource's status field on the XR status
- **Wrong behaviour:** the mission's "RDS address -> XR status" cannot be authored; every plausible gesture is inert and nothing tells the user the feature does not exist here.
- **Repro:** select the Instance; drag the green `address` output dot onto (a) an XRD parameter row, (b) the XRD card header; single-click the dot; right-click the dot; click `status.atProvider.address` under STATUS OUTPUTS in the inspector; click "+ add field" on the XRD card.
- **Observed (literal):** wire path count unchanged (8 -> 8), no picker, no toast; right-click menu shows only `Duplicate | Rename… | Delete`; STATUS OUTPUTS entries are plain labels ("Other resources can wire from this object's status:"); "+ add field" opens a `parameterName [Add] [×]` row (a parameter, not a status field); GUIDE text only says "Drag parameter dots onto resource cards to bind inputs, or status ports to dependent fields."
- **Screens:** `shots/30-drag-address-to-xrd-drop.png`, `shots/44-drag-address-to-xrd-header-drop2.png`, `shots/45-click-address-dot.png`, `shots/46-rightclick-address-dot.png`, `shots/47-click-status-output-entry.png`, `shots/31-xrd-add-field.png`, `shots/93-repro-F4-address-to-xrd.png`.
- **Attempts:** 6 distinct gestures, header drop repeated 3x, plus the Edit-tab workaround in F5. Sub-goal blocked.

### F3 — P1 — Renaming the XRD kind leaves the plural stale and the plural is not editable anywhere
- **Wrong behaviour:** after renaming `XApp` -> `XPostgres` the XRD stays `xapps.platform.example.org`, so the Composition and XRD are named after a kind that no longer exists, and the subtitle showing it is static text.
- **Repro:** click the XRD card -> inspector name field -> type `XPostgres`, Enter -> read the grey subtitle; click the subtitle; Generate and list `$S/out`. Repeat with `XPostgresDB`.
- **Observed (literal):** subtitle `xapps.platform.example.org · v1alpha1` after both renames; clicking it focuses nothing (`document.activeElement` = BODY); `/api/blueprint` `"kind":"XPostgresDB","plural":"xapps"`; generated `out/compositions/xapps.platform.example.org.yaml`, `out/xrds/xapps.platform.example.org.yaml`; composition `metadata.name: xapps.platform.example.org` with `compositeTypeRef.kind: XPostgres`.
- **Screens:** `shots/12-rename-xrd.png`, `shots/34-click-plural-subtitle.png`, `shots/91-repro-F5-rename-kind.png`, `shots/56-generate.png`.
- **Attempts:** 2 renames; 1 click on the subtitle (nothing to try).

### F4 — P1 — "Float editor window" turns the blueprint editor into a 24 px-wide textarea
- **Wrong behaviour:** in edit mode, the only enlarge affordance (`⛶`, title "Float editor window (Move freely)") produces a floating pane whose textarea is 24x80 px — one character per line — so hand-editing is impossible in the mode built for it. Docked, the editor is 80 px tall for an 80-line document and opens scrolled to the end.
- **Repro:** bottom pane -> tab `untitled.cf.yaml` -> `edit` -> click `⛶` at the right of the tab strip.
- **Observed (literal):** textarea bounding box `{w:24, h:80}` floated vs `{w:684, h:80}` docked; the sliver shows `v / a / l / .` stacked; footer note under the editor: `applied through the same gate as an import — invalid YAML never lands`; dock button title `Dock editor (Lock at bottom)`.
- **Screens:** `shots/67-state-check.png`, `shots/74-editor-float-repro2.png`, `shots/58-bp-edit-mode.png` (docked 80 px editor).
- **Attempts:** 2 floats, identical.

### F5 — P1 — Edit-blueprint Apply silently discards unknown keys while applying the rest
- **Wrong behaviour:** a hand-typed `xrd.status:` block vanishes on Apply with no error, no toast and the header still saying "preview", although the same Apply did land a new `sources` entry; the editor's own note promises that invalid input "never lands".
- **Repro:** Edit tab -> add `- provider: ghcr.io/crossplane-contrib/provider-aws-s3:v2.7.0` under `sources:` and `status: {address: {type: string, from: resources.instance.status.atProvider.address}}` under `xrd:` -> Apply -> re-open `edit`.
- **Observed (literal):** header `preview · 4 files`; no toast; `/api/blueprint` `sources` = s3 + rds, `xrd` keys = `group, kind, plural, version, scope, parameters` (no `status`); re-opened editor text has the s3 line and no `status:` block.
- **Screens:** `shots/69-apply-status-and-s3.png`, `shots/70-sources-after-apply.png`.
- **Attempts:** 2 (apply, then re-open to confirm; repeated read of the API).

### F6 — P2 — A parameter can be dropped on a resource card's *output* row; the server rejects it with developer text and the header chip sticks on "error"
- **Wrong behaviour:** output rows (read-only status fields) accept the drop, the PUT fails with a message written for the engine author, and the header chip stays `● error` until the next successful edit.
- **Repro:** drag the `region` dot from the XRD card and release on the `allowMajorVersionUpgrade` row under "outputs" on the Instance card.
- **Observed (literal):** toast `resource "instance": field "status.atProvider.allowMajorVersionUpgrade" is not in Instance spec.forProvider (an unknown field is silently pruned by the API server on apply, so it must be caught here)`; header chip `error` (tooltip repeats the message); console `[API ERROR] 400 /api/blueprint …` same text; chip returned to `preview · 4 files` only after the next successful wire.
- **Screens:** `shots/28-drag-instanceClass-to-body-drop.png`, `shots/92-repro-F3-drop-on-output.png`, `shots/33-hover-error-chip.png`.
- **Attempts:** 2.

### F7 — P2 — Every successful parameter add/rename is followed by a false error toast and a sticky inspector banner
- **Wrong behaviour:** Enter and blur each send a rename; the second one 404s and the UI reports it as an error although the rename succeeded, and the banner stays in the inspector.
- **Repro:** XRD selected -> `+ Add parameter` -> type `region` over `newParam` -> Enter.
- **Observed (literal):** toast `⚠️ rename parameter: "newParam" is not declared ×`; inspector banner `rename parameter: "newParam" is not declared` above the name field; console `[API ERROR] 404 /api/blueprint/parameters/newParam/rename rename parameter: "newParam" is not declared`; `/api/blueprint` shows the new name present. Same double-fire for the `providerName` rename (two identical 400s).
- **Screens:** `shots/14-params-3.png`, `shots/90-repro-F2-add-param.png`.
- **Attempts:** 5 parameters added across the run, toast every time.

### F8 — P2 — `providerName` looks editable but cannot be renamed or deleted, and the refusal tells a browser user to run a CLI command
- **Wrong behaviour:** the name input accepts typing and the `×` is enabled; both actions are reverted with a message about `cf serve` flags.
- **Repro:** XRD selected -> change `providerName` to `region` (or `region2`), Enter; click the `×` on that row.
- **Observed (literal):** toast `rename parameter "providerName" to "region": spec.xrd.parameters.providerName is required for a Namespaced XRD: run cf serve without --blueprint to scaffold one, or add: providerName: {type: string, required: true}`; delete: `delete parameter "providerName": spec.xrd.parameters.providerName is required for a Namespaced XRD: run cf serve without --blueprint …`; console 400 for `POST …/parameters/providerName/rename` (twice per attempt) and `DELETE …/parameters/providerName`.
- **Screens:** `shots/13-add-param-1.png`, `shots/88-repro-F1-rename-providerName.png`, `shots/89-repro-F1-delete-providerName.png`.
- **Attempts:** rename 2, delete 1 (each on a fresh row state); no affordance marks the row as locked.

### F9 — P2 — "Remove provider" is hidden below the provider's full kind list; row hover/click/right-click expose nothing
- **Wrong behaviour:** the only removal control sits ~1300 px down inside the expanded provider entry (after 44 kind rows) in a 900 px viewport; the row itself offers no remove affordance.
- **Repro:** SOURCES -> hover `provider-aws-rds:v2.7.0` (nothing) -> click (expands: ref, digest, "show all kinds", 44 kinds) -> right-click (nothing) -> scroll to the end of the kind list -> `Remove provider` -> confirm.
- **Observed (literal):** button `Remove provider`, title `Remove this provider from the cache`, bounding box y=1331 before scrolling; confirm `Remove ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0 from the cache?`; inline panel in SOURCES `delete provider "ghcr.io/crossplane-contrib/provider-aws-rds:v2.7.0": still referenced by the blueprint's sources and by resources "instance"` (console 409). The refusal itself is correct and well placed; the finding is discoverability and the "cache" wording for what the user thinks of as the blueprint's source list.
- **Screens:** `shots/73-hover-installed-provider.png`, `shots/75-click-installed-provider.png`, `shots/76-rightclick-installed-provider.png`, `shots/94-remove-provider-button.png`, `shots/95-remove-provider-clicked.png`, `shots/97-remove-provider-repro2.png`.
- **Attempts:** 3 gestures before the control was found; removal tried 2x.

### F10 — P3 — SOURCES "k8s" entry reports "· 1 kinds" after an example load and keeps it after undo
- **Wrong behaviour:** the native entry read `k8s · 16 kinds` on a blank doc and `k8s · 1 kinds` after loading "AWS IRSA"; undoing back to my document (verified identical on the server) did not restore 16. Likely the same stale cache as the known issue, listed because it is a different symptom.
- **Repro:** SOURCES (note `k8s · 16 kinds`) -> Examples -> Load "AWS IRSA" -> SOURCES -> Undo -> SOURCES.
- **Observed (literal):** `k8s\n· 1 kinds` in `shots/61`, `shots/62` (after undo), `shots/67`, `shots/70`.
- **Attempts:** 2 observations after two different loads.

### F11 — P3 — Required-field counts disagree across three places for the same kind
- **Wrong behaviour:** KINDS list `Instance 1 req`, hover card `155 fields · 1 required`, inspector `155 leaf fields · 13 required`, while the inspector's Required filter lists one forProvider field (`region`) and one envelope field (`writeConnectionSecretToRef.name REQ`); Validate later passed with the envelope field unset the first time it was flagged.
- **Screens:** `shots/08-kinds-search-instance.png`, `shots/10-drag-instance-drop.png`, `shots/17-instance-selected.png`.
- **Attempts:** visible every time the Instance is selected (2+).

### F12 — P3 — Catalogue entry layout breaks once a provider is installed
- **Wrong behaviour:** in the SOURCES catalogue the installed entry's name truncates to `provider-a…`, its ref wraps onto four lines under the `INSTALLED · 44 KINDS` badge, and the search-term highlight from the earlier "rds" query is painted inside the *installed* list's name (`provider-aws▮rds`).
- **Screens:** `shots/67-state-check.png`, `shots/94-remove-provider-button.png` (catalogue), `shots/77-provider-expanded-full.png` (highlight).
- **Attempts:** 2 screenshots each.

Not filed: a failed drag that starts on a label instead of a port selects page text (`shots/41`) — caused by my own stale coordinates, not reproduced from a clean state; catalogue search "rds" also lists provider-gcp-dns (description match on "records"), acceptable.

## What worked

- Catalogue add: 1.4 s to an installed entry with digest and kind count; the catalogue row flipped to `INSTALLED · 44 KINDS` at once.
- Drag-to-canvas, port-to-port wiring, and the card-header drop with `Wire $param -> resource` picker and SUGGESTED MATCHES — one gesture, one click.
- The `⚠ optional param into required field` hint on the wire, which explained the first Validate failure before it happened.
- Validate: 3 s, error with a line number and an "Open in Drawer" button; green chip `valid · 2 resources`.
- Generate: confirm dialog naming the output directory, `written · 4 files`, file list in the artifacts tree.
- Undo/redo across example loads and imports: server blueprint byte-identical each time (`bp-before-example.json` = `bp-end2.json`).
- The import gate refusing to drop an in-use source, with a message that says what to add and why; the 409 on Remove provider shown inline where the click happened.
- SHARED tab: `$param · N BOUND` fan-out badges.

## Cleanup

Driver process and `cf serve` on 127.0.0.1:19120 stopped; nothing in the repo was touched; `$S` left with `doc.cf.yaml`, `.cf.lock`, `out/`, `shots/`, `bp-*.json`, `driver.js`, `p*.json`.
