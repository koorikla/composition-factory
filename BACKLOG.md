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

### P2

- [ ] **CF-064 — The output drawer takes 250 px of a 720 px viewport to show 142 px of
      YAML. [V]** Measured at 1280×720: topbar 46, columns 424, drawer 250; inside it `#code`
      is 142 px — seven lines of a ~500-line document, so 43% of the drawer is chrome. The
      canvas gets 840×424 and the palette 277 px of list. The fix must give the authoring
      surfaces the vertical space, at this viewport and smaller.

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
