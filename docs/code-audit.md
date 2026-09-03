# Code audit — 2026-09-02

Tree audited: `main` at `ee61f82`, clean and level with `origin/main`.

This is a whole-tree audit run after the consolidation backlog and Backlog v2
closed, so it deliberately does not restate their items. Every finding below
was measured on this tree; the command that produced each number is given so a
later run is comparable rather than impressionistic.

**Scope caveat.** This audit is static: it reads the tree, the tooling and the
shapes. It never built a composition or applied one to a cluster. Backlog v4
ran the opposite method against the same commit — five agents building real
compositions end to end — and found P0 defects in emitted output that nothing
here could surface: nested `forProvider` paths emitted as literal dotted keys,
`value:` always quoted regardless of CRD type, `resolveKind` matching on Kind
while ignoring `provider:`. Read that backlog alongside this. The grades below
are grades for the codebase as an artifact, not a claim that it generates
correct YAML; where the two disagree, the dogfooding wins, because it checked.

## Re-verified — 2026-09-04, at 0d3e914

Tree clean and level with `origin/main`. Every command in the Method section was
re-run, so the numbers below are directly comparable to the two passes under
them. This pass added one thing the previous two did not do: it **rendered the
same blueprint through all three engines and diffed the composed output**. That
is where the only serious finding came from, and it is the same class the scope
caveat warned about — static analysis cannot see it, and neither can a golden
test that pins each emitter against its own output.

### Closed

- **The Lane C round-trip gate now asserts the round trip.** The previous pass's
  strongest finding — `scripts/cluster/test-cluster.sh` applied, read back,
  re-imported and then checked only for a non-empty file — is fixed
  (`35f91e9`, `19a99c9`, `27e6510`, `0d3e914`). Verified independently at the
  CLI on a mixed native + provider blueprint: `cf gen` → `cf adopt` → `cf gen`
  reproduces the original bytes exactly, with the `# Source:` comment as the
  sole difference (which the gate ignores by design).
- **Workspace hygiene is fully recoverable for the first time.** All five
  non-`main` branches are now `0` commits ahead of `main`
  (`git rev-list --count main..<branch>`) — the previous pass had five carrying
  unmerged work. Two stale worktrees remain on disk
  (`~/compositionfactory-engine`, `~/compositionfactory-parity`), both on fully
  merged branches, so both are safe to remove. Working copy is down to 166 MB
  from 178 MB.
- **`cf catalogue --kind` and the `deadcode` regression stay closed.** Five hits
  now, up from four, and the new one — `emit.PreviewExpression`
  (`internal/emit/preview.go:42`) — is a fifth test seam of the same kind: a
  context-free wrapper whose production callers all go to
  `PreviewExpressionContext`. No dead production code.

### Movement

| Measure | ee61f82 | 47949a3 | **0d3e914** |
|---|---|---|---|
| Go test functions | 724 | 815 | **891** |
| Playwright behaviors | 150 | 164 | **180** |
| Production Go LOC | 16 458 | 19 888 | **23 262** |
| Test Go LOC (unit) | 25 398 | 29 415 | **32 615** |
| `internal/cache` coverage | 64.9 % | 64.9 % | **46.7 %** |
| `internal/adopt` coverage | 85.9 % | 78.6 % | **76.5 %** |
| `internal/emit` coverage | 87.7 % | 83.3 % | **81.0 %** |
| `deadcode` hits | 4 | 4 | **5** (all test seams) |
| Branches ahead of `main` | — | 5 | **0** |
| Working copy | 731 MB | 178 MB | **166 MB** |

`gofmt`, `go vet`, staticcheck, the race detector and `npm audit --omit=dev` are
all clean.

Measured on the committed tree at `0d3e914`. A concurrent session was editing
the working copy during this pass — including `internal/cache/store.go` and a
new `internal/cache/sources_test.go` — so the cache coverage figure is the one
most likely to have moved by the time this is read. Both findings below were
re-verified against that dirty tree after the fact and still reproduce.

**Coverage is the one number moving the wrong way, in the three packages doing
the most work.** `internal/cache` has now fallen 18 points across this pass
alone and is the lowest in the tree by a wide margin; `adopt` and `emit` have
each drifted down for a third consecutive pass. None of this is alarming in
absolute terms, but the trend is consistent and it is concentrated in exactly
the packages the round-trip rule depends on.

