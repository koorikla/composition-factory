# Changelog

All notable changes to composition-factory are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Releases up to and including v0.7.0 were reconstructed from the tagged git
history after the fact; from Unreleased onwards entries are written as the work
lands.

Byte-for-byte determinism of generated artifacts is part of the public
contract. Any change that alters emitted YAML for an unchanged blueprint is
listed under **Changed** even when it is a bug fix, because it moves a
consumer's git diff.

## [Unreleased]

### Added

- `docker run -p 127.0.0.1:8080:8080 ghcr.io/koorikla/compositionfactory` is now
  the whole bootstrap. The image carries its container defaults as environment
  (`CF_ADDR`, `CF_BLUEPRINT`, `CF_OUT`, `CF_CACHE_DIR`, `CF_LOCK`) rather than as
  CMD arguments, so they survive a command override, and its CMD runs `serve`
  instead of printing help. A missing blueprint already scaffolded itself and
  schemas already load on demand, so there is nothing left to prepare first:
  no blueprint to hand-write, no provider to pre-cache. `--blueprint` gained
  `--file` as an alias. Binding `0.0.0.0` is what makes `-p` work at all, and
  inside a container that is the container's own namespace -- the native
  default is unchanged and still refuses a non-loopback bind without the
  explicit opt-in.
- The canvas imports existing Crossplane Compositions. `/api/blueprint/adopt`
  already existed but nothing in the UI reached it, so Import posted everything
  to the blueprint gate and rejected the one file most people already have.
  Import now routes on the manifest's own `kind:` -- Blueprint through import,
  Composition or XRD through adopt -- and reports what adoption had to drop
  rather than silently keeping a partial document.
- The starter-blueprint chooser marks each example with whether its provider
  schemas are already cached, and leads with the ones that load right now.
  Loading an example syncs its sources, so on a cold cache -- which is what a
  first run in a fresh container is -- a card that names a provider costs a
  download and cannot work offline at all. `GET /api/examples` carries
  `sourcesReady` and `missingSources` for this.

### Changed

- `docs/code-audit.md` re-verified against `main` at 8b58a1d, 39 commits after
  the audited tree. Closed: `(*Blueprint).Validate` is split into seven named
  validators, `make test-race` runs in CI lane A, and the kind-cluster lane the
  audit called its strongest recommendation now runs on every push. Still open
  and all grown since: the three-way emitter triplication, both oversized
  canvas `init` closures, and the worktree sprawl. Newly recorded:
  `catalogue.Kinds` and `catalogue.PackagesForKind` are exported, tested and
  called by nothing in production.

### Fixed

- `web-proto`: the engine and templates selects in the editor drawer rendered
  as white boxes with near-white text on them under the dark theme, and the tab
  strip above them grew a white scrollbar. Two causes: both selects carried
  `class="sel"`, which styles nothing (it collides with the unrelated
  `.node.sel` rule) where the app's themed select class is `.tsel`; and the
  canvas themed itself entirely through custom properties while never declaring
  `color-scheme`, so every UA-painted widget -- scrollbars, select popups,
  focus rings -- stayed light no matter the theme. `color-scheme` is now
  declared everywhere the palette is.
- `.claude/launch.json` pointed at an absolute scratch path from one machine's
  session, so the canvas preview could not start anywhere else. It uses a
  repo-relative ignored path now.

- `web-proto`: hovering a wire's floating delete button no longer throws it
  across the canvas. The button is positioned by a `transform` **attribute** on
  its `<g>`, which on an SVG element is the same property as CSS `transform`,
  so the `:hover{transform:scale(1.2)}` on that same element dropped the
  translate and animated the button off to the SVG origin — out from under the
  cursor hovering it. The position now lives on the outer group and the grow on
  an inner one, with `transform-box:fill-box` so it grows about its own centre.
- `tests/`: the canvas-geometry e2e tests that failed intermittently on the
  headless Linux runner while passing on macOS now wait on the state they need
  rather than on luck. Three separate causes, each reproduced before it was
  fixed: a wire whose endpoints share a `y` is a perfectly horizontal path whose
  client rect is zero-height, which Playwright calls invisible and refuses to
  click even with `force`; `locator.boundingBox()` waits only for the element to
  be attached and returns `null` while layout has yet to run; and Chromium's
  context-menu hit test truncates the cursor to whole pixels, enough on its own
  to miss a 2.25px stroke. `tests/helpers.js` gains `canvasSettled`,
  `settledBox` and `clickWire`, and a new test pins the hover fix above.

## [0.8.0] - 2026-09-03

### Added

- Discovery CLI: `cf kinds`, `cf fields` and `cf catalogue`, backed by a
  reverse kind index, so a blueprint author can find a kind and its field
  paths without leaving the terminal.
- Adopt engine: `cf` reads an existing `function-patch-and-transform`
  composition's `input.resources` back into a blueprint, emits flat XRD
  parameters, and reports what it could not carry across as a loss report
  rather than dropping it silently.
