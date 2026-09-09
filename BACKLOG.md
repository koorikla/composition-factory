# Backlog

Open work only — concise, prioritized, and verified against the codebase.

Completed work is archived in [docs/backlog-archive.md](docs/backlog-archive.md); full history is in `git log -p BACKLOG.md`.

Each open item carries a permanent `CF-NNN` id. Ids are never reused, including after
archival. When an item is dispatched it gets a self-contained brief at
`docs/tasks/CF-NNN-<slug>.md`; the agent executing it works to
[docs/task-execution-contract.md](docs/task-execution-contract.md). Filing new items
is `.claude/skills/backlog-authoring/`.

---

## Architectural Principle: DSL (`.cf.yaml`) as Canonical Intermediate Representation

The `factory.crossplane.io/v1alpha1` `Blueprint` document (`.cf.yaml`) is and remains the single source of truth and intermediate representation (IR) for `composition-factory`. 

All user interfaces (Canvas, CLI, API, MCP) operate on this model. Crossplane manifests (`composition.yaml`, `definition.yaml`, `functions.yaml`, `package.yaml`) are deterministic, generated artifacts. Manifest import and adoption act as high-fidelity converters *into* the canonical Blueprint format.

---

## Architectural Principle: The Round-Trip Rule

**Anything cf generates must survive Kubernetes and come back.** Apply it to a
real cluster, read it back with `kubectl get <kind> -o yaml`, and cf must be
able to import that — the server-round-tripped form, not just the file cf
wrote. The API server defaults fields, reorders maps, injects `managedFields`,
`creationTimestamp`, `uid`, `resourceVersion` and `status`, and prunes anything
the schema does not know; an importer that only reads cf's own output has not
been tested against the only version of the document that matters operationally.

Recorded in AGENTS.md §1 as an Engine Truth, so it binds every agent and not
just this backlog.

This is the acceptance bar for Track 1, and it is testable rather than
aspirational: `cf gen` → `kubectl apply` → `kubectl get -o yaml` → `cf import`
→ `cf gen` must reproduce the original bytes, with the server-added fields
scrubbed and named in a loss report. Lane C already stands up the kind cluster
this needs on every push, so the oracle exists — it just is not pointed at this
yet. Any generated artifact that cannot make the trip is a bug in the emitter,
not an exception for the importer to special-case.

---

## Open — 2026-09-04 canvas UX run (at `0d3e914`)

Severities below are the **UX** scale, not the engine scale above: **P0** lost work,
impossible, or the interface states something false · **P1** completable only with knowledge
that exists solely in the source · **P2** completable, wastefully · **P3** polish.

Narrative, measurements and screenshots:
[docs/ux-runs/2026-09-04-m1-first-contact.md](docs/ux-runs/2026-09-04-m1-first-contact.md).
No spec is written for any of these yet; the report lists the anchors and the order to write
them in.

### P0

- [ ] **CF-050 — The core loop is pointer-only: a keyboard or touch user cannot place a kind
      or select a card. [V]** Palette rows are `draggable` `<div>`s — 46 rows, 0 tabbable, 0
      with a role, 0 focusable children — and `onDrop` (`canvas.js:1724`) is the only code
      that appends a resource. `proto.css:605-606` `display:none`s a card's buttons until it
      is already selected, so the Inspector is pinned to the XRD panel. The only touch
      handlers are canvas pan/zoom (`canvas.js:1753`). The fix must make adding a kind and
      selecting a card succeed without a precise pointer; drag stays as the pointer path.


### P1

- [ ] **CF-055 — An optional parameter wired into a required provider field renders invalid,
      and the error blames the field. [V]** Wire `$region` (optional) to a Bucket's required
      `spec.forProvider.region` and Validate fails with `line 47: … missing required field
      "spec.forProvider.region"` while the canvas shows the wire drawn, the Inspector shows
      `+ params.region`, and the port's required marker is satisfied. The cause is the
      emitter's `hasKey` guard for optionals; the cure is the `req` checkbox on a different
      object in a different panel. The fix must flag the mismatch at bind time, on the object
      that can be changed.