### New

- **The three engines do not compose the same resources — `metadata.name` is
  missing from native kinds under KCL and Python.** Filed as **CF-045 (P0)**.
  The go-templating emitter writes `name: {{ $xr }}-<resource>`
  (`internal/emit/composition.go:301`); the KCL and Python emitters write a
  `metadata` block carrying only the composition-resource-name annotation
  (`internal/emit/kcl.go:72-79`, `internal/emit/python.go:73`), so Crossplane
  falls back to `generateName: <xr>-` and the object gets a random name. Both
  emitters nonetheless resolve `from: resources.<n>.metadata.name` to the
  *deterministic* name, so they emit a reference to a name that will never
  exist. `crossplane composition render` exits 0. This is the defect archived as
  completed on 2026-09-03 — with the real-cluster failure
  `serviceaccount "web" not found` recorded against it — fixed in one engine of
  three. It is the third time the archive records KCL and Python diverging from
  go-templating on a correctness fix: CF-004 (dotted paths flattened into literal
  keys, "the v0.8.0 fix never propagated to the other two engines") and the
  earlier item where both emitters dropped every field, envelope and annotation
  guard. The pattern, not the instance, is what needs a gate.
- **Generated bytes depend on how the blueprint was named on the command line.**
  Filed as **CF-046 (P1)**. `blueprintSource` (`internal/emit/yaml.go:76-81`)
  returns `b.SourcePath()` verbatim, which `internal/blueprint/load.go:346` sets
  from the caller's argument, so `cf gen testdata/x.cf.yaml` and
  `cf gen $PWD/testdata/x.cf.yaml` produce different files from identical
  inputs, and `cf gen --check` exits 2 on an unchanged tree. `internal/emit/rbac.go:89`
  already does the deterministic thing and uses `b.Metadata.Name`.
- **`docs/mcp.md` claims a "full authoring surface" the MCP server does not
  have.** Filed as **CF-047 (P2)**. A live `tools/list` returns 15 tools: full
  parameter CRUD and no resource CRUD at all, against four resource routes on
  the HTTP API. An agent can only add a composed resource by rewriting the whole
  document with `replace_blueprint`.
- **Nothing statically analyses `web-proto/`.** Filed as **CF-048 (P3)**. Just
  under 8 000 lines of ES modules ship with no linter, formatter or type check;
  the Playwright suite is the only gate, and `tests/helpers.js` exists because
  an uncaught page error once shipped green through it. The Scorecard's
  "Correctness tooling: A" is a grade on the Go half of the tree only.
- **Two hygiene notes, reported rather than filed.** `tests/` has two specs
  numbered 66 (`slice66-canvas-engine-touch-polish`,
  `slice66-canvas-ux-visibility`), which breaks the one-number-one-behavior
  convention both testing skills tell agents to extend; and `rbac.yaml`'s
  provenance header omits the `# Regenerate with: cf gen` line every other
  generated file carries. Neither costs a user anything today.

### What this pass confirmed is working

Named because three of the four findings above are about surfaces, not the
engine, and that ordering is the real result.

- **The headline validation claim holds, in both directions.** A typo'd field
  path fails loudly with the nearest match and a reason, for native and provider
  kinds alike, exits 1, and writes nothing:
  `field "spec.replicaz" is not in the native Deployment schema; did you mean "spec.replicas"?`
- **The RBAC trap is handled correctly and loudly.** Composing a `Job` emits
  `rbac.yaml` with the exact aggregation label, all seven verbs, only the
  non-pre-granted kinds, and a stderr warning naming the file to apply.
  Composing only `Deployment`/`Service` correctly emits nothing.
- **`options: ["missingkey=error"]` is emitted as a sibling of `inline`**, which
  is the form that works rather than the form the function's own README shows.
- **The local round trip is byte-exact** on a blueprint mixing a provider
  managed resource with native kinds, through the Configuration-directory form
  of `cf adopt`.

### Second pass, same day — the cross-engine render comparison

CF-045 was fixed in the main tree by another agent while this audit was running;
verified independently at the CLI — KCL and Python now emit `name: demo-sa-sa`
and the sibling reference resolves. The fix ships a unit test asserting KCL
source substrings; it does **not** ship the rendered comparison the finding
asked for, and `acceptance_test.go` is untouched.

