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
- [ ] forEach on a resource ("for objects"): N instances driven by a parameter
      (e.g. RDS cluster + $instanceCount ClusterInstance nodes), gotpl range
      semantics per the design spec, setResourceNameAnnotation indexed in the
      loop (spec §8), GUI badge becomes authorable. ENGINE + GUI slice.
      — user request 2026-09-01 ("for nf or nodes for rds")
- [ ] Provider actions in SOURCES: expandable info per provider (digest, version,
      kind list, registry host), and remove — needs DELETE /api/providers with a
      409 naming referencers when the blueprint still uses it. More actions as
      they come. — user request 2026-09-01
