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
- [ ] Pipeline steps in the GUI (after the engine lands spec.pipeline): show the
      step chain in the Output/topbar area, one-click "add auto-ready", raw
      input editing per step. — follows the functions-support engine work
- [ ] Envelope field control: per-resource authoring of the Crossplane-native
      envelope — writeConnectionSecretToRef (name/namespace), managementPolicies,
      and whatever else the kind's .m. CRD envelope actually carries (validated
      against Envelope(), same {value|from|raw} field forms). GUI: an "envelope"
      section on the card/inspector. — user request 2026-09-01
- [ ] Cross-object atProvider wiring GUI (engine side in flight): wire e.g. a
      postgres provider field from an RDS instance's status.atProvider output —
      teal status wires, pickable from the source card's status section.
- [ ] Provider detail view: click a provider in SOURCES → full registry ref shown,
      its kinds listed with checkboxes to select which appear in the KINDS rail
      (client-side filter, persisted). — user request 2026-09-02
- [ ] Catalogue must cover upjet family services (provider-aws-rds et al. — repo
      enumeration misses monorepo-published packages). — user request 2026-09-02
- [x] Generate ProviderConfig scaffolds to out/providerconfigs/. — user request 2026-09-02
- [ ] Live-cluster schema source: run against a kind/k3s (or any) cluster's API
      to dynamically discover CRDs/kinds beyond packaged providers — the
      "external schema" phase of the control-plane direction. Big item.
      — user request 2026-09-02
- [ ] Right-click context menu on canvas objects (duplicate/remove/rename/bind…)
      — improve beyond the browser default. — user request 2026-09-02
- [ ] KINDS hover preview: a small card with the kind's description + a few key
      fields when hovering a palette row. — user request 2026-09-02
- [ ] Effective requiredness for the inspector: the Required filter must show
      what a user actually must set — top-level required branches (Deployment's
      selector/template) surfaced as expandable required rows, and leaf
      requiredness conditioned on its ancestor chain (EnvVar.name is required
      only once env exists). Engine: tree.go/API change; GUI: Required filter
      semantics. Regression net already merged (required_test.go). Blocked on
      the engine-batch integration landing (tree.go is hot).