So this pass ran that comparison by hand: one blueprint, generated with each of
`--engine go-templating|kcl|python`, rendered through the real
`crossplane composition render`, and diffed resource by resource. The first
blueprint put through it was the shipped `k8s-workload` starter, and it
diverged — `data.PORT` renders as `"8080"` under go-templating and `8080` under
the other two, which the API server rejects
(`cannot unmarshal number into Go struct field ConfigMap.data of type string`,
confirmed by `kubectl apply --dry-run=server`). Filed as CF-056; the missing
gate itself is CF-057.

That is the whole argument for the gate, made in one run: three instances of
this class are now on record (CF-004, CF-045, CF-056), each found by a person
or an agent rather than by CI, and each time the fix was verified with a test
that could not have caught the next one.

## Re-verified — 2026-09-03, at 8b58a1d, 31bd674 and 47949a3

47 commits and two releases (v0.8.0, v0.9.0) after the audited tree. Every gate
in the Method section was re-run and every finding re-measured with the same
commands, so the numbers here are directly comparable to the ones below them.
The second pass is folded in: where a finding moved twice, both steps are shown.

### Closed

- **Finding 1, `(*Blueprint).Validate` at 592 lines.** Now a 22-line
  orchestrator over `validateRoot` / `validateSources` / `validateXRD` /
  `validateParameters` / `validateTemplates` / `validateResources` /
  `validatePipeline`, split across four new `validate_*.go` files. `load.go`
  fell from 1109 lines to 557. This is exactly the shape the finding asked for.
- **Finding 6, the race detector missing from CI.** Lane A now runs
  `make test-race` after `make test`.
- **The strongest recommendation this audit made is in place.** CI has a Lane C
  that stands up a real kind cluster with Crossplane and the pipeline
  functions and runs `make test-cluster` — unconditionally, on every push and
  pull request. That is the gate that closes the blind spot the scope caveat
  named, the one v4's P0s walked straight through.
- **The three small fixes from the first pass are holding**: lint scoped to
  tracked files, `.dockerignore` trimmed, builder image pinned. Dependabot now
  maintains the pin, and the toolchain agrees in all seven places it is
  declared — `go.mod` at 1.27.0, five CI `go-version` keys, and
  `golang:1.27-alpine`.

### Movement on the rest

| Finding | audit (ee61f82) | 8b58a1d | 31bd674 | 47949a3 |
|---|---|---|---|---|
| 2. Emitter traversal | 279 / 165 / 164 | 294 / 186 / 176 | **194 / 147 / 137** | **124 / 114** bodies, shared refusal |
| 3. JS `init` closures | 882 / 830 | 906 / 835 | 906 / 835 | **22 / 47** |
| 3. JS dispatch ladders | 317 / 316 / 295 | unchanged | unchanged | **322 / 16 / 183** |
| 4. `internal/cluster` coverage | 55.0 % | 55.0 % | **78.5 %** | 78.5 % |
| 5. Working copy | 731 MB, 20 branches | 807 MB, 27 branches | 313 MB, 15 branches | **178 MB**, `web/` gone |

Findings 2, 4 and 5 turned around in the second pass. A shared
`planSingleResource` (`internal/emit/plan.go`) now does the validation, kind
resolution, conventions merge and field planning for all three emitters, which
is the lift the finding asked for; `internal/cluster` gained error-path and
kubeconfig tests; the merged worktrees were pruned.

By the third pass every original finding is closed or nearly so.
`refuseGoTemplateOnlyFeatures` deduplicated the engine preamble, `web/` came off
disk, and the two 800-line `init` closures became 22 and 47 lines. Two
remainders, both verified in code rather than inferred:

- **`canvas.js openFieldPicker` is unchanged at 322 lines**, though it was
  archived as converted. The original finding also mischaracterised it: it is
  not an action ladder but a builder doing search, type compatibility, relevance
  scoring, category grouping and rendering in one function across 43 branch
  points. It needs those extracted, not a dispatch table. The two genuine
  ladders in `inspector.js` were converted — `onBoxClick` 316 → 16.
- **Five branches still carry unmerged commits**, against 10 merged anchors.

### New

- **Coverage fell in the two packages that took the most feature work, then
  partly recovered.** `internal/adopt` 85.9 % → 77.3 % → 78.6 % and
  `internal/emit` 87.7 % → 82.8 % → 83.3 %. Healthy in absolute terms, but the
  new code is still thinner-covered than what it sits beside. `internal/cache`
  (64.9 %) and `internal/xpkg` (72.6 %) have not moved across any pass.
