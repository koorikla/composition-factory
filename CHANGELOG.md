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

- `make lint-strict`: staticcheck over the whole module, pinned to v0.8.1 in
  the `Makefile` and run through `go run` so it needs no separate install and
  cannot drift between a developer's machine and CI. CI lane A runs it next to
  `make lint`.
- `staticcheck.conf`: staticcheck's default check set minus ST1005, with the
  reasoning recorded in the file — this engine's errors are its user interface
  and are deliberately written as full explanatory sentences.
- `CHANGELOG.md` (this file) and `docs/code-audit.md`, a dated, repeatable
  audit of the tree with the method written down so the next run is comparable.

### Changed

- `make lint` runs `gofmt` over the tracked files (`git ls-files '*.go'`)
  instead of the whole directory. Locally, `gofmt -l .` walked 1701 `.go` files
  — ten times the 163 this tree owns — because agent worktrees under
  `.worktrees/` and `.claude/worktrees/` are other branches' full checkouts. An
  unformatted file on an abandoned branch could fail the lint of the tree you
  are actually editing.

### Fixed

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

[Unreleased]: https://github.com/koorikla/compositionfactory/compare/v0.7.0...HEAD
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
