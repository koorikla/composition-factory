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

## Backlog v3 — manifests as source of truth (analysis 2026-09-02)

Memo: docs/research/2026-09-02-manifests-as-source-of-truth.md. Phase 0 ran and returned GO
(2026-09-02) — the spike and its goldens are in docs/backlog-archive.md. Phases 1–3 are no
longer blocked on the decision, only on Kaur saying start. Automation drivers: do not start
Phase 1 from this list.

### Phase 1 — cf-dialect round-trip (Phase 0 returned GO; awaiting Kaur's start)

- [ ] Simplify cf's own emitted template so a human can edit it in place: replace the ten-clause
      status-wire guard chain with a `define "cf.observed"` helper (or `dig`) that stays
      `missingkey=error`-safe; prove by render on the IRSA and foreach-status fixtures. The
      reader must accept both the old and the new chain for one release (migration), then only
      the new. Keep byte-determinism goldens.
- [ ] Specified structure = Configuration source tree: crossplane.yaml (sources), apis/<xr>/
      definition.yaml + composition.yaml, .cf/layout.yaml, .cf.lock. `cf package` and
      `crossplane xpkg build` consume it unchanged.
- [ ] `internal/manifest` for real: model, parse, patch (splice), lint (unguarded optional
      dereference; XRD default vs template default drift; scope vs `.m.` group mismatch).
      Schema validation of field paths against CRDs runs on the parsed placeholders — unchanged
      differentiator.
- [ ] Wire expression grammar limited to what cf emits: `$spec.x` chains, cf's guard chains,
      `(index $.observed.resources "r").resource.status.…` → param/status wires; anything else
      inside a cf-shaped field → raw wire (shown, text-editable, not typed).
- [ ] Engine edits as splices: set literal, wire param, unwire, add guarded optional field, add /
      remove / rename resource document, when, forEach (cf canonical `range`), annotations,
      envelope. Each with a golden proving only the intended bytes change.
- [ ] `cf gen` becomes the one-release migration tool (blueprint → manifest tree); `cf validate`
      and `cf lint` become first-class CLI verbs; `cf adopt`/`import` are subsumed by "open".

### Phase 2 — canvas and API over the manifest model (blocked on Phase 1)

- [ ] API/MCP: document routes become manifest routes (GET/PUT xrd, composition, layout) plus the
      same semantic edit routes as today implemented as splices; blueprint JSON disappears from
      the wire. MCP tools keep their names where the semantics survive.
- [ ] Canvas store `doc` = {xrd, composition model, layout}; regions read the model, not
      `spec.resources`. Opaque spans render as locked regions on a card (text-editable in the
      inspector), whole opaque documents as locked cards. Provider sources come from
      crossplane.yaml dependsOn + apiVersion lookup.
- [ ] `kubectl` export scrub on open with a visible "removed N server-side fields" note; first-save
      canonicalisation note for XRDs.
- [ ] Playwright: the 33 blueprint-shape specs migrate to manifest fixtures; add round-trip specs
      (open foreign composition → edit one field → file diff is one hunk).
- [ ] Examples become manifest trees; the startup chooser loads them; the Guide, dsl.md, cli.md,
      mcp.md and README describe manifests, not the DSL.

### Phase 3 — remove the DSL (blocked on Phase 2 shipping)

- [ ] Delete internal/blueprint, adopt, import, the embedded-blueprint annotation in
      package.yaml, the blueprint examples format; KCL/Python become export-only from the model
      or are dropped (go-templating only for now, per Kaur).
- [ ] Spec addendum: retire §7 (the DSL), restate §2's round-trip non-goal as "understanding is
      partial, preservation is total", update §11/§12.

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

### v2 half-fixes (verifier, 14 of 46 ticked items partially done)