- **`deadcode` went from four unreachable functions to six, then back to five.**
  `catalogue.Kinds` was wired into the `cf catalogue` package table.
  `catalogue.PackagesForKind` (`catalogue/kinds.go:240`) is still exported,
  still tested, still called by nothing.
- **`cf catalogue --kind` did not filter by kind** — found while checking the
  above; the two were the same defect. Fixed at 47949a3: the flag is wired to
  `PackagesForKind`, `--kind Bucket` no longer returns `provider-bitbucket-server`,
  and its output now differs from the free-text search. `deadcode` is back to
  four hits, all test seams — the pre-audit baseline.
- **The Lane C round-trip gate does not assert the round trip.**
  `scripts/cluster/test-cluster.sh:138-158` applies, reads the XRD and
  Composition back with `kubectl get -o yaml`, imports and regenerates — then
  checks only that the regenerated composition file is non-empty, and swallows a
  lossy import with `|| [ $? -eq 2 ]` without inspecting what was dropped. It
  would pass with one resource where the original had five. Nothing in the Go
  suite compares regenerated against original composition bytes either. The
  scaffolding is right and the assertion is a placeholder — which matters more
  than any structural finding in this document, because this gate is what the
  round-trip rule is enforced by.

### Scale, for context

The tree grew through all of it: 724 → 815 Go test functions, 150 → 164
Playwright behaviors, 16 458 → 19 888 production Go lines, 25 398 → 29 415 test
lines. `gofmt`, `go vet`, staticcheck, the race detector and `npm audit` are all
still clean.

## Method

```bash
gofmt -l $(git ls-files '*.go')                       # formatting
go vet ./...                                          # vet
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...  # deeper analysis
go run golang.org/x/tools/cmd/deadcode@latest ./cmd/... # reachability
go test ./... -short -count=1 -cover                  # tests + coverage
go test ./... -short -race -count=1                   # data races
npm audit --omit=dev                                  # JS dependency CVEs
```

Plus a read of the largest units by line count (Go functions, JS functions,
files), the CI workflows, the Dockerfile and deploy manifests, and a scan for
committed secrets.

## Scorecard

| Area | Grade | Basis |
|---|---|---|
| Correctness tooling | A | vet, staticcheck and the race detector are all clean; nothing suppressed — but see the scope caveat: none of it renders or applies |
| Tests | A | 724 Go test functions + 150 Playwright behaviors; 25 398 test LOC against 16 458 production LOC |
| Coverage | A− | 80–100 % across the packages that matter; two soft spots (below) |
| Dead code | A | four "unreachable" hits from `deadcode`, all four test-only helpers or a second `main` — no dead production code |
| Security | A− | no committed secrets, non-root image, loopback-only bind guard; one documented, deliberate exception in the k8s manifest |
| Dependencies | A | 5 direct Go modules; 3 npm devDependencies; `npm audit` reports 0 vulnerabilities |
| Structure | B | five functions carry disproportionate complexity (below) |
| Reproducibility | B+ | strong in the engine, weaker in the build image (now fixed) |
| Workspace hygiene | C | 731 MB working copy, of which ~625 MB is stale agent worktrees |

Overall: **A−** *as an artifact* — well-kept, well-tested, cheap to change.
The remaining work in this dimension is structural (a handful of oversized
units) and janitorial (workspace state).

That is not the same as "correct", and Backlog v4 is the proof: a suite of 874
tests stayed green through defects that break real applies. The reason is
visible in the grades above — the emitter's tests pin its bytes against its own
goldens, so an emitter that is wrong the same way twice passes. Every gate in
the Method section shares that blind spot. The strongest single recommendation
this audit can make is therefore not in its own findings list: it is that the
kind-cluster lane already on the backlog becomes the gate that closes it.

## What is already healthy — and worth not regressing

Stated because an audit that only lists problems misrepresents the tree.