- Render-time OpenAPI validation of emitted resources against the cached CRD
  schemas, plus raw-template and raw-reference validation, so a blueprint that
  would be rejected on apply fails at generate time instead.
- Lane C, in-cluster verification on a real kind cluster with Crossplane and
  the pipeline functions (`make cluster`, `make cluster-down`,
  `make test-cluster`), and workspace isolation so concurrent checkouts do not
  collide on cluster-scoped names.
- Additional vendored core Kubernetes kinds; `providerName` is optional for
  blueprints composed purely of native kinds; `cf --version`.
- Aggregated ClusterRole RBAC emission and array-element emission for provider
  kinds; self-contained FileSystem template exports.
- Python engine `MessageToDict` output, KCL `forEach` list syntax, and
  observed context available to templates.
- `make lint-strict`: staticcheck over the whole module, pinned to v0.8.1 in
  the `Makefile` and run through `go run` so it needs no separate install and
  cannot drift between a developer's machine and CI. CI lane A runs it next to
  `make lint`.
- `staticcheck.conf`: staticcheck's default check set minus ST1005, with the
  reasoning recorded in the file — this engine's errors are its user interface
  and are deliberately written as full explanatory sentences.
- `CHANGELOG.md` (this file) and `docs/code-audit.md`, a dated, repeatable
  audit of the tree with the method written down so the next run is comparable.
- CI grew four jobs beside the Go lane: acceptance, Playwright e2e, a Docker
  build check, and the kind-cluster lane.

### Changed

The entries below alter emitted YAML for an unchanged blueprint. They are bug
fixes, but each moves a consumer's git diff.

- An integer parameter or literal targeting a Kubernetes `IntOrString` field
  now renders as a bare scalar rather than a quoted string. The vendored schema
  normalizes `IntOrString` to type string — the one spelling legal for both
  halves — and the emitter quoted on that basis, emitting `targetPort: "8080"`.
  The API server reads a *string* `targetPort` as a port NAME and rejects a
  numeric one with `must contain at least one letter (a-z)`, so the composed
  Service never applied. A string source still quotes, and `Quantity` (which
  carries no `int-or-string` format) is untouched.
- Typed literals: a `value:` is emitted with the type its target field
  declares instead of always as a quoted string, and leaf strings are quoted
  where YAML 1.2 would otherwise coerce them. Real source headers are
  preserved.
- Nested `forProvider` emission for dotted field paths, and deterministic
  sibling naming for native kinds.
- Map merging, and YAML 1.2 boolean keys are quoted.
- `make lint` runs `gofmt` over the tracked files (`git ls-files '*.go'`)
  instead of the whole directory. Locally, `gofmt -l .` walked 1701 `.go` files
  — ten times the 163 this tree owns — because agent worktrees under
  `.worktrees/` and `.claude/worktrees/` are other branches' full checkouts. An
  unformatted file on an abandoned branch could fail the lint of the tree you
  are actually editing.
- The cluster harness' group suffix carries only the 6-char workspace path
  hash (`w<hash>.cf-test`) rather than the full directory slug. Crossplane
  copies a Composition's name into the `crossplane.io/composition-name` label
  of every CompositionRevision, and label values cap at 63 characters, so a
  longer suffix silently stopped revisions — and therefore all composition —
  from being created.
- `BACKLOG.md` holds open work only; the 150 closed items moved whole to
  `docs/backlog-archive.md`. It is read into every agent's context on every
  session, so that history was a cost paid on every read.

### Fixed

- Cluster source schema loading and native CRD convention handling.
- `PUT`-time validation and atomic blueprint update in the API; sources are
  auto-declared; MCP contract enhancements.
- Canvas: dynamic engine list from `/api/version`, touch `pointerType` guards
  against double-panning, and palette scope grouping.
- `field-when` hints and kind suggestions in validation output; `cf check`
  reports extra files.
- `internal/emit/kcl.go`: `buildEnvTree`'s `findOrCreate` closure was declared
  and assigned separately, the shape required only for a self-recursive
  closure. It never recursed, so the split was a leftover; collapsed to `:=`.
- `catalogue/catalogue.go`: a line of the package doc began with `go:embed`,
  which the toolchain reads as a malformed compiler directive rather than
  prose (SA9009). Reworded.

## [0.7.0] - 2026-09-02

### Added

- KINDS rail scope filtering, a clickable Validate chip that diagnoses the
  environment, next-step guidance after generation, and touchmove rAF
  debouncing.
- Empty-canvas onboarding, catalogue "installed" feedback, inline XR field
  adding, drag-to-wire type warnings, and modal focus traps.
- `/api/version` reports the supported engines; `SplitDocs` is used for CRD
  manifest parsing; `providerName` validation explains itself.
- CONTRIBUTING.md; the Playwright suite runs as its own CI lane with a page
  error guard that fails a test on any uncaught browser error.

### Changed

- The whole consolidation backlog closed: one shared index rebuild for provider
  add/delete/CRD sources, one `srv.mutate` for the six blueprint handlers, one
  `SplitDocs`, one `unknownPath`, one `startDrag`, one DOM `esc`, one test
  fixture package. Schema trees, the catalogue payload and the provider cache
  are memoised rather than rebuilt per request.
