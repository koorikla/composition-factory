# Canvas slice backlog (BDD loop: spec → GUI → backend → verify)

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

- [ ] docs/superpowers spec: line 3 still "Status: draft for review"; §3
      stack still React 19 + xyflow + rjsf + CodeMirror; §11 lists CLI
      commands that never shipped (provider search/list/versions/info/pin,
      index, k8s use, validate) and omits package/push/adopt; §12 milestone
      table has nothing marked done. Write a dated addendum
      (docs/superpowers/specs/2026-09-02-addendum.md) recording the real
      stack, shipped CLI/HTTP/MCP surface, engines (gotpl/kcl/python),
      FileSystem source, adopt, examples, and milestone status.
- [ ] docs/superpowers/plans/2026-08-28-m3-canvas.md describes the deleted
      React canvas as the frozen contract — add a "superseded 2026-09-01 by
      the web-proto pivot" header.
- [ ] docs/mcp.md: 13 tools vs 30 HTTP routes; `adopt_composition` is not in
      the tool table and "the one HTTP route without a tool" is false
      (resource rename/delete, provider delete, import, package, crds
      sources, cluster, rbac, catalogue, examples have no tool). Either add
      the tools (bridge makes each a few lines) or state the scope honestly.
- [ ] Dev-loop docs point at serve.py: Makefile:22-23 and
      playwright.config.js:2 say the suite starts serve.py and needs cf serve
      on :8080 (it boots its own cf serve on 8081); web-proto/README.md
      tells you to run serve.py on 5180; tests/slice1-core-loop.spec.js:7
      same. `cf serve` already serves live source with no-store headers, so
      retire serve.py + `make dev`, fix the three comments, and delete or
      rewrite slice22-cache-selfheal (permanently skipped, probes 5180).
- [ ] docs/catalogue.md:304 points at web/src/api/fixtures (see removal
      below); release.yml pins checkout@v4/setup-go@v5 while ci.yml and
      catalogue.yml use v5/v6.
- [x] No GEMINI.md / AGENTS.md: agents other than Claude get only the README.
      Add a short AGENTS.md (engine truths, BDD loop, never `git add -A`,
      gofmt before commit) so the next non-Claude session inherits the rules.

### Dead weight

