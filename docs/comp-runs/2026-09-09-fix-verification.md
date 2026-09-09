# j4 — re-verification of backlog-archive ticks dated 2026-09-08/09

HEAD verified: f45c2a8 (== origin/main). Scope: every item in docs/backlog-archive.md marked "— completed 2026-09-09" (no 2026-09-08 entries exist): 29 items, CF-050/055/057/059/060/062–085.
Method: detached worktree of origin/main, go build -o ./cf-j4 ./cmd/cf, own cf serve on 127.0.0.1:19141 with scratch blueprint/out/lock, real crossplane render with Docker function images, the full Playwright suite on 19140 (own server), and a custom spec (tests/j4-verify.spec.js, worktree only) against 19141. Shared checkout untouched; no git add/commit/push.

Full Playwright suite at HEAD: 224 passed, 1 skipped (slice16, pre-existing), 0 failed (1.6 m).
CF-081 acceptance test (real images): ok … 7.744s.

Result: 25 GENUINE, 4 HALF, 0 REGRESSED, 0 UNVERIFIABLE.
Most likely candidates for "the bug that was fixed is still not": CF-059 (a mistyped provider ref shows a raw Go error wrapped in the wrong advice "backend may be restarting"), CF-055 (Validate still blames the field, never the optional parameter), CF-080 (python engine renders PORT: "8080.0").

## Table

