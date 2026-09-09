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

## Open — 2026-09-09 provider/sources, render errors, starter health

Severities in this section use the **UX** scale unless marked *engine*: **P0** lost work,
impossible, or the interface states something false · **P1** completable only with knowledge
that exists solely in the source · **P2** completable, wastefully · **P3** polish. Engine scale
(P0 wrong output silently · P1 loss that survives to the cluster · P2 source-only knowledge or
unsafe API contract · P3 docs) is in `.claude/skills/backlog-authoring/SKILL.md`.
Earlier run reports: [docs/ux-runs/](docs/ux-runs/2026-09-04-m1-first-contact.md),
[docs/comp-runs/](docs/comp-runs/2026-09-04-cachedservice-namespaced-roundtrip.md).

### P0

- [ ] **CF-086 — The SOURCES tab shows the provider list it fetched first; loading a starter
      example (or any doc change that swaps sources) leaves it stale until a page reload. [V]**
      `web-proto/js/regions/palette.js:58` caches `GET /api/providers` and refetches only while
      the cache is `null`; the `"doc"` subscription (`:1088`) never clears it and the view (`:401`)
      prefers the cache over `doc.spec.sources`. Repro: open SOURCES → Examples → load RDS: the
      canvas shows Instance, `/api/providers` lists provider-aws-rds, the tab still lists the
      previous provider (the user's "CF-082 is not fixed" report; CF-082 itself holds server-side).
      The tab must reflect the server's list after every doc change.
      Brief: `docs/tasks/CF-086-sources-tab-stale-provider-list.md`.

### P1

- [ ] **CF-088 — Opening a blueprint whose declared source is not in the cache lands on a red
      generate error telling the user to run `cf provider add`; the startup log promised the
      schema would load on demand, and nothing does until a write happens. [V]** Only
      `syncBlueprintSourcesLocked` (`internal/api/blueprint.go:509`) fetches, and only writes call
      it; `cmd/cf/options.go:39` skips the ref with "schemas load on demand". Repro: empty
      `--cache-dir`, blueprint `internal/examples/sqs-queue.cf.yaml`, open the canvas: top bar
      `error`, banner `provider "…" is not in the cache; run: cf provider add …`, `/api/providers`
      `[]`. This is the "fresh pod → instant render error" report; in the container the CLI is
      unreachable from the canvas. Must load on first need or name the in-canvas repair.
      Brief: `docs/tasks/CF-088-declared-source-not-loaded-on-demand.md`. Merges after CF-087.

### P2

- [ ] **CF-087 — *(engine)* A document write whose declared source cannot be fetched answers
      200 and reports the failure only on the server's stderr. [V]** `blueprint.go:558` prints
      `continuing offline` and `continue`s; `persistBlueprint` (`:622`) and `handleLoadExample`
      (`examples.go:88`) then report success. Repro: empty cache, no registry credentials,
      `PUT /api/blueprint` with a new source → `200`, `/api/providers` `[]`, next generate `400`.
      An agent on HTTP/MCP cannot tell a good write from one that left the document unbuildable.
      The write's answer must name the source and the fetch reason; the canvas must show it at
      save time. Brief: `docs/tasks/CF-087-write-hides-source-fetch-failure.md`.

---

## Non-findings (Recorded so they are not re-raised)

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
- [x] `internal/emit/preview.go: PreviewExpression` is a public convenience and test seam; production HTTP endpoint calls `PreviewExpressionContext` directly.