- Acceptance tests build the binary and warm the provider cache once in
  `TestMain` instead of eleven times.

### Fixed

- KCL and Python emitters carry the structured RHS (guards included) instead of
  re-parsing the Go-template text, and refuse `template:` fields and
  `conventions` rather than silently emitting something else.
- Status wires read at the status root; Python drops absent values.
- API context propagation, adopt mutex locking, KCL/Python envelope nesting.
- `sampleXR` for-each rendering, modal hidden CSS, cross-platform keys in the
  e2e suite, and an alert locator collision.

## [0.6.0] - 2026-09-02

### Added

- Templates see the XR's metadata as `.xrMeta`; the IRSA example maps inputs
  both ways.
- Dependency-tree canvas layout — consumers sit to the right of their sources.

### Changed

- Cluster access is opt-in only.
- The e2e suite reuses the engine on 8081, so a crashed run no longer blocks the
  next one.

### Fixed

- Canvas interaction stability under load; the inspector no longer repaints
  underneath an edit in progress.

## [0.5.7] - 2026-09-02

Re-tag of 0.5.6; no code change.

## [0.5.6] - 2026-09-02

### Added

- Live-cluster schema source and CRD discovery.
- First-class map-entry wires and authoring.
- Pipeline-steps authoring in the inspector, with a `functions.yaml` output tab.

### Fixed

- The required chain propagates through CronJob's `jobTemplateSpec`.

## [0.5.5] - 2026-09-02

### Added

- Drag-to-wire, with a picker showing all spec fields, envelope and
  annotations, and effective-required count badges.
- Skaffold config and Kubernetes manifests, with an initContainer that
  pre-populates the provider cache.

## [0.5.4] - 2026-09-02

### Added

- The IRSA example gains RolePolicy, shared tags and namespace wiring.

## 0.5.1 – 0.5.3 - 2026-09-02

### Fixed

- Renders defer during pointer gestures; a stuck gesture can no longer freeze
  rendering; breakpoint crossings keep the inspector; inspector toggle at narrow
  widths.

## [0.5.0] - 2026-09-02

### Added

- Typed object parameters end to end — nested XRD schema, member wires, HTTP
  contract, GUI members.
- `metadata.annotations` authoring on composed resources (the IRSA demo).
- forEach bounds from another resource's observed status; when-conditions
  authored from the inspector; envelope field control; cross-object
  `atProvider` status wiring.
- Effective requiredness in the API and the GUI; conventions skip native
  resources.
- Slide-over panes on narrow screens; the wordmark shows the real build version.

## [0.4.0] - 2026-09-02

### Changed

- Relicensed under AGPL-3.0-only.
- README restructured for the public release: demo GIF first, self-contained
  starter blueprint in the Docker quickstart.

## [0.3.0] - 2026-09-02

### Added

- Multi-arch Docker image and binary releases published from GitHub Actions;
  `ghcr.io/koorikla/compositionfactory` documented in the quickstart.
- ProviderConfig scaffolds per provider family; RBAC and providerconfigs tabs in
  the output drawer.
- Catalogue synthesizes per-service entries for upjet provider families.
- Canvas: right-click menu (duplicate, rename, delete), manual card resize, kind
  hover preview, provider detail with kind picker and native-group toggle,
  removable shared parameters with a type-aware add form.

### Fixed

- `cf serve` serves the live `web-proto/` tree when present, embedded assets
  only as a fallback, and sends `Clear-Site-Data` on document loads so a stale
  browser module cache heals itself.
- Store mutations are serialized — rapid actions can no longer lose each other.
- Card action buttons register as clicks, never as drags.
- The render check no longer feeds providerconfigs to crossplane as functions.

## [0.2.0] - 2026-09-01

### Added

- First tagged release: the `cf` binary (kong CLI), the emit engine, and CI
  lane A.
- `internal/xpkg` fetches only the package layer of an OCI xpkg image rather
  than whole provider images.
- Research notes, design spec, M1 plan and the UX prototype that the canvas is
  built from.

[Unreleased]: https://github.com/koorikla/compositionfactory/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/koorikla/compositionfactory/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/koorikla/compositionfactory/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/koorikla/compositionfactory/compare/v0.5.7...v0.6.0
[0.5.7]: https://github.com/koorikla/compositionfactory/compare/v0.5.6...v0.5.7
[0.5.6]: https://github.com/koorikla/compositionfactory/compare/v0.5.5...v0.5.6
[0.5.5]: https://github.com/koorikla/compositionfactory/compare/v0.5.4...v0.5.5
[0.5.4]: https://github.com/koorikla/compositionfactory/compare/v0.5.3...v0.5.4
[0.5.0]: https://github.com/koorikla/compositionfactory/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/koorikla/compositionfactory/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/koorikla/compositionfactory/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/koorikla/compositionfactory/releases/tag/v0.2.0
