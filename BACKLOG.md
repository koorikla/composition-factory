# Backlog

Open work only — concise, prioritized, and verified against the codebase.

Completed work is archived in [docs/backlog-archive.md](docs/backlog-archive.md); full history is in `git log -p BACKLOG.md`.

---

## Architectural Principle: DSL (`.cf.yaml`) as Canonical Intermediate Representation

The `factory.crossplane.io/v1alpha1` `Blueprint` document (`.cf.yaml`) is and remains the single source of truth and intermediate representation (IR) for `composition-factory`. 

All user interfaces (Canvas, CLI, API, MCP) operate on this model. Crossplane manifests (`composition.yaml`, `definition.yaml`, `functions.yaml`, `package.yaml`) are deterministic, generated artifacts. Manifest import and adoption act as high-fidelity converters *into* the canonical Blueprint format.

---

## Track 1 — Manifest Import & Adoption Compatibility

- [ ] **Simplify Emitted Status Wires**: Replace the 11-term `hasKey/kindIs` guard chain with a clean, missingkey-safe `define "cf.observed"` helper in Go-templating outputs. Keep byte-determinism goldens and ensure `cf adopt` parses both formats seamlessly.
- [ ] **Direct Configuration Source Tree Import**: Extend `cf import` and `cf adopt` to read full Configuration repositories (`crossplane.yaml`, `apis/<xr>/definition.yaml`, `composition.yaml`), extracting XR schemas, resource templates, and parameters into a canonical `.cf.yaml` blueprint in one step.
- [ ] **Opaque Block & Custom Function Pipeline Preservation**: When importing complex foreign compositions containing unknown custom functions or non-standard pipeline steps, preserve them as declared custom steps in `spec.pipeline` / `spec.resources` so they round-trip cleanly without loss.
- [ ] **`kubectl` Export Scrubbing**: Automatically scrub runtime status, managed fields, UIDs, and cluster-assigned metadata when pasting or importing raw cluster dumps (`kubectl get composition -o yaml`).

---

## Track 2 — Canvas Live-Edit & Authoring Experience

- [ ] **Canvas Action Dispatch Maps**: Replace large `if/else` ladders in `web-proto/js/regions/canvas.js` (`openFieldPicker`) and `inspector.js` (`onBoxClick`, `onBoxChange`) with modular `const actions = { ... }` dispatch tables to simplify adding new authoring actions.
- [ ] **Canvas Region Modularization**: Extract inner helpers and sub-views from oversized `init` closures in `palette.js` and `output.js` into isolated, testable JavaScript modules.

---

## Non-findings (Recorded so they are not re-raised)

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