| id | verdict | one-line reason | test that guards it |
|---|---|---|---|
| CF-050 | GENUINE | Real Tab traversal reaches a palette row; Enter places + selects; Space on a focused card selects it | tests/slice85-keyboard-touch-placement.spec.js |
| CF-055 | HALF | Picker badge / inspector / port warning exist and pass slice60, but Validate on a blueprint already in that state still says `missing required field "spec.forProvider.region"` and never names the optional parameter or the promote action (reproduced twice) | tests/slice60-drag-to-card-picker.spec.js:89,:149 (UI warning only) |
| CF-057 | GENUINE | Tooltip names .j4srv/out + "overwrites"; dismissing the confirm writes 0 files; accepting writes compositions/functions.yaml/providerconfigs/xrds | tests/slice84-generate-overwrite-confirmation.spec.js |
| CF-059 | HALF | The four stated conversions work (slice86 green), but a mistyped provider ref (server answers 502 for a registry pull failure) shows the raw Go error verbatim AND the new wrapper adds false advice "backend server may be restarting or unreachable" (reproduced twice) | tests/slice86-friendly-error-diagnostics.spec.js (does not cover this path) |
| CF-060 | GENUINE | dark --faint #8E99A8: 5.99 / 5.54 / 6.61 on surface / surface-2 / ground; .btn.pri 7.09 dark, 6.76 light | tests/slice67-theme-native-controls.spec.js:88,:110 |
| CF-062 | GENUINE | Drawer label "Generated"; tour says "The Generated drawer" and "Val / Wire / Raw toggles" = inspector buttons Val/Wire/Raw; guide: Reset View ⌂ | none (copy only) |
| CF-063 | GENUINE | Palette 220 px at 1280×720; every row title `<kind> · <apiVersion> (<provider>)` e.g. `HorizontalPodAutoscaler · autoscaling/v2 (k8s)` | tests/slice87-palette-kind-truncation.spec.js |
| CF-064 | GENUINE | At 1280×720: drawer 200 px, canvas 474 px, palette 474 px, code viewport 118 px | tests/slice91-output-drawer-sizing.spec.js |
| CF-065 | GENUINE | Click on a palette row adds a card (nodes 4→5) and selects it | tests/slice85-keyboard-touch-placement.spec.js:29 |
| CF-066 | GENUINE | "validating…" → "valid · 2 resources"; unchanged after 4 s idle and after a forced background store.generate(false) | tests/slice92-validate-vocabulary.spec.js |
| CF-067 | GENUINE | Light syntax on --sunk #D8E0EA: .k 5.08, .st 4.77, .tm 5.32, .co 4.81, .sh 5.28 (all ≥ 4.5) | tests/slice67-theme-native-controls.spec.js:179 |
| CF-068 | GENUINE | .wire-hit stroke 14 px, pointer-events: stroke; a click 5 px off the stroke selects the wire; .d dots 7×7 with ::before 18×18 | tests/slice88-wire-hit-targets.spec.js |
| CF-069 | GENUINE | #valid has role=status aria-live=polite tabindex=0; Enter on it expands the collapsed drawer; aria-label "Validation succeeded: 2 resources rendered" | tests/slice89-validate-generate-announcements.spec.js |
| CF-070 | GENUINE (note) | Distinct in both themes, all ≥ 6.5:1; the archive's hex values (#B45309/#826E00) are stale — CF-067 (dd6925f) later re-derived light to #92400E/#685800; prototype in sync | tests/slice67-theme-native-controls.spec.js:135,:163 |
| CF-071 | GENUINE | No #7c3aed in proto.css / canvas-prototype.html / examples.go; --dim --accent --pri --panel --fg resolve in both themes | tests/slice67-…:209,:228; internal/examples/examples_test.go:TestExampleIconColorDesignTokenHygiene |
| CF-072 | GENUINE | /api/kinds reports exactly 16 native kinds = the copy; empty-state and cf provider add guidance pass slice90 | tests/slice90-interface-copy-polish.spec.js |
| CF-073 | GENUINE | metadata.labels[app]/[tier] survive gen → adopt → gen; the only diff is the "# Source:" comment | internal/adopt/adopt_test.go:TestAdoptComposedResourceMetadataPreservation |
| CF-074 | GENUINE | Observed url: "true" renders endpoint: "true" in the ConfigMap AND in the annotation on go-templating, kcl and python (real crossplane render) | internal/emit/quoting_test.go:TestStatusWireIntoStringFieldQuotingAcrossEngines |
| CF-075 | GENUINE | Adopting the Composition alone (no XRD) yields plural: metalosses for kind MetaLoss | internal/adopt/adopt_test.go:TestAdoptPluralInferenceWithoutXRD |
| CF-076 | GENUINE | targetPort {value: 8080} → --validate ok; {value: "8080"} is emitted as integer 8080; http ok. (cf fields Service still DISPLAYS "string" — metadata is preserved internally) | internal/schema/k8s/k8s_test.go:TestIntOrStringPreservesIntOrStringMetadata; internal/emit/render_validate_test.go:TestValidateRenderedNativeServiceIntOrStringTargetPort |
| CF-077 | GENUINE | cf function add …function-auto-ready:v0.5.0 exits 0, "0 function input schemas of 0 CRDs", pin written to the lock | cmd/cf/function_test.go:TestFunctionAddNoInputCRDsSucceeds |
| CF-078 | GENUINE | dsl.md:49/352/361/378 and cli.md:269 all say xpkg.upbound.io/…:v0.5.0 = internal/emit/pipeline.go:25 | internal/emit/docs_test.go:TestDefaultFunctionsInDocsMatchEmitter |
| CF-079 | GENUINE | cli.md:137/156/159 now say "not pre-granted", cite HPA, and state Deployment/Service do NOT trigger it | internal/emit/docs_test.go:TestRBACDocumentationTrigger |
| CF-080 | HALF | go-templating and kcl render PORT: "8080"; python renders PORT: "8080.0" (reproduced twice) — the API server now accepts it, but the value is wrong (MessageToDict float + str()) | internal/emit/quoting_test.go:TestK8sWorkloadConfigMapPortQuotedAcrossEngines (string-level; does not catch) |
| CF-081 | HALF | The cross-engine deep-diff exists and passes with real images, but its blueprint (testdata/xqueue.cf.yaml) has no integer→string-map field, so the CF-080 class it names slips through — python's "8080.0" vs "8080" is not caught | acceptance_test.go:TestAcceptanceAlternativeEnginesRender |
| CF-082 | GENUINE | PUT omitting the source drops it from GET /api/providers and the palette (0 iam kinds); re-POST → 200, source re-declared in the file, 46 kinds back; 409 now requires cached AND declared | internal/api/providers_test.go:TestAddProviderDroppedFromBlueprintSourcesCanBeReadded; blueprint_test.go:TestPutBlueprintDroppingSourceReconcilesProvidersAndAllowsReadd |
| CF-083 | GENUINE | Drawer collapsed (38 px), 1280×720: #render-warn-banner at top 46 / bottom 80, carrying `line 55: resource "app" (Deployment): field "spec.replicas": invalid type: expected integer, got string "notanumber"`; no hover needed | tests/slice83-render-error-collapsed.spec.js |
| CF-084 | GENUINE | All 7 starters: POST /api/examples/{id}/load 200 then POST /api/render ok:true; all 7 cf gen --validate exit 0 "render validation ok" | none (closing commit changed only YAML; TestAllExamplesAreValidBlueprints calls b.Validate() only, never renders) |
| CF-085 | GENUINE | Handles taken while every face was "loading" (work-queue 218 px → 212 px after fonts landed); all three nodes still connected after re-renders; render() reconciles by data-id, no innerHTML = h; slice33/40 green in the full run | tests/slice33-stable-canvas-dom.spec.js, tests/slice40-interaction-stability.spec.js |