- **No dead production code.** `deadcode ./cmd/...` reports exactly four
  unreachable functions: `catalogue.Validate` (reached from the
  `scripts/build-catalogue` main, not the `cf` main), `xpkg.PackageStream` and
  `cache.Store.Clear` (test-only), and `cmd/cf.defaults` (a documented test
  seam onto kong's own default resolution). Nothing to delete.
- **The test pyramid is real.** 1.54 lines of test per line of production code,
  and the acceptance lane renders through the actual `crossplane composition
  render` rather than asserting on strings.
- **Errors are written for the person who has to fix the blueprint** — they
  name the bad path, the nearest match, and the YAML to write instead. That is
  a deliberate style, and it is why this audit configures staticcheck's ST1005
  off rather than reflowing them.
- **The bind guard is right.** `cf serve` refuses a non-loopback `--addr`
  unless the operator passes `--i-know-this-is-unauthenticated`, and the error
  says what is wrong *and* why it matters.

## Findings

As written on 2026-09-02. Current status for each is in the re-verification
above; findings 1 and 6 are closed.

### 1. `(*Blueprint).Validate` is 592 lines — Medium

`internal/blueprint/load.go:408-1000`. The single largest unit in the
repository, and the one place every authoring mistake has to be caught. It
validates the XRD, parameters, resources, fields, `when`, `forEach`, envelope,
annotations, pipeline and templates in one pass.

Its neighbours show the shape the split should take: `validateStatusRef`,
`validateForEachParamRef` and `validateForEachStatusRef` already sit beside it
as separate functions. Extracting `validateXRD`, `validateParameters`,
`validateResources` and `validateFields` in the same style is mechanical, and
each becomes directly testable rather than reachable only through a whole
blueprint.

Effort: half a day. Risk: low — `internal/blueprint` is at 90.3 % coverage,
and the error strings are pinned by tests, so a behaviour change shows up
immediately.

### 2. The three emitters triplicate one walk — Medium

`internal/emit/composition.go:writeTemplateBody` (279 lines),
`python.go:pythonTemplateBody` (165), `kcl.go:kclTemplateBody` (164).

Diffing the KCL and Python bodies gives 99 changed lines out of ~165: the two
functions are the same traversal — refuse conventions, resolve the kind, plan
fields/envelope/annotations, open a `when`, open a `forEach`, write
apiVersion/kind/metadata/spec/forProvider — differing only in the syntax
tokens each language needs. `composition.go` runs the same traversal a third
time.

The consolidation backlog already did the hard half of this by making the plan
structured (`structuredRHS` carries kind, param, path and guard, so KCL and
Python no longer re-parse Go-template text). What remains is to lift the walk
itself: one `walkResources` driving a small backend interface
(`openResource`, `writeKey`, `openMap`, `formatLiteral`, `condition`, `loop`),
with the three current bodies becoming the three backends.

Effort: two days. Risk: medium, but bounded — every emitter path has a
byte-pinned golden, so a walk that changes one byte fails immediately. Do not
start it without the goldens green first.

### 3. Two JS modules are one closure each — Medium

`web-proto/js/regions/palette.js:init` is 882 lines and
`web-proto/js/regions/output.js:init` is 830 — each module's entire body lives
inside its `init`, so nothing in it can be reached, reasoned about or tested in
isolation. Three more functions are oversized for the same reason they grew:
they dispatch on an action attribute through a long `if/else` chain.

| Unit | Lines |
|---|---|
| `palette.js` `init` | 882 |
| `output.js` `init` | 830 |
| `canvas.js` `openFieldPicker` | 317 |
| `inspector.js` `onBoxClick` | 316 |
| `inspector.js` `onBoxChange` | 295 |

The dispatch chains have an obvious replacement: a `const actions = { … }` map
keyed by the `data-a` value, one small named function per action. That is a
mechanical change with 150 Playwright behaviors underneath it, and it is the
single biggest legibility win available in the canvas.

Effort: one day for the three dispatchers; the two `init` closures are a
larger, separate job. Risk: low for the dispatchers *provided* the Playwright
suite runs — note that it does not run on a shared port safely while another
agent session is live (see AGENTS.md §2).

### 4. Coverage thins exactly where the tree touches the outside world — Low

`internal/cluster` at 55.0 % and `internal/cache` at 64.3 % are the two lowest,
and `internal/cluster` is both the newest package and the only one that talks
to a live API server. `cmd/cf` (71.9 %) and `internal/xpkg` (72.6 %) follow.

The gap is not alarming in isolation, but the live-cluster schema source is on
the backlog to be re-pointed at a real kind cluster; that work should bring
`internal/cluster`'s error paths (unreadable kubeconfig, unreachable server,
partial CRD listings) up with it rather than after it.

