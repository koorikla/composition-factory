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


### P1 — alternative engines are broken for real blueprints

- [ ] Lane B renders only go-templating. Extend the acceptance test to render each engine
      through its real function image (function-kcl, function-python) on the same fixtures;
      the Python `.get` bug would have been caught on day one.

### P1 — adopt loses most of what it reads, silently

- [ ] cf cannot adopt its own output: the `{{- $spec := … -}}` prelude and `{{- if hasKey }}`
      guards break the mask-then-YAML parser (`cannot unmarshal string into … map`). Every
      `{{ }}` is masked as a quoted scalar, so block-level actions become YAML values.
      Decide: fix adopt's masking to treat control-flow lines as opaque blocks, or fold adopt
      into the Backlog v3 reader (which must read cf's dialect anyway). Found by C and D.
- [ ] Non-param mustache lands in `value:` and gen single-quotes it (`tags: '{{ toYaml … }}'`);
      `.observed.composite.resource.spec.X` is not recognised as a param; nested maps are
      flattened to dotted paths gen then refuses; arrays/objects serialised via fmt.Sprint
      (`'[map[conditionStatus:False …]]'`); composed apiVersion is rebound to `--provider`
      (cluster-scoped `iam.aws.upbound.io` → `.m.`); without `--provider`, `sources: null`.

### P2 — DSL expressiveness gaps every scenario hit

- [ ] The escape hatch does the real work: nested objects (until P0 lands), array elements,
      typed literals, quoted strings, XR-derived names (`{{ $xr }}-sa-key`), per-index values
      (`printf "10.0.%d.0/24" $i`, `index (list …) $i`) and aggregate status wires over a
      forEach set (a 500-character one-line `range … append … toJson` with a hand-written
      guard chain) all needed `raw:`. `raw:` must be single-line, and `$i`, `$spec`, `$xr`,
      `$xrMeta` are undocumented — every agent read composition.go to learn them. Document the
      raw contract now; then add first-class forms in this order: typed literals (P0 above),
      `resources.<looped>[*].status.<path>` list wires, forEach index helpers (cidr/az from
      index), XR-name interpolation in envelope/annotation values, paired forEach.
- [ ] `template:` cannot see observed resources (`map has no entry for key "observed"`), so
      any string built around a status wire (a redrive policy JSON, `serviceAccount:<email>`)
      is raw with a hand-copied 11-term guard. Give templates the observed map and a helper
      that emits the guard. A, E.
- [ ] Object param into a map leaf is refused; an explicit `tags[env]` entry replaces the
      convention wholesale instead of merging. A, C.
- [ ] Field-level `when` is rejected as `unknown field "when"` with no hint it is resource-level.
      Parameter named `n` fails as `parameters.false` (YAML 1.1). Kind `ProjectIamMember` gets
      "not found; run cf provider add" instead of the nearest match. `--check` ignores stale
      extra files in out/.

### P2 — discovery and CLI

- [ ] The kind list mixes `.m.` and cluster-scoped duplicates for a Namespaced XRD (backlog
      v2 labelled them, did not hide them). `cf provider add --help` and the providerconfigs
      ASSUMPTION note point at xpkg.upbound.io/upbound while everything else is
      ghcr.io/crossplane-contrib. A, C.
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

## Backlog v5 — whole-tree code audit (2026-09-02, main @ ee61f82)

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

- [ ] `(*Blueprint).Validate` (internal/blueprint/load.go:408-1000) is 592
      lines, the largest unit in the repo and the one place every authoring
      mistake has to be caught: XRD, parameters, resources, fields, when,
      forEach, envelope, annotations, pipeline and templates in one pass. Its
      own neighbours show the split — `validateStatusRef`,
      `validateForEachParamRef`, `validateForEachStatusRef` already sit beside
      it. Extract `validateXRD` / `validateParameters` / `validateResources` /
      `validateFields` in the same style; each becomes directly testable
      instead of reachable only through a whole blueprint. Half a day, low
      risk (package is at 90.3% coverage, error strings pinned by tests).
- [ ] The three emitters walk the same tree three times:
      composition.go:writeTemplateBody (279 lines),
      python.go:pythonTemplateBody (165), kcl.go:kclTemplateBody (164).
      Diffing the KCL and Python bodies gives 99 changed lines out of ~165 —
      the same traversal (refuse conventions, resolve kind, plan
      fields/envelope/annotations, open when, open forEach, write
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
- [ ] palette.js `init` is 882 lines and output.js `init` is 830 — each
      module's entire body lives inside its init, so nothing in it can be
      reached or tested in isolation. Larger and separate from the dispatch
      chains above; do those first and reassess.

### Coverage and CI

- [ ] Coverage thins exactly where the tree touches the outside world:
      internal/cluster 55.0%, internal/cache 64.3%, cmd/cf 71.9%,
      internal/xpkg 72.6%. internal/cluster is both the newest package and the
      only one that talks to a live API server. Bring its error paths
      (unreadable kubeconfig, unreachable server, partial CRD listings) up as
      part of the kind-cluster work above, not after it.

### Workspace hygiene (needs a decision, not a delete)

- [ ] The working copy is ~710 MB, of which ~625 MB is stale agent worktrees:
      .claude/worktrees/ (321 MB, 7 worktrees), .worktrees/ (145 MB, 4), plus
      web/ (159 MB, the retired React canvas — already out of git, still on
      disk). Of the 20 local branches besides main, 15 are fully merged and
      exist only as worktree anchors. Five carry unmerged commits and need a
      call each: subagent-DX--Client---Test-Suite-Polish-Engineer-self-37c2a470
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
