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

## Status: All Active Backlog Items Completed

All items across Tracks 1, 2, 3 and the 2026-09-03 findings (CF-001 through CF-044) have been completed, verified with automated tests (including unit, race detector, Playwright browser suite, and widened Lane C round-trip byte diff), and archived in [docs/backlog-archive.md](docs/backlog-archive.md).

---

## Non-findings (Recorded so they are not re-raised)

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