## Evidence

### CF-084 — starters through load + render, and cf gen --validate
```
=== irsa          load http=200   {"ok":true,"resources":3,"error":"","unavailable":""}
=== rds-postgres  load http=200   {"ok":true,"resources":1,"error":"","unavailable":""}
=== k8s-app       load http=200   {"ok":true,"resources":7,"error":"","unavailable":""}
=== k8s-workload  load http=200   {"ok":true,"resources":4,"error":"","unavailable":""}
=== k8s-cronjob   load http=200   {"ok":true,"resources":3,"error":"","unavailable":""}
=== s3-bucket     load http=200   {"ok":true,"resources":3,"error":"","unavailable":""}
=== sqs-queue     load http=200   {"ok":true,"resources":3,"error":"","unavailable":""}
internal/examples/irsa.cf.yaml exit=0 :: render validation ok …
internal/examples/k8s-app.cf.yaml exit=0 :: render validation ok …
internal/examples/k8s-cronjob.cf.yaml exit=0 :: render validation ok … wrote .j4srv/gen/rbac.yaml … warning: composed native Kubernetes kinds require cluster RBAC permissions not pre-granted to Crossplane
internal/examples/k8s-workload.cf.yaml exit=0 :: render validation ok …
internal/examples/rds.cf.yaml exit=0 :: render validation ok …
internal/examples/s3-bucket.cf.yaml exit=0 :: render validation ok …
internal/examples/sqs-queue.cf.yaml exit=0 :: render validation ok …
```
Guard: git show --stat 770c657 touches only internal/examples/{irsa,k8s-app,s3-bucket,sqs-queue}.cf.yaml + docs. examples_test.go has TestAllExamplesAreValidBlueprints (b.Validate() only). No test renders the starters.

### CF-082 — provider / spec.sources reconciliation (two runs)
Run 1:
```
PUT pristine http=200
POST /api/providers iam → http=200  … "provider":{"ref":"ghcr.io/crossplane-contrib/provider-aws-iam:v2.7.0","digest":"sha256:5883bf…","kinds":46}
GET providers: [provider-aws-sqs, provider-aws-iam]
PUT doc with sources omitting iam → http=200
GET providers after PUT: {"providers":[{"ref":"ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0",…"kinds":8}]}
POST providers again → http=200
GET blueprint spec.sources: [{'provider': '…provider-aws-sqs:v2.7.0'}, {'provider': '…provider-aws-iam:v2.7.0'}]
kinds from iam provider count: 46
```
Run 2:
```
PUT pristine http=200
-- iam kinds after omit: 0
-- providers after omit: provider-aws-sqs
-- re-add: POST providers http=200
-- iam kinds after re-add: 46
```
internal/api/providers.go:161-163: "// Only return 409 Conflict if already in srv.Providers AND already declared in spec.sources." … "provider %q is already cached" (message text unchanged, but the branch is now unreachable in the stuck state).
Go: ok github.com/koorikla/compositionfactory/internal/api 0.041s for the two named tests.

### CF-083 — error visible with drawer collapsed
```
[CF-083] PUT status=200 (k8s-workload with app.fields["spec.replicas"] = {raw: "notanumber"})
[CF-083] drawer collapsed height=38
[CF-083] #valid chip text="validation error"
[CF-083] {"bannerExists":true,"bannerHidden":false,"top":46,"bottom":80,"h":34,"innerHeight":720,
 "text":"⚠️ line 55: resource \"app\" (Deployment): field \"spec.replicas\": invalid type: expected integer, got string \"notanumber\" Open in Drawer ×","warnTop":0,"warnHidden":false}
✓ CF-083 schema-type validation error is visible on screen with the drawer collapsed (2.1s)
```
Full suite: slice83 both tests ✓.

