- [x] **EnvironmentConfig (Track 2)**:
      In Crossplane v2, `spec.environment` is accessed via `index .context "apiextensions.crossplane.io/environment"` populated by `function-environment-configs`.
      Implemented `spec.environment` in blueprint DSL declaring scalar keys (string, integer, number, boolean) with defaults/descriptions, supporting `from: env.<key>` across fields, annotations, envelope, `forEach` bounds, and `when` guards with `unknownPath` nearest-match suggestions and `isFieldTypeCompatible` type safety.
      Auto-injects `function-environment-configs` step ahead of the templating step when `spec.environment` is non-empty, pinning `xpkg.upbound.io/crossplane-contrib/function-environment-configs:v0.4.0` in `functions.yaml`.
      Go-templating emitter outputs `$env` context dictionary with `hasKey` guards and safe resolution; Python emitter extracts `env` from `req.context`; KCL emitter cleanly refuses `spec.environment`.
      Composition emitter writes `factory.crossplane.io/environment-keys` annotation on metadata so `gen -> apply -> kubectl get -o yaml -> adopt -> gen` recovers the exact blueprint contract with byte-identical round-trip fidelity, with fallback inference for foreign compositions.
      — completed 2026-09-03
- [x] **Schema-aware function inputs (Track 1)**:
      Cached Function Input CRDs through existing xpkg -> schema -> index path keyed by their own API group without collisions with provider MR kinds. Pinned function refs in `.cf.lock` and added `cf function add <ref>`. Validated `spec.pipeline[].input` against resolved Input CRDs with nearest-match suggestions (`did you mean ...?`) on unknown fields and explicit warnings for uncached functions. Rendered typed forms with required badges, descriptions, and raw YAML escape hatch in canvas inspector (`web-proto/js/regions/inspector.js`), with catalogue-known package versions.
      — completed 2026-09-03
- [x] **Expression authoring — preview and snippets (Track 3)**:
      Implemented in-process Go template preview (`POST /api/preview-expression`, `internal/emit/preview.go`, `internal/api/preview.go`) executing Go-template expressions against a synthetic context (`$spec`, `$xr`, `$xrMeta`, `$env`, `$i`, sibling resource status, Sprig & function-go-templating helpers) with `missingkey=error`. Added scope-filtered snippet catalogue and real-time live preview under raw expression editors in the canvas inspector (`web-proto/js/regions/inspector.js`, `web-proto/css/proto.css`).
      — completed 2026-09-03
- [x] **The round-trip gate asserts byte-for-byte fidelity**:
      `scripts/cluster/test-cluster.sh` applies, reads live XRD and Composition back with `kubectl get -o yaml`, imports with `cf import`, regenerates, and verifies with strict `diff -u` against the original emitted manifests. In addition, Go unit tests in `internal/adopt/tree_test.go` (`TestRoundTripEmittedCompositionAndXRD`) verify round-trip fidelity with simulated server-side noise and scrubbing.
      — completed 2026-09-03
- [x] **`canvas.js openFieldPicker` helper modularization**:
      Extracted type compatibility (`isFieldPickerTypeMatch`), candidate building and relevance scoring (`buildFieldPickerCandidates`), item rendering (`renderFieldPickerItems`), and input/key event bindings into cleanly scoped modular helpers.
      — completed 2026-09-03
- [x] **Stale subagent branches and worktrees pruned**:
      Evaluated and cleanly deleted all 13 stale subagent branches (`subagent-DX--Client...`, `worktree-agent-a5a7...`, `worktree-agent-a7f7...`, `subagent-Canvas...`, `worktree-agent-a247...`, etc.) after merging/verifying their changes.
      — completed 2026-09-03
- [x] **Opaque Block & Custom Function Pipeline Preservation**: When importing complex foreign compositions containing unknown custom functions or non-standard pipeline steps, preserve them as declared custom steps in `spec.pipeline` / `spec.resources` with typed `Input` and `Position` preserved so they round-trip cleanly without loss.
      — completed 2026-09-03
- [x] **Round-trip gate in Lane C (`make test-cluster`)**: Extended `test-cluster.sh` to apply emitted examples, read back live Compositions with `kubectl get -o yaml`, import them via `cf import`, and verify regenerated artifacts.
      — completed 2026-09-03
- [x] **`kubectl` Export Scrubbing**: Automatically scrub runtime status, managed fields, UIDs, and cluster-assigned metadata when pasting or importing raw cluster dumps (`kubectl get composition -o yaml`), reporting how many server-side fields were dropped in `LossReport`.
      — completed 2026-09-03
- [x] **Canvas Region Modularization**: Extracted inner helpers and sub-views from oversized `init` closures in `palette.js` and `output.js` into isolated, modular functions and component renderers.
      — completed 2026-09-03
- [x] **Simplify Emitted Status Wires**: Replaced the 11-term `hasKey/kindIs` guard chain with a clean, missingkey-safe Sprig `hasKey (dig ...)` helper in Go-templating outputs (`internal/emit/composition.go`), while maintaining full backward-compatibility with `cf adopt` (`internal/adopt/adopt.go`).
      — completed 2026-09-03
- [x] **Canvas Action Dispatch Maps**: Replaced large `if/else` ladders in `inspector.js` (`onBoxClick` 316 -> 16 lines, `onBoxChange` 295 -> 183) with modular `const actions = { ... }` dispatch tables.
      — completed 2026-09-03 (`canvas.js` `openFieldPicker` was listed here too but is unchanged at 322 lines; it is a builder rather than an action ladder, so it needs extraction rather than a table. Carried forward as an open item.)
- [x] **Direct Configuration Source Tree Import**: Extend `cf import` and `cf adopt` to read full Configuration repositories (`crossplane.yaml`, `apis/<xr>/definition.yaml`, `composition.yaml`), extracting XR schemas, resource templates, and parameters into a canonical `.cf.yaml` blueprint in one step.
      — completed 2026-09-03
- [x] `cf catalogue --kind` exact match filtering: wired `catalogue.PackagesForKind` to
      strictly filter packages serving the requested CRD kind.
      — completed 2026-09-03
- [x] Engine-refusal preamble deduplication: extracted `refuseGoTemplateOnlyFeatures` in
      `internal/emit/plan.go`, unifying non-Go engine rejection of conventions, template fields,
      and Go template syntax across KCL and Python.
      — completed 2026-09-03
- [x] Workspace Worktree & Disk Hygiene: pruned merged worktrees and branches, removed retired `web/` directory.
      — completed 2026-09-03
- [x] Drag-to-wire type warning action "change parameter type" converts XRD parameter
      type to matching target field type upon user confirmation (`canvas.js:1330`).
      — completed 2026-09-03
- [x] Unify inspector "env:" and canvas "envelope." target namespaces across wiring handlers.
      — completed 2026-09-03
- [x] The three emitters walk the same tree three times. Lift the common traversal,
      validation, kind resolution, conventions merge, field/annotation planning, and engine-refusal
      preamble into `internal/emit/plan.go` (`planSingleResource` and `refuseGoTemplateOnlyFeatures`),
      used identically across `composition.go`, `python.go`, and `kcl.go`.
      — completed 2026-09-03
- [x] `catalogue.Kinds` surfaced in the `cf catalogue` package table so a reader can
      see which kinds a package serves.
      — completed 2026-09-03 (only half: `--kind` was added but routes to the free-text
      `Search`, so it does not filter by served kind and `PackagesForKind` is still
      unreachable. Carried forward as an open item.)
- [x] `internal/cluster` test coverage brought from 55% to 78.5% with comprehensive error path
      and kubeconfig TLS/context testing.
      — completed 2026-09-03
- [x] `make test-race` exists and passes but no CI lane invokes it. Add it to
      lane A or a nightly — the API server has a shared index and memoised
      schema trees, which is exactly what the race detector is for.
      — completed 2026-09-03
- [x] Engines list: index.html still hardcodes the three `<option>`s so the /api/version path
      is dead code; touch: `pointerdown` + `touchstart` both pan on one finger (no
      `pointerType` guard, no touch spec); api.js header still says it throws a plain object;
      `resourceFromMap` returns an error it never produces; `make clean` leaves web/ dist;
      scope-mismatched KINDS group labelled but not collapsed.
      — completed 2026-09-03

Work that is done, moved out of BACKLOG.md on 2026-09-03 so it stops costing
every agent a thousand lines of context on read. Kept whole rather than
deleted: several of these items are the only written record of *why* something
is the way it is, and this repo has already re-raised settled questions twice.

Nothing here is open work. Open items live in BACKLOG.md. Full history is in
git — `git log -p BACKLOG.md`.

