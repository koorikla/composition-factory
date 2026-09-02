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
- [ ] go-templating FileSystem source export (user request 2026-09-02): an
      export option that emits templates as a folder structure (one object
      per yaml file, helm-chart-style readability), wired for `source:
      FileSystem` + ConfigMap mounts, splitting across multiple ConfigMaps
      to stay under the ~1MiB object limit for big compositions.
- [x] Docs restructure (user request 2026-09-02): split README extras into
      /docs to keep the main README quick to read; make the gif-recorder
      flow part of it; use /docs content to GENERATE the in-app Guide tab(s)
      and improve them. — completed 2026-09-02
- [x] Startup example chooser (user request 2026-09-02): pick between a few
      starting blueprints on first load — IRSA, an RDS composition, and a
      k8s app composition that uses both. — completed 2026-09-02
- [ ] Ansible provider support (user backlog 2026-09-02): explore converting
      a runbook/role into a composition.
- [ ] (back-backlog) Generate compositions in other languages — KCL, Python
      (function-kcl, function-python et al.) as alternative emitters.