### 5. Working copy is 731 MB, ~85 % of it stale — Low, but immediate

| Path | Size | State |
|---|---|---|
| `.claude/worktrees/` | 321 MB | 7 worktrees; 4 fully merged into main |
| `.worktrees/` | 145 MB | 4 worktrees, all fully merged into main |
| `web/` | 159 MB | the retired React canvas; already removed from git, still on disk |
| `bin/`, `node_modules/` | 40 MB | build products, correctly ignored |

Of the 20 local branches besides `main`, **15 are fully merged** (zero commits
ahead) and exist only as worktree anchors. Five carry unmerged commits and need
a decision, not a delete:

| Branch | Ahead |
|---|---|
| `subagent-DX--Client---Test-Suite-Polish-Engineer-self-37c2a470` | 12 |
| `worktree-agent-a5a7710927acd2ff7` | 12 |
| `worktree-agent-a7f7485202bdacadb` | 4 |
| `subagent-Canvas---UX-Authoring-Engineer-self-84d324ac` | 1 |
| `worktree-agent-a247d77332728f705` | 1 |

This one is listed as a finding rather than fixed in this pass because
deleting another session's worktree is not a call an audit gets to make on its
own — and the `~/.gemini/antigravity/…` worktrees belong to a *live* parallel
agent.

### 6. Smaller items

- **`make lint` walked ten times the tree it lints.** `gofmt -l .` visited
  1701 `.go` files against the 163 this tree owns, because the agent worktrees
  are full checkouts of other branches. An unformatted file on an abandoned
  branch could fail the lint of the tree you are editing. *Fixed below.*
- **The build image floated its toolchain.** `FROM golang:alpine` on a project
  whose contract is byte-identical output for identical input. *Fixed below.*
- **The Docker build context carried ~300 MB it never reads** —
  `.dockerignore`'s bare `node_modules` matches only the context root, so
  `web/node_modules` went in, as did `.worktrees/`. *Fixed below.*
- **CI never ran the race detector.** `make test-race` exists and passes, but
  no lane invokes it. Worth adding to lane A or a nightly, given the API
  server's shared index and memoised schema trees.
- **Three `# TODO:` markers** in `internal/emit/providerconfigs.go:200-202` are
  intentional: they are emitted *into* the generated ProviderConfig scaffold as
  instructions to the operator, not left-behind notes.

## Fixed in this pass

Each verified with `make lint`, `make lint-strict` and `go test ./... -short`.

1. `internal/emit/kcl.go` — `buildEnvTree`'s `findOrCreate` was split into a
   declaration and an assignment, the shape needed only by a self-recursive
   closure. It never recurses. Collapsed to `:=` (staticcheck S1021).
2. `catalogue/catalogue.go` — a package-doc line began with `go:embed`, which
   the toolchain reads as a malformed compiler directive (SA9009). Reworded.
3. `staticcheck.conf` — staticcheck's default check set minus ST1005, with the
   reasoning in the file. With it, staticcheck is clean at zero findings.
4. `make lint-strict` — staticcheck pinned to v0.8.1 and run via `go run`, so
   it needs no install and cannot drift between a laptop and CI. Added to CI
   lane A and documented in AGENTS.md §3.
5. `make lint` — gofmt now runs over `git ls-files '*.go'`. Verified both ways:
   an unformatted tracked file still fails the gate.
6. `Dockerfile` — builder pinned to `golang:1.25-alpine`, the minor `go.mod`
   declares. Image rebuilt and smoke-tested.
7. `.dockerignore` — `**/node_modules`, `.worktrees`, `web` and `.demorun`
   excluded from the build context.
8. `CHANGELOG.md` — the repo had 13 tags and no changelog. Backfilled from the
   tag history, with an Unreleased section carrying the above.

## Recommended order

1. The workspace prune (finding 5) — needs a decision, then it is one command.
2. `Validate` split (finding 1) — highest complexity-per-risk return.
3. The three JS dispatch chains (finding 3) — biggest legibility win in the
   canvas; needs the Playwright suite on an unshared port.
4. Race detector in CI (finding 6).
5. Emitter walk unification (finding 2) — most valuable and most invasive;
   worth doing only with the goldens green and nothing else in flight.
6. `internal/cluster` error paths (finding 4), alongside the kind-cluster work
   already on the backlog.