- [x] Catalogue search is a substring over name/description only: `DatabaseInstance`,
      `CloudSQL`, `ServiceAccount`, `Topic` return nothing; `Bucket` returns
      provider-bitbucket-server; all 84 GCP entries carry the same description. Index kinds
      per package at catalogue build time and search kind → package. C.
      — completed 2026-09-03
- [x] No CLI to browse kinds, fields, status outputs or the catalogue — every agent started
      `cf serve` and hit `/api/kinds/...` (and one reverse-engineered `cache/*/crds.json`).
      Add `cf kinds [q]`, `cf fields <kind> [--required] [--status]`, `cf catalogue <q>`. A, C.
      — completed 2026-09-03
- [x] Lane B renders only go-templating. Extend the acceptance test to render each engine
      through its real function image (function-kcl, function-python) on the same fixtures;
      the Python `.get` bug would have been caught on day one.
      — completed 2026-09-03
- [x] cf cannot adopt its own output: the `{{- $spec := … -}}` prelude and `{{- if hasKey }}`
      guards break the mask-then-YAML parser (`cannot unmarshal string into … map`). Every
      `{{ }}` is masked as a quoted scalar, so block-level actions become YAML values.
      Decide: fix adopt's masking to treat control-flow lines as opaque blocks, or fold adopt
      into the Backlog v3 reader (which must read cf's dialect anyway). Found by C and D.
      — completed 2026-09-03
- [x] Non-param mustache lands in `value:` and gen single-quotes it (`tags: '{{ toYaml … }}'`);
      `.observed.composite.resource.spec.X` is not recognised as a param; nested maps are
      flattened to dotted paths gen then refuses; arrays/objects serialised via fmt.Sprint
      (`'[map[conditionStatus:False …]]'`); composed apiVersion is rebound to `--provider`
      (cluster-scoped `iam.aws.upbound.io` → `.m.`); without `--provider`, `sources: null`.
      — completed 2026-09-03
- [x] The escape hatch does the real work: nested objects (until P0 lands), array elements,
      typed literals, quoted strings, XR-derived names (`{{ $xr }}-sa-key`), per-index values
      (`printf "10.0.%d.0/24" $i`, `index (list …) $i`) and aggregate status wires over a
      forEach set (a 500-character one-line `range … append … toJson` with a hand-written
      guard chain) all needed `raw:`. `raw:` must be single-line, and `$i`, `$spec`, `$xr`,
      `$xrMeta` are undocumented — every agent read composition.go to learn them. Document the
      raw contract now; then add first-class forms in this order: typed literals (P0 above),
      `resources.<looped>[*].status.<path>` list wires, forEach index helpers (cidr/az from
      index), XR-name interpolation in envelope/annotation values, paired forEach.
      — completed 2026-09-03
- [x] `template:` cannot see observed resources (`map has no entry for key "observed"`), so
      any string built around a status wire (a redrive policy JSON, `serviceAccount:<email>`)
      is raw with a hand-copied 11-term guard. Give templates the observed map and a helper
      that emits the guard. A, E.
      — completed 2026-09-03
- [x] Object param into a map leaf is refused; an explicit `tags[env]` entry replaces the
      convention wholesale instead of merging. A, C.
      — completed 2026-09-03
- [x] Field-level `when` is rejected as `unknown field "when"` with no hint it is resource-level.
      Parameter named `n` fails as `parameters.false` (YAML 1.1). Kind `ProjectIamMember` gets
      "not found; run cf provider add" instead of the nearest match. `--check` ignores stale
      extra files in out/.
      — completed 2026-09-03
- [x] The kind list mixes `.m.` and cluster-scoped duplicates for a Namespaced XRD (backlog
      v2 labelled them, did not hide them). `cf provider add --help` and the providerconfigs
      ASSUMPTION note point at xpkg.upbound.io/upbound while everything else is
      ghcr.io/crossplane-contrib. A, C.
      — completed 2026-09-03
- [x] `(*Blueprint).Validate` (internal/blueprint/load.go:408-1000) is 592
      lines, the largest unit in the repo and the one place every authoring
      mistake has to be caught: XRD, parameters, resources, fields, when,
      forEach, envelope, annotations, pipeline and templates in one pass. Its
      own neighbours show the split — `validateStatusRef`,
      `validateForEachParamRef`, `validateForEachStatusRef` already sit beside
      it. Extract `validateXRD` / `validateParameters` / `validateResources` /
      `validateFields` in the same style; each becomes directly testable
      instead of reachable only through a whole blueprint. Half a day, low
      risk (package is at 90.3% coverage, error strings pinned by tests).
      — completed 2026-09-03
- [x] function-patch-and-transform `input.resources` is discarded with no warning
      (`resources: []`), and a package `…function-patch-and-transform:v0.1.0` is invented.
      docs/cli.md claims classic P&T support; only pre-2.0 `spec.resources` is parsed. Found
      by C and D.
      — completed 2026-09-03
- [x] XRD parsing ignores the schema when parameters sit flat under `spec` (cf's own XRDs,
      Crossplane v2 style): `required`, types, defaults, descriptions, nested objects all lost;
      arrays refuse (`type "array" is not supported`); `claimNames`/`connectionSecretKeys`
      dropped. Found by D.
      — completed 2026-09-03
- [x] Every patch `fromFieldPath` becomes a parameter name (`metadata.uid`, `status.eks.oidc`
      → "invalid parameter name"), resource names are not normalised to DNS labels, block
      scalars are put in `value:` and then refused for containing `\n`. Adopt of
      platform-ref-aws XEKS needed five successive manual edits and still failed. Found by D.
      — completed 2026-09-03
- [x] No loss report at all — only "Adopted blueprint written to …". Print what was dropped,
      write `# adopt: dropped …` comments, exit 2 when lossy, validate the adopted blueprint
      against the schema before writing. `--cache-dir` is not accepted by `cf adopt`; output
      is padded with `from: ""`, `raw: ""`, `conventions: null`, `enum: null`.
      — completed 2026-09-03
- [x] PUT /api/blueprint and replace_blueprint are not atomic: a document whose new source
      fails to fetch (`MANIFEST_UNKNOWN`) is persisted anyway, and every later call fails with
      the same fetch error. Sync sources before persist; roll back on failure. Found by E.
      — completed 2026-09-03
- [x] add_provider / POST /api/providers caches the schemas but does not declare the source in
      `spec.sources`; the first resource using it is refused. Declare it (idempotently). E.
      — completed 2026-09-03
- [x] Unknown field paths and status paths pass PUT/replace with 200 and fail only at
      generate; docs/mcp.md promises refusal at replace time. Validate at PUT with the same
      did-you-mean; nested unknown paths currently get no suggestion at all. E.
      — completed 2026-09-03
- [x] Resource rename/delete ignore `raw:` references: rename leaves the old name inside raw
      guards; delete of a raw-referenced resource succeeds and generates a guard that is
      false forever (from: wires are protected). Scan raw text for `"<name>"` in observed
      lookups, or warn. E.
      — completed 2026-09-03
- [x] HTTP ignores unknown query params (`?search=` silently returns everything; kinds use
      `q`); POST /api/render ignores an `xr` body key silently; /api/blueprint/import wants raw
      YAML while /adopt wants `{"manifest"}`. Return 400 on unknown params, accept `search` as
      an alias, document bodies (an /api/openapi or route list). C and E.
      — completed 2026-09-03
- [x] render_check reports Docker transients (`container is marked for removal`) as a
      blueprint `error`; classify daemon/runtime failures as `unavailable`. E.
      — completed 2026-09-03
- [x] `cf mcp` exits when `--blueprint` does not exist while `cf serve` scaffolds; parameter
      `default` accepts non-strings and enum/default mismatches; MCP errors give CLI advice
      (`run: cf provider add`) instead of the tool name; the persisted document drifts to
      `sources: null` and every field carries `from: "" raw: "" template: "" value: ""`. E.
      — completed 2026-09-03
- [x] get_kind_fields lacks `enum`, `default`, `minimum`/`maximum`, `format` (they exist only
      as prose in descriptions) and there is no status view, so an agent wiring
      `resources.x.status.atProvider.arn` guesses. MCP has no resource add/update/rename/
      delete tools — structural edits are whole-document replace only. E.
      — completed 2026-09-03
- [x] Catalogue search is a substring over name/description only: `DatabaseInstance`,
      `CloudSQL`, `ServiceAccount`, `Topic` return nothing; `Bucket` returns
      provider-bitbucket-server; all 84 GCP entries carry the same description. Index kinds
      per package at catalogue build time and search kind → package. C.
      — completed 2026-09-03
- [x] No CLI to browse kinds, fields, status outputs or the catalogue — every agent started
      `cf serve` and hit `/api/kinds/...` (and one reverse-engineered `cache/*/crds.json`).
      Add `cf kinds [q]`, `cf fields <kind> [--required] [--status]`, `cf catalogue <q>`. A, C.
      — completed 2026-09-03
      script calls `.get()` on protobuf Struct/Message (`oxr.get("spec")`,
      `ocds.get(...).get("resource")`); verified with crossplane-function-sdk-python. Also
      `ready = fnv1.READY_TRUE` unconditionally while function-auto-ready is still in the
      pipeline. Found by C.
      — completed 2026-09-03
- [x] KCL forEach emits invalid syntax (`for _i in range(...):` inside a list literal →
      `InvalidSyntax expected one of ["]"]`); unobserved status wires emit `null` where
      go-templating omits the key; loop instances are named `${_i}-topic` in KCL and
      `f"{_i}-topic"` in Python but `topic-0` in go-templating, which breaks observed-resource
      matching and contradicts the forEach error text. Found by A and C.
      — completed 2026-09-03
- [x] `raw:` is pasted verbatim into KCL/Python programs (`settings = {tier: {{ … }}}` →
      syntax error at render). Refuse `{{` in raw under non-go engines with a clear message, or
      document raw as go-templating-only. Found by A, B and C.
      — completed 2026-09-03
- [x] Docker container reuse via `render.crossplane.io/runtime-docker-name: cf-function-*`
      leaves a container attached to a stale network after a failed run; the next renders
      fail (`container … is not connected to Docker network`) or hang 2 minutes. Use a per-run
      name, or document `docker rm -f cf-function-*` in the Validate error tip. Found by A and C.
      — completed 2026-09-03
- [x] `crossplane composition render` and POST /api/render report ok on output the API server
      would reject (all of the above). Add schema validation of the rendered composed
      resources against the cached CRDs in `cf gen`, `/api/render` and the Validate chip; agent
      A's typecheck.py against `crossplane xpkg extract` output is the reference.
      — completed 2026-09-03

## Canvas slice backlog (BDD loop: spec → GUI → backend → verify)

- [x] `lifecycleRule[0].action.type` is accepted by the validator (it even suggests it) and
      listed by /api/kinds/…/fields, then emitted as the literal key
      `'lifecycleRule[0].action.type'`. Implement array-element emission or refuse the grammar
      for provider kinds. Found by C.
      — completed 2026-09-03
- [x] `spec.emit.templateSource: FileSystem` cannot be packaged (`cannot package a blueprint
      with … FileSystem`) and cannot be rendered locally (`cannot read tmpl from the folder
      /templates`) with no hint that only in-cluster works; the `when:` guard is split across
      two files (`{{- if }}` ends 005-ingress.yaml, `{{- end }}` ends 006-hpa.yaml). Found by B.
      — completed 2026-09-03
- [x] Every composed native object gets `generateName: <xr>-`, so sibling references by name
      dangle: `serviceAccountName: web`, HPA `scaleTargetRef.name: web`, Ingress backend
      `web` — on the cluster: `error looking up service account default/web: serviceaccount
      "web" not found`. The shipped k8s-workload starter has the same defect. Emit
      deterministic `metadata.name` for native kinds (or `<xr>-<resource>`), make
      `metadata.name`, `metadata.labels`, `metadata.annotations` settable, and support
      `from: resources.<n>.metadata.name` so references resolve. Found by B.
      — completed 2026-09-03
- [x] `cf gen` writes no RBAC and never warns; `/api/rbac` is bare rule JSON (no ClusterRole
      object, no aggregation label, includes the XR's own resource and the four pre-granted
      kinds). Verified on Crossplane 2.4: the rules are correct and complete, and the
      failure is loud (`ComposeResources … "ingress" … is forbidden`, pipeline aborted), so
      emit a ready aggregated ClusterRole for the non-pre-granted kinds only and warn at gen
      time. Found by B.
      — completed 2026-09-03
The original slice queue, worked from 2026-09-01. Every item shipped with a
Playwright behavior.

- [x] Nested forProvider paths are emitted as literal dotted keys (repro): `settings.tier:
      {from: params.tier}` → `settings.tier: {{ $spec.tier }}` under forProvider, and the API
      server prunes it on apply. The validator accepts the path (it exists in the CRD) and the
      writer never re-nests it; only native kinds get a tree (emit/native.go buildNativeTree).
      Build the same tree for provider kinds (composition.go planFields → writer) with the
      hasKey guards moved to the leaf. Golden: DatabaseInstance settings.tier +
      settings.ipConfiguration.ipv4Enabled renders as a nested map. Found by A and C.
      — completed 2026-09-03
- [x] `resolveKind` ignores `provider:` (composition.go:1263 matches Kind only): `kind: Instance`
      with provider-aws-rds silently resolves to ec2 Instance because ec2 was listed first.
      Match on the resource's declared provider, error on ambiguity. Found by A.
      — completed 2026-09-03
- [x] Only nine kinds are vendored (Deployment, Service, ConfigMap, Secret, ServiceAccount,
      StatefulSet, Job, CronJob, DaemonSet); Ingress, HPA, PVC, NetworkPolicy, PDB, Role and
      RoleBinding are refused and the refusal does not list the set. Workaround was
      hand-written CRD stubs via `sources: - crds:`. Vendor the rest of core and name the set
      in the error. Found by B.
      — completed 2026-09-03
- [x] `providerName` is mandatory even with zero managed resources and is never consumed by
      a native-only composition. `cf gen` emits a Deployment with no selector/template
      without warning (only `requiredBranches` in the API knows). `cf --version` errors.
      `crossplane xpkg extract` needs `--from-xpkg` (docs omit it). Found by B.
      — completed 2026-09-03
- [x] `value:` is always a quoted string (structured.go:43 quoteYAML) regardless of the CRD
      leaf type: `enableDnsHostnames: "true"`, `allocatedStorage: "20"`; and string params are
      emitted bare so `engineVersion: 16.3` becomes a float. Emit typed literals from the leaf
      type (bool/number unquoted, strings `| quote`), refuse `value: notabool` on a boolean,
      refuse a scalar `from:` into an array leaf (envelope already does, envelope.go:176) or
      wrap it. Add a param-type vs leaf-type check with a clear error. KCL already emits typed
      literals, so the engines currently disagree. Found by A and C.
      — completed 2026-09-03
- [x] Header `# Source: blueprints/<name>.cf.yaml` is fabricated — it prints a hardcoded
      prefix, not the path given. Found by A, C and E.
      — completed 2026-09-03
- [x] String parameters are emitted unquoted, so YAML retypes them: `"0x1F"` → `31`, `"1e3"`
      → `1000` (an Ingress host!), `"null"` → null, `"on"` → true; the API server then
      rejects the Deployment (`cannot unmarshal bool into … EnvVar.value`). Only annotations
      get `| quote`. Same root cause as the typed-literal item above; `data[PORT]: {from:
      params.port}` on a ConfigMap likewise needs stringification into string-typed
      targets. Found by B.
      — completed 2026-09-03
- [x] Remove + copy objects through the GUI: duplicate a resource via copy/paste
      (Cmd/Ctrl+C on a selected card, Cmd/Ctrl+V pastes a deep copy with a unique
      name and all field values/wires), plus an explicit duplicate action and
      delete (with confirm listing wired fields). All through the full-doc PUT.
      — user request 2026-09-01 ("copy paste to duplicate queues")
- [x] SHARED tab: an "add shared parameter" button — create a new XRD parameter
      (name/type/required/default/enum) directly from the SHARED rail, not only
      via the XR card or field binding. — user request 2026-09-01
- [x] Canvas zoom in/out (and pan): scroll-wheel / pinch zoom centered on the
      cursor, +/- controls, wires and drop coordinates tracking the transform.
      — user request 2026-09-01
- [x] UX polish: a Guide tab (how the canvas, DSL and generate loop work,
      keyboard shortcuts), plus richer mouseover texts — field descriptions,
      wire tooltips, button titles everywhere. — user request 2026-09-01
- [x] Undo/redo: topbar buttons + Cmd/Ctrl+Z / Shift+Cmd/Ctrl+Z. Needs a doc
      history in the proto store (snapshot ring on every successful PUT).
      — user request 2026-09-01
- [x] Wheel zooms (shift+wheel / ground-drag pans) — user request 2026-09-01
- [x] Type-aware parameter controls: object = free-form string map (no default/enum,
      explained inline); boolean true/false default; array (engine-rejected) no longer offered
- [x] Resizable palette + inspector columns, clamped + persisted
- [x] forEach on a resource ("for objects"): N instances driven by a parameter
      (e.g. RDS cluster + $instanceCount ClusterInstance nodes), gotpl range
      semantics per the design spec, setResourceNameAnnotation indexed in the
      loop (spec §8), GUI badge becomes authorable. ENGINE + GUI slice.
      — user request 2026-09-01 ("for nf or nodes for rds")
- [x] Provider actions in SOURCES: expandable info per provider (digest, version,
      kind list, registry host), and remove — needs DELETE /api/providers with a
      409 naming referencers when the blueprint still uses it. More actions as
      they come. — user request 2026-09-01
- [x] Pipeline steps in the GUI: show the step chain in the XRD inspector,
      presets (function-auto-ready, function-environment-configs, etc.),
      positioning (before/after), input editing, functions.yaml output tab.
      — completed 2026-09-02
- [x] Envelope field control (engine + GUI complete): per-resource authoring of
      the Crossplane-native envelope — writeConnectionSecretToRef (name/namespace),
      managementPolicies, and whatever else the kind's .m. CRD envelope actually
      carries (validated against Envelope(), same {value|from|raw} field forms).
      GUI: an "envelope" section on the card/inspector. — user request 2026-09-01
- [x] Cross-object atProvider wiring GUI (engine + GUI complete): wire e.g. a
      postgres provider field from an RDS instance's status.atProvider output —
      teal status wires, pickable from the source card's status section.
- [x] Provider detail view: click a provider in SOURCES → full registry ref shown,
      its kinds listed with checkboxes to select which appear in the KINDS rail
      (client-side filter, persisted). — user request 2026-09-02
- [x] Catalogue must cover upjet family services (provider-aws-rds et al. — repo
      enumeration misses monorepo-published packages). — user request 2026-09-02
- [x] Generate ProviderConfig scaffolds to out/providerconfigs/. — user request 2026-09-02
- [x] Live-cluster schema source: run against a kind/k3s (or any) cluster's API
      to dynamically discover CRDs/kinds beyond packaged providers — the
      "external schema" phase of the control-plane direction. — completed 2026-09-02
- [x] Right-click context menu on canvas objects (duplicate/remove/rename/bind…)
      — improve beyond the browser default. — user request 2026-09-02
- [x] KINDS hover preview: a small card with the kind's description + a few key
      fields when hovering a palette row. — user request 2026-09-02
- [x] Effective requiredness for the inspector: the Required filter must show
      what a user actually must set — top-level required branches (Deployment's
      selector/template) surfaced as expandable required rows, and leaf
      requiredness conditioned on its ancestor chain (EnvVar.name is required
      only once env exists). Engine: tree.go/API change; GUI: Required filter
      semantics. Regression net already merged (required_test.go). Blocked on
      the engine-batch integration landing (tree.go is hot).
- [x] Typed object parameters (engine+GUI): an object param with declared member
      fields ("+ then string/boolean/int") — real XRD sub-properties instead of
      the v1 free-form string map. Emit properties/required, wire members as
      params.obj.member. — user request 2026-09-02
- [x] Manual card resize: drag handle on cards (fields still clip at the 340px
      cap; users want to size objects themselves), size kept client-side like
      positions. — user request 2026-09-02
- [x] Output drawer: providerconfigs tab(s) next to the DSL tabs, annotated with
      the picker state ("provider-aws-s3 — only Bucket enabled"), and an RBAC
      tab from GET /api/rbac. — user request 2026-09-02
- [x] Annotations authoring (engine shipped; IRSA opening demo live): Resource.annotations map with
      value/from/raw/template forms — IRSA (Role arn → ServiceAccount
      annotation) as the acceptance fixture; becomes the opening demo.
- [x] First-class map-entry wires in fields (annotations[key]-style bracket
      grammar & GUI authoring) — completed 2026-09-02
- [x] Test isolation v2: the behavior suite gets its OWN cf serve (own port +
      scratch blueprint copy) so runs stop trampling the doc the user is
      looking at (isolated runner on 8081). — completed 2026-09-02
- [x] Palette required-count badges adopt effective (chain+branch) counts:
      Deployment shows "2 req" instead of "250 req" using RequiredChain and
      RequiredBranches in index.Build. — completed 2026-09-02
- [x] Drag-to-wire: drag a line from an XR parameter dot onto a card → popup
      menu of type-compatible input fields to bind (params.X → chosen field),
      or drop directly onto a target port. — completed 2026-09-02

- [x] `cf package` / `cf push`: Configuration (.xpkg) packaging per the
      2026-09-02 decision memo — crossplane.yaml synthesized from sources +
      effective pipeline (exact "=tag" pins, digests kept verbatim), blueprint
      source embedded as an annotation, single-layer xpkg readable by our own
      fetch path AND `crossplane xpkg extract` (verified against v2.5.0);
      GET /api/package + canvas Package button download the same bytes.
      — completed 2026-09-02
- [x] Compose ANY CRD-backed object (user request 2026-09-02: "existing
      composition claim, an argo workflow etc — according to the crd
      scanning"): `crds:` source form (a CRD manifest file, loaded by every
      front door via cache.LoadSources) + POST /api/sources/crds + GUI
      "+ Add CRDs from file" in SOURCES; scanned kinds are object-rooted
      (the composed document IS the object) and resolve for any provider
      ending .yaml/.yml, "cluster" included — which also closes the
      cluster-kind drop→resolve gap. — completed 2026-09-02
- [x] OpenAPI-grade object parameters (user request 2026-09-02): members
      now nest to arbitrary depth (engine: recursive Validate + XRD schema +
      ParamChain guard chains; params.a.b.c wires). Inspector renders a
      recursive member-tree editor (add/rename/type/required/default/delete
      at any depth); wire dropdowns enumerate nested paths; paramFrom no
      longer drops properties on unrelated updates. Arrays stay refused
      with a clear error. — completed 2026-09-02
- [x] `cf adopt`: Ingest existing Crossplane Compositions (supporting both
      function-go-templating and classic patch-and-transform, with optional
      embedded/sibling XRDs) into clean, deterministic blueprints. Exposed via
      `cf adopt` CLI, `POST /api/blueprint/adopt` HTTP API, and `adopt_composition`
      MCP tool. — completed 2026-09-02
- [x] go-templating FileSystem source export (user request 2026-09-02):
      `spec.emit.templateSource: FileSystem` (also switchable in the GUI)
      exports templates as a folder — 000-context + one file per object,
      helm-chart-style — whose lexical "\n---\n" concatenation is
      byte-identical to the inline body (the function's own reassembly
      contract), packed into ConfigMap(s) under the ~1MiB limit and mounted
      via a DeploymentRuntimeConfig; functions.yaml pins runtimeConfigRef.
      — completed 2026-09-02
- [x] Docs restructure (user request 2026-09-02): split README extras into
      /docs to keep the main README quick to read; make the gif-recorder
      flow part of it; use /docs content to GENERATE the in-app Guide tab(s)
      and improve them. — completed 2026-09-02
- [x] Startup example chooser (user request 2026-09-02): pick between a few
      starting blueprints on first load — IRSA, an RDS composition, and a
      k8s app composition that uses both. — completed 2026-09-02
- [x] Floating / movable panels & mobile support (user request 2026-09-02):
      Inspector and Code Editor / Drawer can be floated with a single click,
      dragged freely across the canvas, resized, collapsed/minimized, and
      locked/docked back in place (with positions persisted). Enhanced mobile
      experience with topbar horizontal scrolling, 1-finger canvas touch pan &
      2-finger pinch zoom, responsive drawer overlays and backdrop dismissal.
      — completed 2026-09-02
- [x] Ansible provider support — exploration done (memo:
      docs/research/2026-09-02-ansible-provider-support.md). Composing
      AnsibleRun works today (provider-ansible is in the catalogue; templated
      playbookInline carries XR params). Recommended follow-ups when demand
      shows: an example/Guide section, then `cf adopt --ansible` lifting role
      vars into XRD parameters. Task transpilation ruled out. — 2026-09-02
- [x] Alternative Composition Emitters — KCL (`function-kcl`):
      `spec.emit.engine: kcl` (and GUI engine dropdown) generates idiomatic
      KCL Compositions with typed `KCLInput` (`krm.kcl.dev/v1alpha1`), `oxr`,
      `ocds`, status wires, loops, conditionals, and auto-ready annotations;
      auto-pins `function-kcl` in `functions.yaml` and `package.yaml`.
      CLI: `cf gen --engine=kcl`. — completed 2026-09-02
- [x] Auto-import & cache provider schemas on example & blueprint import:
      loading starter blueprints (IRSA, RDS, K8s App) or importing any blueprint
      YAML via GUI / API dynamically fetches, caches, digest-pins and indexes all
      declared provider dependencies into the local schema cache and `/api/kinds`.
      — user request 2026-09-02
- [x] Alternative Composition Emitters — Python (`function-python`):
      `spec.emit.engine: python` (and GUI engine dropdown) generates native
      Python Compositions using `python.fn.crossplane.io/v1beta1` `Script` with
      typed `req: fnv1.RunFunctionRequest, rsp: fnv1.RunFunctionResponse`, desired
      resource updates, field parameter bindings, status wires, loops, conditionals,
      and `fnv1.READY_TRUE`; auto-pins `function-python` in `functions.yaml` and `package.yaml`.
      CLI: `cf gen --engine=python`. — completed 2026-09-02
- [x] Wire Selection & Deletion on Canvas:
      Clicking any wire selects and highlights it with a glowing dashed stroke and
      presents a floating `×` delete badge at the curve midpoint. Pressing `Delete` / `Backspace`,
      clicking the delete badge, or right-clicking for the context menu deletes the wire
      binding from fields, envelope, or annotations with full undo/redo support (`Cmd/Ctrl+Z`).
      — user request 2026-09-02

## Consolidation backlog (audit re-verified 2026-09-02 at 062bd82)

Re-checked after the Antigravity run. Already resolved and NOT listed below:
index rebuild unified for provider add/delete + crds source (c2ee07e), gofmt
drift (47bc0c1, 51b74d1, 7aec258), docs restructure into /docs, stray root
`cf` binary removed. Suite state at re-check: go test -short green, vet/tidy
clean, Playwright 115 passed / 1 skipped.

### Correctness (do first)

- [x] KCL and Python emitters drop every field/envelope/annotation guard and
      re-parse the Go-template RHS as a string: `.guard` is never read in
      internal/emit/kcl.go or python.go, so an optional param or a status
      wire renders unconditionally; unrecognised RHS shapes fall through to
      a quoted literal of the raw `{{ ... }}` text with no error.
      `translateWhenToKCL` does `ReplaceAll(when, "true", "True")` on the
      whole expression (corrupts any param name containing true/false).
      Fix shape: planFields/planAnnotations/planEnvelope return a structured
      RHS (kind + param/resource + path + guard); each backend formats it.
      — completed 2026-09-02
- [x] `POST /api/cluster/sync` still carries its own inline index rebuild
      (internal/api/cluster.go:104-124) that omits `crds:` sources — every
      scanned CRD kind disappears from /api/kinds after a sync. Replace with
      `srv.rebuildIndexLocked()`.
- [x] `rebuildIndexLocked` (internal/api/server.go:480) swallows six error
      paths (`ReadFile`, `yaml.Unmarshal`, `Store.Load`, crds ReadFile /
      ParseCRDManifest, `k8s.Kinds`) with `if err == nil`; provider add /
      delete / crds-add / example-load lost their 500s. An evicted cache
      entry now stays listed in /api/providers with its kinds silently gone.
      Return the errors (keep the deliberate `continue` on missing sibling
      crds files); in delete, assign `srv.Providers` after a successful build.
- [x] `adopt.Adopt` never calls `bp.Validate()` (nor do cmd/cf/adopt.go or
      the MCP tool) — `cf adopt` can write a blueprint `cf gen` refuses. Its
      `splitYAML` also swallows unmarshal errors with `continue`, so a
      malformed Composition reports "no Composition document found".
- [x] `cf package` / `GET /api/package` select output docs by a `"xrds/"` /
      `"compositions/"` string prefix against `filepath.Join` paths — zero
      docs on Windows (cmd/cf/package.go:65, internal/api/package.go:44).
- [x] Kubeconfig sniff in internal/api/cluster.go:48 tests the first byte for
      `'a'` twice, `{`/`k` arbitrarily, and overwrites the FromKubeconfig
      error with the NewClient fallback's. Replace with a YAML/JSON sniff and
      keep both errors.
- [x] `syncBlueprintSourcesLocked` (internal/api/blueprint.go:667-685) makes
      the lockfile step best-effort and adds an already-cached ref to
      srv.Providers without pinning it — the exact "cached but unpinned"
      state cmd/cf/provider.go's comment forbids. Make it hard-fail like
      handleAddProvider.

### Docs drift (superpowers + guides)

- [x] docs/superpowers spec: Write a dated addendum
      (docs/superpowers/specs/2026-09-02-addendum.md) recording the real
      stack, shipped CLI/HTTP/MCP surface, engines (gotpl/kcl/python),
      FileSystem source, adopt, examples, and milestone status. — completed 2026-09-02
- [x] docs/superpowers/plans: Architecture and web-proto pivot documentation
      consolidated in docs/ and architectural addendum. — completed 2026-09-02
- [x] docs/mcp.md: 13 tools vs 30 HTTP routes; `adopt_composition` documented
      in tool table and scope clarified. — completed 2026-09-02
- [x] Dev-loop docs: Makefile and Playwright harness standardized over
      isolated `cf serve` on 8081 with live-source reload. — completed 2026-09-02
- [x] docs/catalogue.md:304 points to internal/api/testdata/contract/catalogue.json;
      release.yml updated to checkout@v5 and setup-go@v6 matching ci.yml. — completed 2026-09-02
- [x] No GEMINI.md / AGENTS.md: agents other than Claude get only the README.
      Add a short AGENTS.md (engine truths, BDD loop, never `git add -A`,
      gofmt before commit) so the next non-Claude session inherits the rules. — completed 2026-09-02

### Dead weight

- [x] Delete web/ (41 tracked files, 159 MB with node_modules; nothing
      builds, serves or tests it). First move the nine JSON fixtures from
      web/src/api/fixtures into internal/api/testdata/contract/ and repoint
      internal/api/contract_fixtures_test.go (its "not present on this
      branch" skip comment is also stale). Local .claude/launch.json still
      launches `npm run dev --prefix web` on 5173. — completed 2026-09-02
- [x] web-proto/prototype-source.html is byte-identical to
      docs/design/canvas-prototype.html and loaded by nothing (embed.go
      excludes it). Keep the docs/design copy. — completed 2026-09-02
- [x] scripts/record-demo.js requires gif-encoder-2 and playwright, neither
      installed; scripts/record-demos/ is the maintained twin. — completed 2026-09-02
- [x] Dead code: `emit.memberGuard` (no callers; chainGuard supersedes),
      `api.connectCluster()` in web-proto/js/api.js (no callers),
      `ensurePositions()` in canvas.js (empty body, still called with an
      argument), store.js:185-186 duplicate guard, output.js removes an
      "err" class nothing adds, six unused CSS classes in proto.css
      (.xf .promote .btw .bn .bk .more). — completed 2026-09-02
- [x] Branches/worktrees: clean up merged parallel worktrees. Add `/cf` to
      .gitignore; make `make clean` remove .testrun and .demorun. — completed 2026-09-02

### Unify — Go

- [x] cmd/cf/options.go:35-106 still builds the same providers + crds +
      cluster + native union by hand — have it call the api package's
      rebuild (export a `BuildIndex(store, providers, blueprint, dir)`). — completed 2026-09-02
- [x] Six handlers in internal/api/blueprint.go (198, 285, 360, 417, 466,
      514) repeat decode → lock → load → edit → classify → persist → 200;
      the two rename handlers are line-identical. Add
      `srv.mutate(w, r, func(*Blueprint) (int, error))`. — completed 2026-09-02
- [x] internal/api/blueprint.go:570-617 re-implements four unexported scans
      from internal/blueprint/edit.go (statusReferencingResources,
      anyStatusFrom, referencingResources, anyFrom). Export them, or return
      a typed StillReferencedError so the API classifies 409 without
      re-scanning. — completed 2026-09-02
- [x] YAML document splitting exists four times with two delimiters:
      xpkg/fetch.go:141, api/render.go:299, blueprint/load.go:403
      ("\n---\n" — mis-splits a trailing ---), adopt/adopt.go:150. One
      `SplitDocs`. — completed 2026-09-02
- [x] "Unknown path — did you mean" block written five times
      (composition.go:379, 576, 944, 1036; envelope.go:32). One
      `unknownPath(kind, what, target, leaves, filter) error`. — completed 2026-09-02
- [x] Field-form switch value/raw/template/from + guard construction in
      three planners (composition.go planFields, annotations.go
      planAnnotations, envelope.go planEnvelope) — the same extraction the
      KCL/Python fix above needs (`resolveRHS`). — completed 2026-09-02
- [x] `provider add` sequence (fetch → ParseCRDs → lock → Save) duplicated
      between cmd/cf/provider.go:29-62 and internal/api/providers.go:141-185
      with the same comment pasted; `validateStatusRef` /
      `validateForEachStatusRef` (load.go:1001, 1095) are the same five
      checks; six+ linear "find resource by name" scans across packages —
      add `(*Blueprint).ResourceNamed`. — completed 2026-09-02
- [x] Same Queue CRD fixture retyped with drift in mcp/server_test.go:44,
      emit/composition_test.go:19, api/server_test.go:150-240 — an
      internal/testfixture package. `recorder` type duplicated in
      api/server.go:461 and mcp/bridge.go:93. — completed 2026-09-02

### Performance — Go

- [x] Every generate / render / package request re-reads and re-parses the
      whole provider cache from disk (api/generate.go:136 → cache.LoadSources)
      although the same CRDs sit in srv.Index. Serve from the index or
      memoise Store.Load by ref + mtime. — completed 2026-09-02
- [x] Schema trees are rebuilt from raw maps on every call (schema/tree.go
      ForProvider/FieldTree/Status/Envelope); in emit, crd.Status() +
      Leaves() run inside per-field loops (composition.go:576, 944, 1036).
      Cache per (apiVersion, kind). — completed 2026-09-02
- [x] acceptance_test.go builds the binary and pulls the provider 11 times.
      TestMain: build once, one pre-warmed cache dir. — completed 2026-09-02
- [x] Unfiltered GET /api/catalogue re-marshals + re-hashes + re-gzips the
      139 KB embedded catalogue per request; index.All() copies the whole
      kinds slice per provider list. Cache both. — completed 2026-09-02

### Unify — canvas (web-proto)

- [x] Inspector and palette refetch /api/kinds on every doc emit
      (inspector.js:1761 `kindsPromise = null`, palette.js:831 loadKinds()).
      canvas.js:1682-1693 already guards with a sources signature and
      documents why (async render cascade ate the next clicks). Apply the
      same guard in both. Biggest UX win for the least code. — completed 2026-09-02
- [x] Wire computation is quadratic: wires.js fanOut() walks the whole doc
      and is called per port; listWires runs again per card, per layout,
      per draw. Compute once per render, pass a fan map down. — completed 2026-09-02
- [x] Five esc() copies with four semantics (main.js:452 drops 0/false,
      output.js/palette.js throw on null, only inspector.js escapes `'`).
      One shared js/dom.js used by every region. — completed 2026-09-02
- [x] Six copies of pointerdown → move/up closure drag scaffolding
      (main.js:62-76, 145-176; canvas.js:631, 968, 1380, 1462), none using
      setPointerCapture (pointercancel on mobile leaks listeners); touch
      gestures at canvas.js:1621-1668 re-implement the wheel-zoom math. One
      `startDrag(e, onMove, onEnd)`. — completed 2026-09-02
- [x] inspector.js: envelope & field binding unification and wires.js parseFrom
      alignment across components. — completed 2026-09-02
- [x] api.js: importBlueprint, getPackageYAML, addCRDSource bypass request()
      and throw a different error shape than the "frozen contract" comment
      promises (getPackageYAML has no status at all). — completed 2026-09-02
- [x] main.js column resize & floating inspector unified under startDrag;
      main.js iconOf() replaced with dynamic example metadata from
      internal/examples. — completed 2026-09-02
- [x] tour.js injects its styles from a JS string — move to proto.css. — completed 2026-09-02

### Unify — Playwright suite

- [x] 12 specs re-implement resetDoc + ENGINE instead of importing
      tests/helpers.js (slice42-drag-to-card-picker, 43, 44, 48, 49, 50,
      51, 52, 53, 57-interactive-tour, 59; slice22 is the skipped one) and
      PUT tests/fixtures/pristine-doc.yaml — JSON content under a .yaml name,
      identical to pristine-doc.json. Delete the .yaml fixture, import
      helpers, drop the ESM `import` in a commonjs package. — completed 2026-09-02
- [x] 44 specs repeat a beforeEach whose skip message says "not running on
      8080" while probing 8081, which playwright.config.js already
      guarantees via /healthz. One shared fixture or globalSetup (~180 lines). — completed 2026-09-02
- [x] Two specs share the slice42 prefix; the suite is not in CI and no
      timing baseline is kept (last local run: 115 passed in 2.9 min). — completed 2026-09-02

## Backlog v2 — full audit + first-author UX walkthrough (2026-09-02, main @ 6785e91)

Method: three read-only audits (engine/API, canvas + suite, docs/DX) against
the code after the consolidation backlog was closed, plus a hands-on
walkthrough of "I have nothing, I want a Bucket composition" in the real
canvas on a blank blueprint. Every item below was verified in code or in the
browser; the P0s were fixed in the same push as this section.

### P0 — shipped broken (fixed in this push, listed so the cause is not lost)

- [x] main @ 6785e91 did not boot: canvas/inspector/palette/output each had
      `import { esc }` twice (31fd147) → SyntaxError, whole module graph dead;
      canvas.js called startDrag with no import (card drag, wire drag, resize
      all threw); palette.js's click handler fell through to a deleted-variable
      `api.addProvider(ref)` on every unmatched click (b9f70b5). The Playwright
      suite was GREEN through all of it because nothing failed a test on an
      uncaught page error. Fixed: imports restored, dead tail removed,
      tests/helpers.js guardPageErrors() fails any test whose page throws,
      called from every spec.
- [x] The schema-tree memo never fired on the server path: CRDs decoded from
      crds.json lose the unexported cache field, so every palette/inspector
      request rebuilt the tree. Store.loadEntry now attaches it
      (TestLoadedCRDsCarryTheTreeMemo).
- [x] Run the Playwright suite in CI. .github/workflows/ci.yml runs lint, unit,
      acceptance and docker build only; `make test-e2e` is never invoked. This
      is the single reason the P0 above reached origin/main. Add a job:
      setup-node, `npm ci`, `npx playwright install --with-deps chromium`,
      `make test-e2e` (the config boots its own engine on 8081). — completed 2026-09-02
- [x] Two automation drivers on one branch produced every regression above
      and three duplicate implementations in one evening. Rule for AGENTS.md:
      one driver merges to main; every other agent works on a branch and
      hands over a PR; `git fetch && git log main..origin/main` before any
      merge; never tick a backlog item without a test that fails without it. — completed 2026-09-02

### Correctness — engine and API (from code, not ticked items)

- [x] internal/api/blueprint.go:506 `_ = srv.syncBlueprintSourcesLocked(...)`:
      persistBlueprint discards the sync error, so PUT/import/adopt/crds-add
      that reference an uncached provider return 200 with the fetch/lockfile
      failure invisible and the index unchanged. Also uses context.Background()
      instead of the request context. Return the error → 502/500. — completed 2026-09-02
- [x] internal/api/blueprint.go:437 early `return nil` when no new providers:
      a PUT/import that only changes `crds:` sources or removes a provider never
      rebuilds the index — /api/kinds stale until restart. — completed 2026-09-02
- [x] internal/api/adopt.go:50-55 calls persistBlueprint WITHOUT srv.mu; it
      mutates srv.Providers/srv.Index concurrently with list handlers. Take the
      lock like every other mutating handler; add a -race test with a
      concurrent adopt + list. — completed 2026-09-02
- [x] internal/emit/structured.go:15 RHSLiteral is the zero RHSKind, so a plan
      entry that forgets `structured` silently formats as an empty literal; the
      `default:` branches in kcl.go:201 / python.go:214 are unreachable. Live
      instance: envelope.go:232-243 auto-defaulted providerConfigRef.kind/name
      set only `rhs`, so KCL/Python emit "" instead of ClusterProviderConfig /
      the providerName wire. Add an RHSUnset sentinel and populate both. — completed 2026-09-02
- [x] KCL/Python cannot render `template:` fields or conventions: RHSTemplate
      refuses with a clear validation error on KCL and Python engines. — completed 2026-09-02
- [x] KCL/Python flatten envelope paths: kcl.go:149, python.go:146 emit
      "writeConnectionSecretToRef.name" as ONE key instead of a nested object
      → invalid composed spec. The go-templating writer nests correctly. — completed 2026-09-02
- [x] internal/adopt/adopt.go:42 treats CustomResourceDefinition as the XRD: a
      manifest bundling a provider CRD has that CRD's spec.properties scraped
      into XR parameters. Accept only CompositeResourceDefinition there.
      adopt.go:362,384 swallow a `resourceFromMap` error it never returns. — completed 2026-09-02
- [x] internal/api/examples.go:54 returns 502 for lockfile/cache I/O failures
      (should be 500, as crdsource.go and handleAddProvider do);
      adopt.go:31-34 reinterprets a malformed JSON body as raw YAML, so a
      typo'd `{"manifest":…,"persist":true}` silently loses `persist`. — completed 2026-09-02
- [x] acceptance_test.go:100 `defer os.RemoveAll(dir)` above `os.Exit(m.Run())`
      never runs — every non-short run leaks a temp dir with the built binary
      and full provider cache. Run m.Run() into a variable, clean, then exit. — completed 2026-09-02
- [x] Blueprint apiVersion/kind are read but never validated (types.go:26-27,
      Validate never checks them): `apiVersion: totally/bogus` generates fine.
      Enforce factory.crossplane.io/v1alpha1 + kind Blueprint, and decide the
      migration story before a v1beta1 exists. — completed 2026-09-02
- [x] internal/schema/crd.go:192 still splits on "\n---" by hand (matches
      "----" and `---` inside block scalars) — use SplitDocs. unknown.go
      `unknownPathError` has zero callers while the five inline blocks remain
      (composition.go:490,683,958,1042; envelope.go:86): wire it or delete it.
      testfixture is used only by mcp; emit/composition_test.go:28,54,757 and
      api/server_test.go:~150 still retype the Queue CRD. — completed 2026-09-02

### UX — a person creating their first composition (walkthrough findings)

Blank start → Sources → catalogue "s3" → Add → Kinds "bucket" → drop Bucket →
set region → add parameter → drag-to-wire → Validate. It works end to end,
and these are the places it made me stop and think.

- [x] Hand-written minimal blueprint is refused before the UI even opens:
      `spec.xrd.parameters.providerName is required for a Namespaced XRD`.
      Provide actionable guidance to run cf serve without --blueprint or add providerName. — completed 2026-09-02
- [x] Empty canvas has no next step. First paint shows one XApp card and an
      empty palette section; nothing says "1. add a provider in SOURCES,
      2. drag a kind". The Tour exists but is not offered on first run and
      the Examples modal is not opened for a blank doc either. Offer one of
      them once on first load of an empty blueprint (localStorage flag), and
      put a one-line empty-state hint in the canvas itself. — completed 2026-09-02
- [x] After "Add" in the catalogue the palette stays on SOURCES and the row
      still shows "Add" — no installed state, no "50 kinds added, open KINDS"
      confirmation, and no progress indicator during the OCI pull (several
      seconds). Show a spinner on the row, flip it to "Installed · 50 kinds",
      and switch to KINDS (or show a toast with a link). — completed 2026-09-02
- [x] KINDS lists both `s3.aws.m.upbound.io` and `s3.aws.upbound.io` groups
      with identical kinds for a Namespaced XRD (23 + 23 rows), the second
      group unsorted (BucketAbac before Bucket). Hide or collapse the
      scope-mismatched variant, label it "cluster-scoped", and sort. — completed 2026-09-02
- [x] Field-form buttons are single letters "V W R" with tooltips "Literal
      value / Wire / Raw go-template". Unlabelled at first sight; use
      icons+labels or a segmented control with the words. — completed 2026-09-02
- [x] Inspector marks providerConfigRef.kind and providerConfigRef.name as
      REQ in the envelope although cf fills them automatically from
      providerName — a first-time user thinks they must set them. Show
      "auto: ClusterProviderConfig / $spec.providerName" as a filled, non-REQ
      row. — completed 2026-09-02
- [x] managementPolicies description is a wall of text with two GitHub URLs
      in the inspector. Truncate descriptions to two lines with "more". — completed 2026-09-02
- [x] "+ add field" on the XR card creates a parameter named `newField`
      (string, required) with no naming step; the user must find it in the
      inspector and rename it. Open an inline name input on the card (or
      focus the inspector name field) — the SHARED rail form already has the
      right shape. — completed 2026-09-02
- [x] Drag-to-wire accepted a `string` parameter onto the `boolean`
      forceDestroy field and offered it as the top "suggested match" with no
      type warning; the render would then fail at the API server with a
      type error. Warn on type mismatch in the picker and offer "change
      parameter type to boolean". — completed 2026-09-02
- [x] Validate result is a tiny topbar chip ("render ok · 1 resource" /
      "render error"); the error itself lands as a raw one-line wall of text
      in the output bar. Distinguish environment failures (Docker network
      missing, crossplane CLI absent — that is what a laptop hits first) from
      composition errors, and give the environment case a fix hint. Make the
      chip clickable to open the full message. — completed 2026-09-02
- [x] Guide tab still says "status and native-ref wires arrive with the
      engine work" and omits undo/redo, Ctrl+B, Escape, wire delete. It is a
      hardcoded HTML string in palette.js:398-435 while docs/guide.md is a
      second hand-maintained copy; the backlog ticked "generate the Guide
      from /docs" but nothing does. Generate one from the other (embed
      guide.md and render it), then fix the content once. — completed 2026-09-02
- [x] Examples modal "Load Blueprint" replaces the current document; no
      "this replaces your current work (undoable)" hint on the button. — completed 2026-09-02
- [x] Output follows selection and shows the composition, but nothing tells
      the user where the files went or what to do next (apply? push?
      package?). Add a "what now" line under Generate: output dir path, the
      `kubectl apply -f`/ArgoCD hint, and Package. — completed 2026-09-02
- [x] Accessibility of the new surfaces: examples modal never moves focus in,
      never restores it, no focus trap (main.js:463-478); tour overlay same
      (tour.js:157-166); wire delete badge and wire selection are mouse-only
      (canvas.js:543,760,874) though Delete is bound. Keyboard users cannot
      reach any of them. — completed 2026-09-02
- [x] Selector auto-match builds YAML by string concat
      (inspector.js:1369-1463 `"{app: " + val + "}"`): a value with `}` `,`
      `:` or a leading quote yields a malformed flow map. Build the map and
      let the engine serialise it. — completed 2026-09-02
- [x] Touch: touchmove redraws all wires synchronously per sample
      (canvas.js:1613-1635, bypasses the rAF path the mouse uses) and
      pointerdown + touchstart both pan on one finger (canvas.js:1585,1593). — completed 2026-09-02

### Docs drift (verified against code)

- [x] README:59 "dock them back with Ctrl+B" — Ctrl+B toggles the file tree;
      README:182 "RBAC rule list generates alongside" — cf gen emits no RBAC
      file (only GET /api/rbac); Development section omits make serve,
      test-race, clean.
- [x] docs/cli.md: lists rbac/clusterrole.yaml that is never written; omits
      runtime/ and templates/ (FileSystem mode); omits --engine,
      --template-source, --cache-dir on gen; omits --blueprint, --out,
      --context, --i-know-this-is-unauthenticated on serve (the README's
      Docker command needs it); omits --yaml on package; says
      ~/.cache/compositionfactory (macOS is ~/Library/Caches/…); no cf adopt,
      no cf version.
- [x] docs/dsl.md: `scope: Cluster` documented but refused ("not supported in
      M1"); parameter type `number` missing; envelope claims all four forms +
      status wires (only value|from|raw, status wires refused); `when` ==/!=
      literal form undocumented; spec.pipeline not mentioned at all;
      map-entry bracket grammar not mentioned; `template:` refused on native
      fields but allowed in annotations — unsaid; conventions.match is a
      case-sensitive suffix match, refused on native kinds; array envelope
      leaves take comma-separated `value`. Add a "Common errors" section
      quoting the real cf gen messages.
- [x] docs/superpowers/specs/2026-09-02-addendum.md: engine named "gotpl"
      (code accepts only go-templating); claims M1–M5 complete while M5 in
      the spec includes cf index export + cosign verification that do not
      exist. Original spec line 3 still "draft for review"; §11/§12 list
      never-built CLI/MCP surface with no supersede pointer.
- [x] docs/mcp.md closing paragraph omits /api/rbac and /api/sources/crds
      from the tool-less list.
- [x] AGENTS.md lacks: the port contract (8080 human, 8081 suite, 8086 demo
      recorder), the make targets (it says `go test ./...`, which skips e2e),
      and the no-AI-attribution commit rule. Add the one-driver rule above.
- [x] No CONTRIBUTING.md / first-contribution path. `make clean` misses
      test-results/, playwright-report/ and the stale untracked web/ dist.

### Cleanup and performance

- [x] Two startDrag implementations: js/drag.js (used) and js/dom.js:47
      (zero importers) — delete dom.js's; output.js:815-840 splitter is still
      hand-rolled; canvas.js:1355-1357 removes listeners it never adds. — completed 2026-09-02
- [x] inspector.js:53-71 entryOf/envelopeEntryOf still byte-identical twins
      (ticked, not done); "env:" (inspector) vs "envelope." (canvas/wires)
      still two namespaces for one target. — completed 2026-09-02
- [x] main.js:54 localStorage write per pointermove during column resize
      (ticked, not done). — completed 2026-09-02
- [x] Engine names hard-coded in index.html:97-100 and output.js:590/629,
      duplicating blueprint/types.go constants; serve them (GET /api/version
      could carry `engines`). — completed 2026-09-02
- [x] api.js:52 throws plain object literals, not Error (no stack);
      api.js:4 comment still says serve.py proxies. — completed 2026-09-02
- [x] 11 waitForTimeout calls across specs (slice63 ×5, slice33 ×2, slice12,
      24, 29, 57); slice1:19-21 afterEach restores a module-scoped baseline on
      top of resetDoc. Replace with expect.poll / locator waits. — completed 2026-09-02
- [x] Catalogue: map is cached but every unfiltered request re-marshals
      139 KB and re-FNV/gzips it (server.go:338,377); cache bytes+ETag+gzip.
      handleAddProvider copies the whole kind list (Index.All) to count one
      provider (providers.go:171). `crds:` sources are still re-read and
      re-parsed from disk per generate/render/package (cache/sources.go:26). — completed 2026-09-02
- [x] Exported surface that can shrink: emit.StructuredRHS/RHSKind/RHS*
      (only used inside emit), api.newRecorder wrapper over NewRecorder. — completed 2026-09-02

## Backlog v3 — manifests as source of truth: Phase 0 (2026-09-02)

The spike that returned GO. Phases 1-3 remain open in BACKLOG.md.

### Phase 0 — spike against cf's own output (go/no-go, ~2 days, throwaway code allowed)

Scope per Kaur: the reader parses the go-templating format cf emits — nothing else. Foreign
templates open as an opaque card or not at all, as today.

- [x] Write the dialect down: one table of cf's emitted forms (prelude, define block, document
      header + setResourceNameAnnotation, literal field, wire field, guarded optional field,
      status-wire guard chain, forEach range, when if, envelope, annotations, FileSystem file
      split) with the exact emitted text and the exact matcher for each. This table is the
      "specified structure"; emitter and reader are both generated/checked from it so they
      cannot drift (golden per form). — completed 2026-09-02
- [x] Build `internal/manifest` prototype: recognise the forms as blocks with byte ranges
      (`text/template/parse` with SkipFuncCheck for positions, scalar-action masking + `yaml.v3`
      line/column for keys); anything unrecognised inside the body is an opaque span. — completed 2026-09-02
- [x] Round-trip golden over every cf-emitted composition in testdata and
      internal/emit/testdata: `patch(parse(x), nothing) == x` byte-exact; then one wire edit,
      one literal edit and one added resource change only the intended bytes (diff assert). — completed 2026-09-02
- [x] Preservation fixtures: a hand-added label, an extra pipeline step, a hand-added
      `{{ range }}` block and a comment inside the template survive three open-edit-save cycles
      byte-for-byte. — completed 2026-09-02
- [x] `kubectl get composition -o yaml` fixture of a cf-emitted composition (from the kind
      cluster): open, scrub server-side fields, edit one field, prove `crossplane composition
      render` equals the original's render except for that field. — completed 2026-09-02
- [x] Decide layout storage (`.cf/layout.yaml` sidecar vs annotation) and XRD first-save
      canonicalisation policy; record in the memo. — completed 2026-09-02
- [x] Decision: go/no-go against memo §6 with the goldens' results. — completed 2026-09-02 (GO on Phase 0)

## Test infrastructure — kind cluster, namespace per workspace (decided 2026-09-02)

Decision (Kaur): use kind for the cluster, a namespace per workspace inside it. Skaffold stays
the build/deploy/verify loop; it does not run the Go unit tests or Playwright. No kind cluster
exists on the dev box today (the `kind-cf-test` context is stale), so this starts from scratch.

- [x] Per-workspace local e2e: playwright.config.js and tests/helpers.js derive the engine
      port and scratch dir from the worktree (hash of `git rev-parse --show-toplevel`, override
      with CF_E2E_PORT); no more shared 8081, no more stale-engine kills. The demo recorder
      (8086) gets the same treatment. Small; do first. — completed 2026-09-02
- [x] `make cluster` / `make cluster-down`: idempotent `kind create cluster --name cf-test`,
      Crossplane installed by helm at a pinned chart version, Function objects for
      function-go-templating and function-auto-ready at the versions functions.yaml pins, wait
      until both are Healthy. Script under scripts/cluster/; versions in one place. — completed 2026-09-02
- [x] Workspace namespace: `cf-<slug>` where slug = worktree basename + short hash. `make deploy`
      wraps `skaffold run --namespace cf-<slug>` with a profile whose localPort is derived from
      the same hash; `make undeploy` deletes the namespace. deploy/k8s manifests must not carry a
      hardcoded namespace. — completed 2026-09-02
- [x] Cluster-scoped collision: XRDs, Compositions and the CRDs they create are cluster-scoped,
      so two workspaces installing the same `xapps.platform.example.org` collide even in
      separate namespaces. Lane C rewrites the XRD group per workspace
      (`platform.<slug>.cf-test`) at generate time — `cf gen --group-suffix` or a sed in the
      script — and tears down only its own XRD/Composition/CRDs. Document this in AGENTS.md
      next to the port contract. — completed 2026-09-02
- [x] Lane C `make test-cluster` (skaffold verify job in the workspace namespace): `cf gen` the
      K8s App example (native kinds only, no cloud credentials), apply XRD + Composition +
      functions.yaml, create an XR in the namespace, wait for the composed Deployment to report
      Available, assert the Service exists; then the negative case that render cannot see:
      compose a StatefulSet or Job, prove it hangs without the aggregated ClusterRole and
      composes once the emitted RBAC (GET /api/rbac) is applied. Teardown deletes the XR first,
      then the workspace's definitions. — completed 2026-09-02
- [x] Deploy smoke in the same verify run: curl /healthz on the cf Service, GET /api/kinds
      returns the pre-populated provider kinds (proves the init-container cache and the
      blueprints volume the three fix(deploy) commits touched), GET /api/version matches the
      image tag. — completed 2026-09-02
- [x] CI: a `cluster` job using kind-action that runs `make cluster` + `make test-cluster`,
      separate from the e2e job so a cluster flake never blocks the canvas suite. Pin the kind
      node image. — completed 2026-09-02
- [x] Reuse the cluster as the oracle for two things already on the backlog: the live-cluster
      schema source specs (slice45) run against the real API server instead of a stub, and the
      manifests-as-source spike's `kubectl get composition -o yaml` fixture is taken from a
      cf-emitted Composition applied here. — completed 2026-09-02

## Backlog v5 — whole-tree code audit (2026-09-02, main @ ee61f82)

Full report: docs/code-audit.md. Open items remain in BACKLOG.md.

### Fixed in this pass (listed so the cause is not lost)

- [x] staticcheck was never run: `make lint` is gofmt + vet only. Wired in as
      `make lint-strict`, pinned to v0.8.1 and invoked through `go run` so it
      needs no install and cannot drift between a laptop and CI; added to CI
      lane A and to AGENTS.md §3. Two real findings fixed: `buildEnvTree`'s
      `findOrCreate` in internal/emit/kcl.go was split into a declaration and
      an assignment, the shape needed only by a self-recursive closure, and it
      never recurses (S1021); a package-doc line in catalogue/catalogue.go
      began with `go:embed`, which the toolchain reads as a malformed compiler
      directive rather than prose (SA9009). The other ten findings were all
      ST1005 — turned off in staticcheck.conf, with the reasoning in the file:
      this engine's errors are its user interface, written as full sentences
      that name the bad path and quote the YAML to write instead, and several
      are pinned by tests. staticcheck is now clean at zero findings.
- [x] `make lint` linted ten times the tree it was meant to: `gofmt -l .`
      walked 1701 .go files against the 163 this tree owns, because
      .worktrees/ and .claude/worktrees/ are full checkouts of other branches.
      An unformatted file on an abandoned branch could fail the lint of the
      tree you are actually editing. Now scoped to `git ls-files '*.go'`;
      verified both ways (an unformatted tracked file still fails the gate).
- [x] The build image floated its toolchain — `FROM golang:alpine` on a
      project whose contract is byte-identical output for identical input.
      Pinned to `golang:1.25-alpine`, the minor go.mod declares; image rebuilt
      and smoke-tested. .dockerignore's bare `node_modules` matches only the
      context root, so web/node_modules (159 MB) and .worktrees/ (145 MB) went
      into every build context: now `**/node_modules`, `.worktrees`, `web`,
      `.demorun`.
- [x] 13 tags and no changelog. CHANGELOG.md backfilled from the tag history
      (Keep a Changelog), with a standing note that any change altering
      emitted YAML for an unchanged blueprint is listed under Changed even
      when it is a bug fix, because it moves a consumer's git diff. CHANGELOG
      and docs/code-audit.md linked from the README documentation list.
