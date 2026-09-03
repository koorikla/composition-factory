# Backlog

Open work only — concise, prioritized, and verified against the codebase.

Completed work is archived in [docs/backlog-archive.md](docs/backlog-archive.md); full history is in `git log -p BACKLOG.md`.

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

## Track 1 — Manifest Import & Adoption Compatibility

- [ ] **Simplify Emitted Status Wires**: Replace the 11-term `hasKey/kindIs` guard chain with a clean, missingkey-safe `define "cf.observed"` helper in Go-templating outputs. Keep byte-determinism goldens and ensure `cf adopt` parses both formats seamlessly.
- [ ] **Direct Configuration Source Tree Import**: Extend `cf import` and `cf adopt` to read full Configuration repositories (`crossplane.yaml`, `apis/<xr>/definition.yaml`, `composition.yaml`), extracting XR schemas, resource templates, and parameters into a canonical `.cf.yaml` blueprint in one step.
- [ ] **Opaque Block & Custom Function Pipeline Preservation**: When importing complex foreign compositions containing unknown custom functions or non-standard pipeline steps, preserve them as declared custom steps in `spec.pipeline` / `spec.resources` so they round-trip cleanly without loss.
- [ ] **`kubectl` Export Scrubbing**: Automatically scrub runtime status, managed fields, UIDs, and cluster-assigned metadata when pasting or importing raw cluster dumps (`kubectl get composition -o yaml`), reporting how many server-side fields were dropped rather than dropping them silently.
- [ ] **Round-trip gate in Lane C** (the acceptance bar for this whole track):
      extend `make test-cluster` to apply each emitted example, read it back
      with `kubectl get -o yaml`, import that through `cf import`, regenerate,
      and assert the bytes match the original. Start with the K8s App example
      (native kinds, no cloud credentials, already what Lane C composes), then
      the IRSA and RDS examples behind the provider-credential gate. Every
      difference the assert finds is either a scrub rule the importer is
      missing or an emitter bug — record which, per field.

---

## Track 2 — Canvas Live-Edit & Authoring Experience

- [ ] **Canvas Action Dispatch Maps**: Replace large `if/else` ladders in `web-proto/js/regions/canvas.js` (`openFieldPicker`) and `inspector.js` (`onBoxClick`, `onBoxChange`) with modular `const actions = { ... }` dispatch tables to simplify adding new authoring actions.
- [ ] **Canvas Region Modularization**: Extract inner helpers and sub-views from oversized `init` closures in `palette.js` and `output.js` into isolated, testable JavaScript modules.

---

## Non-findings (Recorded so they are not re-raised)

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
