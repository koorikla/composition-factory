# Backlog

Open work only — 60 items, each verified against the code or reproduced by hand
when it was written.

Completed work is not here. 151 closed items, with their original wording and
reasoning, live in [docs/backlog-archive.md](docs/backlog-archive.md); full
history is in `git log -p BACKLOG.md`. Tick an item by moving it there, not by
leaving an `[x]` behind — this file is read into an agent's context on every
session, so its length is a running cost.

Read order for a fresh agent: **v4 first** (the only pass that built real
compositions and applied them to a cluster, so its P0s outrank everything
else), then v5, then v3.

One exception to the open-only rule: v5 keeps a short "Non-findings" list of
things that look like bugs and are not. It is there to stop them being
re-raised, which is a context saving, not a cost.

## Backlog v3 — Manifest Import & Live Edit Compatibility (DSL retained)

Decision: Retain DSL `.cf.yaml` as a first-class canonical intermediate representation while building seamless Crossplane manifest import, bidirectional adoption, and live-editing compatibility.

### Phase 1 — Manifest Import & Dialect Round-Trip

- [ ] Simplify cf's emitted status-wire templates: replace the 11-clause status-wire guard chain with a `define "cf.observed"` helper (or `dig`) that stays `missingkey=error`-safe; prove by render on the IRSA and foreach-status fixtures. Accept both the old and new chain during migration. Keep byte-determinism goldens.
- [ ] Direct Configuration source tree import: `cf import` / `cf adopt` accepts a full Configuration tree (`crossplane.yaml`, `apis/<xr>/definition.yaml`, `composition.yaml`), extracting XR definitions, resource templates, and parameters into the live workspace.
- [ ] `internal/manifest` parsing and splice editing: model, parse, and live-patch raw YAML manifests preserving comments, formatting, and unmanaged blocks.
- [ ] Schema validation of field paths against CRDs runs on parsed placeholders and live-edited manifest trees.

### Phase 2 — Canvas Live-Edit & Bi-directional Manifest Sync

- [ ] Canvas & API live editing: allow toggling between editing `.cf.yaml` DSL and direct manifest views (`composition.yaml`, `definition.yaml`) with real-time bidirectional synchronization.
- [ ] Opaque block preservation: foreign compositions with custom pipeline steps, unknown functions, or extra annotations render as structured/locked cards on canvas and are preserved verbatim across round trips.
- [ ] `kubectl` export scrub on open with a visible "removed N server-side fields" note.

## Backlog v4 — dogfooding five real compositions on v0.7.0 (2026-09-02)

Method: five agents each built a composition end to end with only the CLI, docs and HTTP/MCP
API — A: AWS VPC/subnets/RDS with forEach + status wires + when; B: cloud-agnostic K8s app on
native kinds; C: GCP CloudSQL/GCS/IAM/PubSub incl. KCL and Python engines; D: adopting three
real-world compositions; E: SQS queue + DLQ built purely through MCP and HTTP. A sixth agent
re-verified all 46 ticked v2 items. Artifacts under the session scratchpad `dogfood-*/`
(blueprints, outputs, renders, REPORT.md). Items marked (repro) were reproduced by hand.


### P2 — discovery and CLI

- [ ] Every status wire is an 11-term hasKey/kindIs guard; correct but unreviewable by eye
      (the same item Backlog v3 Phase 1 needs). A, E.

### Structure — canvas and modules

- [ ] Three canvas dispatch chains are if/else ladders on a `data-a` action attribute: canvas.js openFieldPicker, inspector.js onBoxClick, onBoxChange. Replace with a `const actions = { … }` map, one small named function per action.
- [ ] palette.js `init` and output.js `init` closures modularization: extract reusable component helpers out of init.

### Workspace hygiene (needs a decision, not a delete)

- [ ] The working copy is 807 MB and growing (731 MB at the audit on
      2026-09-02): .claude/worktrees/ (379 MB, was 321), .worktrees/ (145 MB),
      plus web/ (159 MB, the retired React canvas — already out of git, still
      on disk). 27 worktrees are registered. Of the 27 local branches besides
      main, 22 are fully merged and exist only as worktree anchors — up from
      15 of 20 a day ago, so this accretes faster than it is cleared. The same
      five carry unmerged commits and need a call each: subagent-DX--Client---Test-Suite-Polish-Engineer-self-37c2a470
      (12 ahead), worktree-agent-a5a7710927acd2ff7 (12),
      worktree-agent-a7f7485202bdacadb (4),
      subagent-Canvas---UX-Authoring-Engineer-self-84d324ac (1),
      worktree-agent-a247d77332728f705 (1). The ~/.gemini/antigravity/…
      worktrees belong to a live parallel session — leave those alone.

### Non-findings, recorded so they are not re-raised

- [x] `deploy/k8s/deployment.yaml` passes `--i-know-this-is-unauthenticated`
      with `--addr 0.0.0.0:8080`. Deliberate and documented in the manifest:
      safe only because the Service is ClusterIP. Worth re-checking the day an
      Ingress is put in front of it, not before.
- [x] The three `# TODO:` markers in internal/emit/providerconfigs.go:200-202
      are emitted into the generated ProviderConfig scaffold as instructions
      to the operator. They are output, not leftovers.
- [x] `deadcode` names catalogue.Validate, xpkg.PackageStream,
      cache.Store.Clear and cmd/cf.defaults unreachable. All four are reached
      from tests or from the scripts/build-catalogue main; nothing to delete.