- [ ] **CF-057 — Generate overwrites files on disk and nothing says so beforehand.** The
      tooltip (`index.html:33`) says "Regenerate … now"; the handler writes every path in
      `srv.OutDir` with `os.WriteFile`, no confirmation, no backup, including over another
      blueprint's output. The word "write" appears only afterwards, in a banner, and only if
      the drawer is open. The fix must name the destination and the overwrite before the act.

- [ ] **CF-059 — Raw Go errors and HTTP statuses reach the user verbatim.** `api.js:49`
      surfaces `network error: Failed to fetch`; `api.js:60` falls back to a bare
      `502 Bad Gateway`; `inspector.js:305` shows the literal `operation failed`. Server
      messages arrive addressed in the blueprint's coordinate system (`spec.resources[3] …`)
      after the user renamed a card. The pattern to copy is already here — `diagnoseError`
      (`output.js:368-399`) keeps the server text and appends the fix.

- [ ] **CF-060 — The dark theme's `--faint` was never re-derived; 30 AA failures, including
      the Generate button at 2.69:1. [V]** Every ink token inverts between themes except
      `--faint` (45.1% → 45.7% lightness), which lands at 3.64:1 on `--surface` across
      sixteen selectors; `#fff` is hardcoded against accents that were lightened for dark, so
      `.btn.pri` is 2.69:1 and `.fan` is 2.16:1 at 9 px. Measured: 17 AA text failures light,
      30 dark, 10 UI-boundary failures each. Values are inherited unchanged from
      `docs/design/canvas-prototype.html` — **zero token drift** — so the prototype must be
      fixed with `proto.css` or the drift reopens.


### P2

- [ ] **CF-063 — At 1280×720 the palette is 179 px and truncates every long kind name to the
      same prefix. [V]** `#lrail` measures 179×277 — nine rows of forty-six. `bucket` yields
      `BucketAnaly…`, `BucketCorsC…`, and two rows both reading `BucketObject…`; the
      catalogue's three `s3` matches all render as `provider-aws-…`. The column is not
      resizable — dragging its edge selects text in the YAML viewer. Long CRD kind names are
      the normal case for upjet providers. The fix must let a user tell two rows apart.

- [ ] **CF-064 — The output drawer takes 250 px of a 720 px viewport to show 142 px of
      YAML. [V]** Measured at 1280×720: topbar 46, columns 424, drawer 250; inside it `#code`
      is 142 px — seven lines of a ~500-line document, so 43% of the drawer is chrome. The
      canvas gets 840×424 and the palette 277 px of list. The fix must give the authoring
      surfaces the vertical space, at this viewport and smaller.

- [ ] **CF-065 — Clicking a palette kind row does nothing, and nothing says drag is
      required. [V]** The delegated click handler (`palette.js:696`) has thirteen `closest()`
      branches and none matches `.kind`. The hint that says "Drag a kind onto the canvas" sits
      below the fold of a 277 px list. The fix must make the first instinct — click the thing
      you want — either work or explain itself.

- [ ] **CF-066 — The button says Validate, every result says "render", and the generate chip
      then erases it.** `Validate` (`index.html:32`) yields `rendering…` / `render ok · N
      resources` / `render error` (`output.js:740-754`), and the same element is the generate
      status chip — so a successful result is overwritten by the debounced preview 300 ms
      after the next keystroke. The fix must keep one vocabulary and let the result survive.

- [ ] **CF-067 — The light-theme code viewer fails AA on four of five syntax colours.** All
      on `--sunk` `#D8E0EA`: template 3.55:1, shared 3.55:1, comment 3.68:1, key 4.46:1; only
      strings pass. All five pass in dark, so the light theme is the one never measured. The
      same 4.46:1 pair breaks the raw-expression editor and the selected artifact-tree row.
      The fix must choose the light accents against `--sunk`, not against white.