### CF-085 — font-load rebuild
Fonts delayed 2.5 s by route interception (8 requests intercepted); handles captured at DOM-attach:
```
[CF-085] fonts.status at first paint=loading faces=IBM Plex Sans:loading,…,IBM Plex Mono:loading,…; nodes=["xrd","work-queue","dead-letter"] widths=[198,218,198]
[CF-085] fonts.status=loaded connected=[true,true,true] widthsAfter=[198,212,198] present=[true,true,true]
✓ CF-085 card elements survive late font load and re-render (identity preserved)
```
canvas.js:476-495: builds "existing" map by data-id, removes stale, prev.innerHTML = nextEl.innerHTML only when changed; no canvasEl.innerHTML = h remains. grep -rn "document.fonts" web-proto/js → no hits (still true; moot with reconcile). Full suite: slice33 (2) and slice40 (3) ✓; 224 passed.

### CF-080 — integer param into ConfigMap data (real render, 3 engines)
```
=== CF-080 engine=go-templating   77:  PORT: "8080"
=== CF-080 engine=kcl             77:  PORT: "8080"
=== CF-080 engine=python          77:  PORT: "8080.0"
=== CF-080 python repro run 2     77:  PORT: "8080.0"
```
Emitted code: go `PORT: {{ $spec.port | quote }}`, kcl `PORT = str(_spec?.port)`, python `"PORT": str(spec.get("port"))`. python.go:31 uses MessageToDict(req.observed.composite.resource) → protobuf Struct numbers arrive as floats, so str() yields "8080.0". TestK8sWorkloadConfigMapPortQuotedAcrossEngines passes (checks the emitted source text, not the rendered value).

### CF-081 — cross-engine acceptance diff
```
--- PASS: TestAcceptanceAlternativeEnginesRender/engine=kcl (1.03s)
--- PASS: TestAcceptanceAlternativeEnginesRender/engine=python (1.05s)
--- PASS: TestAcceptanceAlternativeEnginesRender/diff-engines (0.00s)
ok  github.com/koorikla/compositionfactory 7.744s
```
testdata/xqueue.cf.yaml: only maxMessageSize: {from: params.maxMessageSize} (integer → integer field). No integer→string-map field, so the CF-080 divergence above ("8080" vs "8080.0") is outside the guard's reach. normalizeComposedResource only drops metadata.name.

### CF-074 — status wire quoting (real render with observed resource, 3 engines)
Blueprint: Queue q + ConfigMap cm with data[endpoint]: {from: resources.q.status.atProvider.url} and annotation platform.example.org/endpoint from the same wire; observed status.atProvider.url: "true".
```
=== engine=go-templating  34:  endpoint: "true"   39:    platform.example.org/endpoint: "true"
=== engine=kcl            34:  endpoint: "true"   39:    platform.example.org/endpoint: "true"
=== engine=python         34:  endpoint: "true"   39:    platform.example.org/endpoint: "true"
```
--- PASS: TestStatusWireIntoStringFieldQuotingAcrossEngines

### CF-073 / CF-075 — adopt
```
gen → compositions/…: labels: app: 'shop' tier: 'web'
adopt exit=0
adopted blueprint:  metadata.labels[app]: {value: shop}  metadata.labels[tier]: {value: web}  automountServiceAccountToken: {value: "true"}  plural: metalosses
regen: labels: app: 'shop' tier: 'web'
diff o1 o2: only "# Source: metaloss" vs "# Source: metalosses.platform.example.org"
CF-075: cf adopt <composition only> → kind: MetaLoss / plural: metalosses (stderr empty)
```
--- PASS: TestAdoptComposedResourceMetadataPreservation, --- PASS: TestAdoptPluralInferenceWithoutXRD

### CF-076 — int-or-string
```
targetPort {value: 8080}   => exit=0 :: render validation ok   emitted: targetPort: 8080
targetPort {value: "8080"} => exit=0 :: render validation ok   emitted: targetPort: 8080
targetPort {value: http}   => exit=0 :: render validation ok   emitted: targetPort: 'http'
cf fields Service: spec.ports[0].targetPort   string   false   Number or name of the port…
```
--- PASS: TestIntOrStringPreservesIntOrStringMetadata, --- PASS: TestValidateRenderedNativeServiceIntOrStringTargetPort

### CF-077
```
$ cf-j4 function add xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0 --lock .j4srv/fn.lock
added xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0
  digest sha256:762d6ac7…
  0 function input schemas of 0 CRDs
exit=0
lock: {"providers": null, "functions": [{"ref": "…function-auto-ready:v0.5.0", "digest": "sha256:762d…"}]}
```
--- PASS: TestFunctionAddNoInputCRDsSucceeds

