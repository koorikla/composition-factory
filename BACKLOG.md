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

## Open — re-checked 2026-09-03 @ 47949a3

Most of Tracks 1–3 is genuinely done and archived: `cf catalogue --kind` now
filters exactly (`--kind Bucket` no longer returns `provider-bitbucket-server`),
`refuseGoTemplateOnlyFeatures` deduplicates the engine preamble, `palette.js`
and `output.js` `init` went 906/835 lines to 22/47, `onBoxClick` 316 → 16,
`onBoxChange` 295 → 183, `web/` is off disk (working copy 313 MB → 178 MB),
`internal/adopt` coverage 78.6% → 84.4%, and `deadcode` is back to four hits,
all test seams. Three things do not hold up.

- [ ] **The round-trip gate does not assert the round trip.**
      `scripts/cluster/test-cluster.sh:138-158` does the hard part — applies,
      reads the XRD and Composition back with `kubectl get -o yaml`, imports,
      regenerates — and then checks only that the regenerated composition file
      is non-empty (`[ ! -s "${COMP_FILES[0]}" ]`). It would pass with one
      resource where the original had five. It also swallows a lossy import
      with `|| [ $? -eq 2 ]` without inspecting what was dropped. The principle
      above asks for the original bytes back: diff the regenerated composition
      and XRD against the ones `cf gen` produced before the apply, and fail on
      any difference that is not a named, expected server-side field. No Go
      test covers this either — nothing compares regenerated against original
      composition bytes anywhere in the suite.
- [ ] **`canvas.js openFieldPicker` is still 322 lines** (was 317 at the
      audit). Archived as converted to dispatch tables; it was not — and the
      original finding mischaracterised it. It is not an action ladder but a
      builder doing five jobs in one function: search input handling, type
      compatibility, relevance scoring, category grouping, and DOM rendering,
      across 43 branch points. Extract those four helpers; a `const actions`
      table is the wrong shape for it. The two genuine ladders in
      `inspector.js` were converted and are done.
- [ ] **Five branches still carry unmerged commits** and need a call each:
      `subagent-DX--Client---Test-Suite-Polish-Engineer-self-37c2a470` (12
      ahead), `worktree-agent-a5a7710927acd2ff7` (12),
      `worktree-agent-a7f7485202bdacadb` (4),
      `subagent-Canvas---UX-Authoring-Engineer-self-84d324ac` (1),
      `worktree-agent-a247d77332728f705` (1). Merge, cherry-pick or delete —
      10 of the 15 local branches are already merged anchors.

Minor, carried without a task: `internal/cache` (64.9%) and `internal/xpkg`
(72.6%) coverage have not moved across any pass.

---

## Non-findings (Recorded so they are not re-raised)

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated` with `--addr 0.0.0.0:8080`. Safe because the Service is ClusterIP.
- [x] `# TODO:` markers in `internal/emit/providerconfigs.go` are generated instructions for the cluster operator, not leftover comments.
- [x] `deadcode` reports on test-seam utilities (`catalogue.Validate`, `xpkg.PackageStream`, `cache.Store.Clear`) are expected.