- [ ] **CF-068 — Wire hit targets are a 2.25 px stroke and parameter dots are 7×7 px. [V]**
      Measured live: the XR parameter dots are 7×7 CSS px stacked 21 px apart, so WCAG 2.2
      SC 2.5.8's spacing exception does not apply either. Both `onCwClick` (`canvas.js:798`)
      and `onContextMenu` (`canvas.js:957`) look for a `.wire-hit` element that `drawWires`
      never emits and no template contains. The fix must give the wire a hit area independent
      of its drawn stroke.

- [ ] **CF-069 — Nothing announces a Validate or Generate result, and the error text is
      hover-only.** A repo-wide grep for `aria-live` returns nothing; the state chip is a
      `<span>` mutated with `textContent` (`index.html:21`) whose full error lives only in a
      `title`, which a keyboard user cannot summon. The chip is given `cursor:pointer` and a
      click handler without being focusable. The fix must announce the outcome and make the
      error readable without hovering.

- [ ] **CF-070 — `--shared` and `--warn` are the same hex, so a shared binding and a warning
      are the same colour. [V]** `proto.css:10,12` both `#877200`; `:26,28` both `#D6AB33`.
      In the YAML viewer `.code .tm` and `.code .sh` are two different facts rendered
      identically, while the canvas legend (`index.html:63`) teaches that this colour means
      "shared"; selection reuses `--warn` as a fourth meaning (`proto.css:136`). The fix must
      make the semantic tokens pairwise distinct in both themes.

### P3

- [ ] **CF-071 — Design-token hygiene: a purple, five undefined `var()`s, and no type or
      spacing scale. [V for the purple]** `#7c3aed` (hue 262.1°) ships as the CRON card colour
      (`internal/examples/examples.go:105`, painted inline at `main.js:577`) — the only violet
      in the served graph, and against the project's own rule. `--dim`, `--accent`, `--pri`,
      `--panel` and `--fg` are referenced but never defined, silently discarding intent and
      rendering the tour card near-black in the light theme. There are 14 font sizes over
      8–18 px and 23 spacing values with no tokens for either; 288 inline `style=` attributes
      in JS carry most of them, which is why `proto.css` review cannot see them. A hue and
      token lint is the gate that would hold this; it belongs with the CF-048 work.

- [ ] **CF-072 — Interface copy: implementation words, a wrong count, and dead-end empty
      states. [V for the count]** *envelope*, *emission engine*, *fan-out*, *the doc*, *the
      chip*, *the rail*, *from the cache* name the system, not the goal. The starter
      blueprints have four names, and `Import` produces results that say *adopt failed*. The
      canvas empty state claims **14 native Kubernetes kinds** (`canvas.js:443`); `nativeKinds`
      has **16**, and the palette on the same screen reads `16 kinds`. `No kinds match.` shows
      when nothing was searched, and the Providers Catalogue returns functions because the
      type filter is not passed. *"No schema found for kind X."* offers nothing, while the
      server already says `run: cf provider add %s`.

---

## Open — 2026-09-09 provider/sources, render errors, starter health

Found by re-checking the tree at `7edec90` and by reproducing a user report about
providers. Every item below was executed, not read.


---


---

## Open — 2026-09-04 composition-tester run and cross-engine audit (at `0d3e914`)

Renumbered from CF-049…CF-057 to CF-073…CF-081 on 2026-09-09: the canvas UX run above had
already bound CF-049…CF-072, and its report cross-references those ids. Branch
`CF-049-comp-tester-findings` carries the old numbers and is superseded by this section.
Report and re-runnable repros:
[docs/comp-runs/2026-09-04-cachedservice-namespaced-roundtrip.md](docs/comp-runs/2026-09-04-cachedservice-namespaced-roundtrip.md).



---

## Non-findings (Recorded so they are not re-raised)

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
- [x] `internal/emit/preview.go: PreviewExpression` is a public convenience and test seam; production HTTP endpoint calls `PreviewExpressionContext` directly.