- [ ] Design spec still "draft for review" with §11/§12 unsuperseded.
- [ ] Drag-to-wire type warning ships but "change parameter type" does not;
      catalogue-add spinner/toast/tab-switch and the managementPolicies "more" button have
      no real spec coverage (the latter's spec passes vacuously); inspector "env:" vs canvas
      "envelope." namespaces still split; output.js splitter still hand-rolled; `crds:` sources
      still re-read per generate; api/server_test still retypes the Queue CRD; the Guide tab
      is still a hardcoded copy of docs/guide.md.

## Backlog v5 — whole-tree code audit (2026-09-02 @ ee61f82, re-verified 2026-09-03 @ 8b58a1d)

Re-verification, 39 commits and one release later: findings 1 (Validate at 592
lines) and 6 (no race detector in CI) are closed and archived, and the audit's
strongest recommendation landed — CI Lane C now stands up a real kind cluster
and runs `make test-cluster` on every push, which is the gate that closes the
blind spot the caveat below names. What remains open moved the wrong way: the
emitter triplication, both JS init closures and the worktree sprawl all grew.
Numbers in each item are current as of 2026-09-03. Full delta:
docs/code-audit.md, "Re-verified".

Scope caveat, and it matters: this audit is static. It reads the tree, the
tooling and the shapes; it never built a composition or applied one. Backlog
v4 above ran the opposite method on the same commit — five agents building
real compositions end to end — and found P0 defects in emitted output that
nothing here could have surfaced (dotted forProvider keys, `value:` always
quoted, `resolveKind` ignoring `provider:`). Read v4 first. The grades below
are grades for the codebase as an artifact, not for whether it generates
correct YAML; where the two disagree, v4 wins, because it checked.

Method: tooling first, then a read of the largest units. `gofmt`/`go vet`,
staticcheck v0.8.1, `deadcode ./cmd/...`, `go test ./... -short -cover`,
`go test ./... -short -race`, `npm audit`, plus the CI workflows, Dockerfile,
deploy manifests and a secrets scan. Full report with the numbers and the
commands that produced them: docs/code-audit.md.

Headline: there was very little to clean *at this level*. The consolidation
backlog and Backlog v2 took the duplication; vet, staticcheck and the race
detector are clean, and `deadcode` finds no dead production code at all (its
four hits are test seams and a second `main`). 724 Go test functions + 150
Playwright behaviors, 25 398 test LOC against 16 458 production LOC, 80-100%
coverage on the packages that matter. What is left *in this dimension* is
structural. That a suite this large stayed green through v4's P0s is itself
the finding worth carrying forward: the tests pin the emitter's bytes against
its own goldens, so an emitter that is wrong in the same way twice passes.

### Structure — the five units carrying disproportionate complexity

- [ ] The three emitters walk the same tree three times. Re-measured
      2026-09-03: composition.go:writeResourceTemplate (294 lines, was 279),
      python.go:pythonTemplateBody (176, was 165), kcl.go:kclTemplateBody
      (186, was 164). Diffing the KCL and Python bodies gives 108 changed
      lines out of ~185 (was 99 of ~165) — the gap widens every release, so
      this item gets more expensive the longer it waits.
      It is the same traversal in all three (refuse conventions, resolve
      kind, plan fields/envelope/annotations, open when, open forEach, write
      apiVersion/kind/metadata/spec/forProvider), differing only in syntax
      tokens. The structured-RHS work already did the hard half; what remains
      is to lift the walk: one `walkResources` over a small backend interface
      (openResource, writeKey, openMap, formatLiteral, condition, loop), the
      three current bodies becoming the three backends. Two days, medium risk
      but bounded — every path has a byte-pinned golden. Do not start it with
      anything else in flight.
- [ ] Three canvas dispatch chains are 300-line if/else ladders on a
      `data-a` action attribute: canvas.js openFieldPicker (317),
      inspector.js onBoxClick (316), onBoxChange (295). Replace with a
      `const actions = { … }` map, one small named function per action. One
      day, low risk with 150 Playwright behaviors underneath — but the suite
      needs an unshared port first (see the per-workspace e2e item above).
      Biggest legibility win available in the canvas.
- [ ] palette.js `init` is 906 lines and output.js `init` is 835 (2026-09-03;
      882 and 830 at the audit — both still growing) — each
      module's entire body lives inside its init, so nothing in it can be
      reached or tested in isolation. Larger and separate from the dispatch
      chains above; do those first and reassess.

### Coverage and CI

- [ ] Coverage thins where the tree touches the outside world. Re-measured
      2026-09-03: internal/cluster 55.0% (unmoved), internal/cache 64.9%,
      internal/xpkg 72.6%, cmd/cf 74.0%. internal/cluster is the only package
      that talks to a live API server — bring its error paths (unreadable
      kubeconfig, unreachable server, partial CRD listings) up as part of the
      cluster lane, which now runs on every push.
- [ ] Coverage fell in the two packages that took the most v0.8.0 work:
      internal/adopt 85.9% → 77.3% (adopt engine + loss report),
      internal/emit 87.7% → 82.8% (render-time validation, typed literals,
      nested forProvider). Healthy in absolute terms, but both moved down
      while the code beneath them moved up. Cover the new paths before the
      next feature lands on them.

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

### Dead code introduced with the discovery CLI (found 2026-09-03)

- [ ] `catalogue.Kinds` and `catalogue.PackagesForKind`
      (catalogue/kinds.go:230,240) are exported, covered by
      TestKindsAndPackagesForKind, and called by nothing in production —
      `deadcode ./cmd/...` went from four unreachable functions to six, and
      these are the two new ones. The maps behind them are live (`Matches`
      uses them to power catalogue.Search), so these are accessors written
      for a caller that never arrived. Either surface them (`cf catalogue`
      showing which kinds a package serves, or a `--kind` lookup) or
      unexport them and drop the test.

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