- [x] Delete web/ (41 tracked files, 159 MB with node_modules; nothing
      builds, serves or tests it). First move the nine JSON fixtures from
      web/src/api/fixtures into internal/api/testdata/contract/ and repoint
      internal/api/contract_fixtures_test.go (its "not present on this
      branch" skip comment is also stale). Local .claude/launch.json still
      launches `npm run dev --prefix web` on 5173.
- [x] web-proto/prototype-source.html is byte-identical to
      docs/design/canvas-prototype.html and loaded by nothing (embed.go
      excludes it). Keep the docs/design copy.
- [x] scripts/record-demo.js requires gif-encoder-2 and playwright, neither
      installed; scripts/record-demos/ is the maintained twin.
- [x] Dead code: `emit.memberGuard` (no callers; chainGuard supersedes),
      `api.connectCluster()` in web-proto/js/api.js (no callers),
      `ensurePositions()` in canvas.js (empty body, still called with an
      argument), store.js:185-186 duplicate guard, output.js removes an
      "err" class nothing adds, six unused CSS classes in proto.css
      (.xf .promote .btw .bn .bk .more).
- [ ] Branches/worktrees all 0 ahead of main: engine-mvp, canvas-parity,
      worktree-agent-a8f64090cfffd3a90, worktree-agent-aed01c6e7b850f5a9,
      worktree-kcl-emitter, worktree-selectors-functions,
      worktree-startup-examples; blank-start-guide's feature commit is on
      main as 4b81974. worktree-fs-export is still locked by a live Claude
      session — leave it. Add `/cf` to .gitignore; make `make clean` remove
      .testrun and .demorun.

### Unify — Go

- [x] cmd/cf/options.go:35-106 still builds the same providers + crds +
      cluster + native union by hand — have it call the api package's
      rebuild (export a `BuildIndex(store, providers, blueprint, dir)`). — completed 2026-09-02
- [x] Six handlers in internal/api/blueprint.go (198, 285, 360, 417, 466,
      514) repeat decode → lock → load → edit → classify → persist → 200;
      the two rename handlers are line-identical. Add
      `srv.mutate(w, r, func(*Blueprint) (int, error))`.
- [x] internal/api/blueprint.go:570-617 re-implements four unexported scans
      from internal/blueprint/edit.go (statusReferencingResources,
      anyStatusFrom, referencingResources, anyFrom). Export them, or return
      a typed StillReferencedError so the API classifies 409 without
      re-scanning.
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

- [ ] Every generate / render / package request re-reads and re-parses the
      whole provider cache from disk (api/generate.go:136 → cache.LoadSources)
      although the same CRDs sit in srv.Index. Serve from the index or
      memoise Store.Load by ref + mtime.
- [x] Schema trees are rebuilt from raw maps on every call (schema/tree.go
      ForProvider/FieldTree/Status/Envelope); in emit, crd.Status() +
      Leaves() run inside per-field loops (composition.go:576, 944, 1036).
      Cache per (apiVersion, kind). — completed 2026-09-02
- [ ] acceptance_test.go builds the binary and pulls the provider 11 times.
      TestMain: build once, one pre-warmed cache dir.
- [x] Unfiltered GET /api/catalogue re-marshals + re-hashes + re-gzips the
      139 KB embedded catalogue per request; index.All() copies the whole
      kinds slice per provider list. Cache both. — completed 2026-09-02

### Unify — canvas (web-proto)

- [x] Inspector and palette refetch /api/kinds on every doc emit
      (inspector.js:1761 `kindsPromise = null`, palette.js:831 loadKinds()).
      canvas.js:1682-1693 already guards with a sources signature and
      documents why (async render cascade ate the next clicks). Apply the
      same guard in both. Biggest UX win for the least code.
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
      `startDrag(e, onMove, onEnd)`.
- [ ] inspector.js: entryOf/setField/commitValue and their envelope twins
      are pairwise identical bar `fields` vs `envelope`; the bound-row
      markup is pasted four times; field-path parsing is inline string
      prefix checks instead of wires.js parseFrom; the target namespace is
      "env:" in inspector.js but "envelope." in canvas.js. Pick one.
- [x] api.js: importBlueprint, getPackageYAML, addCRDSource bypass request()
      and throw a different error shape than the "frozen contract" comment
      promises (getPackageYAML has no status at all). — completed 2026-09-02
- [ ] main.js column resize writes localStorage on every pointermove and
      the resize listener is unthrottled; inspector rebuilds its whole panel
      with innerHTML on every doc emit (the pattern canvas.js abandoned for
      selection stability). [x] main.js:441 iconOf() hardcodes the example IDs
      from internal/examples — serve the icon with the example.
- [x] tour.js injects its styles from a JS string — move to proto.css.

### Unify — Playwright suite

- [ ] 12 specs re-implement resetDoc + ENGINE instead of importing
      tests/helpers.js (slice42-drag-to-card-picker, 43, 44, 48, 49, 50,
      51, 52, 53, 57-interactive-tour, 59; slice22 is the skipped one) and
      PUT tests/fixtures/pristine-doc.yaml — JSON content under a .yaml name,
      identical to pristine-doc.json. Delete the .yaml fixture, import
      helpers, drop the ESM `import` in a commonjs package.
- [ ] 44 specs repeat a beforeEach whose skip message says "not running on
      8080" while probing 8081, which playwright.config.js already
      guarantees via /healthz. One shared fixture or globalSetup (~180 lines).
- [ ] Two specs share the slice42 prefix; the suite is not in CI and no
      timing baseline is kept (last local run: 115 passed in 2.9 min).