### CF-078 / CF-079
```
docs/cli.md:269  cf function add xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0
docs/dsl.md:49/:352/:361/:378  xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0
internal/emit/pipeline.go:25  Package: "xpkg.upbound.io/crossplane-contrib/function-auto-ready:v0.5.0"
docs/cli.md:137/:156  rbac.yaml (emitted when composed native Kubernetes kinds require cluster RBAC permissions not pre-granted to Crossplane)
docs/cli.md:159  … (e.g., `HorizontalPodAutoscaler` …). Pre-granted kinds like `Deployment` or `Service` do not trigger RBAC emission
```
--- PASS: TestDefaultFunctionsInDocsMatchEmitter, --- PASS: TestRBACDocumentationTrigger

### CF-059 — friendly errors (two runs of the Sources-tab path)
Server: POST /api/providers {"ref":"ghcr.io/nope/does-not-exist:v0.0.1"} →
{"error":"fetch \"ghcr.io/nope/does-not-exist:v0.0.1\": GET https://ghcr.io/v2/nope/does-not-exist/manifests/v0.0.1: MANIFEST_UNKNOWN: manifest unknown"} http=502 (providers.go:193 writeJSONError(w, http.StatusBadGateway, err.Error())).
UI (#region-palette .warnbar), run 1 and run 2 identical:
```
Server unavailable (HTTP 502 Bad Gateway): fetch "ghcr.io/nope/does-not-exist:v0.0.1": GET https://ghcr.io/v2/nope/does-not-exist/manifests/v0.0.1: MANIFEST_UNKNOWN: manifest unknown. The backend server may be restarting or unreachable.
```
api.js:67-72 maps every 502/503/504 to "Server unavailable … restarting or unreachable"; the server uses 502 for upstream registry failures, so the advice is wrong and the raw Go error (fetch "…": GET https://…) still reaches the user verbatim. slice86's five tests pass (they cover network failure, a mocked 502 with empty body, spec.resources[i] mapping and "operation failed").

### CF-055 — optional parameter into required field (two runs)
Pristine doc with parameters.region.required = false (both Queues wire region: {from: params.region}):
```
run 1: POST /api/render → {"ok":false,"resources":0,"error":"line 51: resource \"dead-letter\" (Queue): missing required field \"spec.forProvider.region\" in Queue spec.forProvider\nline 76: resource \"work-queue\" (Queue): missing required field \"spec.forProvider.region\" in Queue spec.forProvider"}
run 2: identical; grep -c -i "optional|promote|params.region" over the error → 0
```
slice60 tests 89 (picker badge "optional → req" + promote prompt) and 149 (.port-opt-warn, inspector .wire-warn "optional param into required field") ✓ in the full run. The Validate message itself is unchanged and still blames the field.

### CF-050 / CF-065
```
[CF-050] focused after Tabs: {"cls":"kind","kind":"DaemonSet","tag":"DIV"}
[CF-050] after Enter: nodes 3->4, selected=daemon-set      (doc.spec.resources contains DaemonSet)
[CF-050] Space on focused card selects it
[CF-065] click on palette row: nodes 4->5
```
slice85: 5/5 ✓ (includes touch pointerdown and card action buttons).

### CF-057
```
[CF-057] version.outDir=.j4srv/out
[CF-057] generate title="Write generated manifests to .j4srv/out (overwrites existing files)"
[CF-057] dialog="Generate will write manifests to disk in '.j4srv/out', overwriting existing files.\n\nProceed?"
[CF-057] files after dismiss: []
[CF-057] files after accept: ["compositions","functions.yaml","providerconfigs","xrds"]
```

### CF-063 / CF-064
```
[CF-064] {"drawer":{"w":1280,"h":200},"canvas":{"w":734,"h":474},"palette":{"w":220,"h":474},"code":{"w":1090,"h":118},"drawerCss":"200px"}
[CF-063] DaemonSet · apps/v1 (k8s) | Deployment · apps/v1 (k8s) | StatefulSet · apps/v1 (k8s) | HorizontalPodAutoscaler · autoscaling/v2 (k8s)  (row width 219, title on row and on .nm)
```

### CF-066 / CF-069
```
[CF-066] before: "preview · 4 files" aria-label="Preview: 4 files"
[CF-069] {"role":"status","live":"polite","tabindex":"0"}
[CF-066] immediately after click: "validating…"
[CF-066] after: "valid · 2 resources" aria-label="Validation succeeded: 2 resources rendered"
[CF-066] after 4s idle: "valid · 2 resources"
[CF-066] after background generate(false): "valid · 2 resources"
[CF-069] after Enter on chip data-collapsed=null   (drawer expanded)
```

### CF-068
```
[CF-068] {"wireHits":3,"widths":["14px","14px","14px"],"pe":["stroke","stroke","stroke"],
 "dots":[{"cls":"port req","w":7,"h":7,"before":{"content":"\"\"","w":"18px","h":"18px"}}, … ×6 identical]}
[CF-068] 5px-off point: {"x":484,"y":179,"hit":"wire-hit"}
[CF-068] wire selected by 5px-off click
```
proto.css:203 svg.wires path.wire-hit{…stroke-width:14px;…pointer-events:stroke}

### CF-060 / CF-067 / CF-070 / CF-071 — computed tokens and ratios
Computed from the live page (data-theme toggled):
```
light: --dim #67727F --accent #1358B7 --pri #1358B7 --panel #FFFFFF --fg #0B0F14 --faint #67727F --warn #92400E --shared #685800 --surface #FFFFFF --surface-2 #F4F7FA --ground #E2E8F0 --sunk #D8E0EA --muted #54606F --ok #0D6D4B
dark:  --dim #8E99A8 --accent #5CA0F5 --pri #5CA0F5 --panel #161B22 --fg #E8ECF2 --faint #8E99A8 --warn #F59E0B --shared #D6AB33 --surface #161B22 --surface-2 #1C222B --ground #0C1015 --sunk #0F141A
CF-060 dark --faint on surface 5.99, surface-2 5.54, ground 6.61; .btn.pri dark 7.09, light 6.76
CF-070 light warn≠shared: warn/surface 7.09, warn/surface-2 6.59, shared/surface 7.04, shared/surface-2 6.54
CF-070 dark  warn≠shared: 8.05 / 7.45 / 8.02 / 7.41
CF-067 light on --sunk: .k 5.08, .st 4.77, .tm 5.32, .co 4.81, .sh 5.28
grep 7c3aed in proto.css, canvas-prototype.html, examples.go → none
```
CF-070 note: git log -S'B45309' → introduced in 74a1499 (CF-070), removed in dd6925f (CF-067). The archive's CF-070 wording quotes the superseded values; the contract still holds and slice67/slice91 drift tests pass.

### CF-062 / CF-072
```
[CF-062] {"drawerLbl":"Generated"}; tour.js:76 title: "The Generated drawer"; tour.js:52 "… Val / Wire / Raw toggles …"
[CF-062] inspector mode buttons=["Val","Wire","Raw"]; docs/guide.md:34 Reset View ⌂ … zoom to 100% … origin (0, 0); guide.md:13 "The **Generated** drawer"
[CF-072] native k8s kinds from API=16; canvas.js:467 "(16 native Kubernetes kinds ready without providers)"
```

### Full Playwright suite (HEAD, port 19140, own server)
```
  224 passed (1.6m)   1 skipped: slice16-provider-remove.spec.js:25 (pre-existing skip)
  slice33 ✓✓  slice40 ✓✓✓  slice60 ✓✓✓✓  slice67 ✓×11  slice83 ✓✓  slice84 ✓✓✓  slice85 ✓×5  slice86 ✓×5  slice87 ✓✓✓  slice88 ✓✓✓✓  slice89 ✓✓✓  slice90 ✓✓✓  slice91 ✓✓✓  slice92 ✓✓
```

## Notes for the fixer
- CF-080: python emitter should coerce via int() when the source param type is integer (or format floats with no fractional part) before str(); add a real-render assertion on the rendered value, not the emitted source text.
- CF-081: add an integer→string-map (and a status-wire) field to testdata/xqueue.cf.yaml or diff the k8s-workload starter as well.
- CF-059: providers.go:193 should not answer 502 for a registry lookup failure (or api.js must not label 502 as "backend restarting"); the message for a bad ref should say the package was not found.
- CF-055: when render fails on a required field fed by an optional parameter, the message should name the parameter and the promote action.
- CF-084: no test renders the starters; a table-driven --validate over internal/examples/*.cf.yaml (behind the same tool gate as acceptance) would guard it.
