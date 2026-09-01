# compositionfactory

`cf` generates Crossplane v2 Compositions and CompositeResourceDefinitions
(XRDs) from a provider's own schemas. `cf provider add` pulls one layer of a
provider's xpkg OCI image (not the whole multi-hundred-MB image) and extracts
its CustomResourceDefinitions; every field a blueprint sets is then checked
against that CRD's real `spec.forProvider` schema at generate time, so a
typo'd field path fails loudly instead of being silently pruned by the
Kubernetes API server on apply.

One engine, `internal/emit`, backs every front door: the `cf gen` CLI, the
`cf serve` HTTP API, and the canvas GUI in `web-proto/` all call the same
`emit.Generate` and produce byte-identical output for the same blueprint.
Output is plain YAML meant to sit in a Git repo an existing GitOps pipeline
already syncs — there is no database and no cluster requirement; `cf gen`
touches neither.

## Quickstart

**1. Build.**

```sh
make build          # -> bin/cf, version stamped from git describe
```

**2. Add a provider.** No cluster, no Docker — this pulls CRDs anonymously
from the registry and pins the resolved digest into `.cf.lock`.

```sh
bin/cf provider add ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0
```

**3. Author a blueprint.** `testdata/xqueue.cf.yaml` is a real one (it backs
the acceptance test):

```yaml
apiVersion: factory.crossplane.io/v1alpha1
kind: Blueprint
metadata:
  name: xqueue
spec:
  sources:
    - provider: ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0
  xrd:
    group: platform.hooli.tech
    kind: XQueue
    plural: xqueues
    version: v1alpha1
    scope: Namespaced
    parameters:
      location:       {type: string, required: true, enum: [EU, US]}
      providerName:   {type: string, required: true}
      maxMessageSize: {type: integer, default: "2048"}
  resources:
    - name: main-queue
      kind: Queue
      provider: ghcr.io/crossplane-contrib/provider-aws-sqs:v2.7.0
      fields:
        region:         {value: "eu-north-1"}
        maxMessageSize: {from: params.maxMessageSize}
```

A resource `fields` entry is exactly one of `value` (a literal), `from`
(`params.<name>`, a bare template dereference), or `raw` (literal go-template
text, written verbatim, unquoted — the escape hatch). See
[The blueprint DSL](#the-blueprint-dsl) below for all three used together.

**4. Generate.**

```sh
bin/cf gen testdata/xqueue.cf.yaml -o out
# wrote out/compositions/xqueues.platform.hooli.tech.yaml
# wrote out/functions.yaml
# wrote out/xrds/xqueues.platform.hooli.tech.yaml
```

`cf gen --check` writes nothing and instead exits `0` (in sync), `1` (tool
error) or `2` (the tree has drifted from the blueprint) — the exit code CI
distinguishes a broken generator from hand-edited generated YAML with.

**5. Verify with the real render pipeline.**

```sh
crossplane composition render testdata/xr.yaml \
  out/compositions/xqueues.platform.hooli.tech.yaml \
  out/functions.yaml \
  --xrd out/xrds/xqueues.platform.hooli.tech.yaml --timeout 5m
```

This needs the `crossplane` CLI and a reachable Docker daemon (the pipeline
functions run as containers); `functions.yaml` is a required third argument.

**6. Run the canvas.** `cf serve` is loopback-only by default and needs the
providers named in the blueprint already cached (step 2):

```sh
bin/cf serve --blueprint testdata/xqueue.cf.yaml --out out
```

In another shell:

```sh
python3 web-proto/serve.py
```

Open <http://127.0.0.1:5180>. `serve.py` is a static file server for
`web-proto/` that proxies every `/api/*` request to `cf serve` on `:8080`, so
every fetch the browser makes is same-origin.

## The canvas

`web-proto/` is plain ES modules and CSS — no framework, no build step. It is
a thin client: every mutation is a full-document `PUT /api/blueprint`, and
the palette, inspector and output panes render from what the server returns,
never from client-side state the server hasn't confirmed.

Built today:

- drag a kind from the palette onto the canvas to add a composed resource;
  drag cards to reposition (kept client-side for the session, not written
  into the blueprint)
- wire a resource field to an XRD parameter, including shared fan-out — one
  parameter driving several fields, shown with a `×N` badge and its own wire
  color
- three field modes per field row — **V**alue (literal), **W**ire
  (`from: params.X`), **R**aw (verbatim template text) — switchable inline
- add a provider by reference from the SOURCES rail; each source row shows
  its pinned digest and cached kind count
- live generate: edits debounce into a real `POST /api/generate`, and the
  output drawer shows the regenerated Composition, XRD and blueprint YAML
- a topbar Validate chip that runs a real `crossplane composition render`
  against a sample XR synthesized from the blueprint's own XRD
  (`POST /api/render`) and reports pass/fail/resources-rendered — not just
  "the YAML parses"
- undo/redo over a server-backed document history (buttons and Cmd/Ctrl+Z)
- pan and zoom (wheel, shift-wheel, on-screen zoom controls)
- duplicate and remove canvas objects
- a Guide tab covering the canvas, the DSL and the generate loop, plus
  mouseover text throughout
- resizable palette and inspector columns

`web/` is a separate, in-progress React rewrite of the same canvas
(`@xyflow/react`, `@rjsf/core`, CodeMirror); its API fixtures under
`web/src/api/fixtures/` are cross-checked against the live server's JSON
shapes by `internal/api/contract_fixtures_test.go`, but it is not the canvas
this README's quickstart runs.

## The blueprint DSL

`spec.xrd.parameters` is single-source: one declaration produces both the
XRD's OpenAPI schema and the template's default/required behavior — there is
no separate place to redeclare a default. Each parameter has:

- `type`: `string`, `integer`, `number`, `boolean`, or `object`.
  `object` is a free-form string map (`additionalProperties: {type: string}`
  in the emitted schema, no nested structure). `array` is rejected at
  validation time — the XRD emitter cannot yet write a structural `items:`
  schema for it, and a `from:` mapping would render Go's `fmt` of the slice
  (`[a b c]`), which is valid YAML and silently wrong. Use a scalar, or set
  the field with `raw:`.
- `required`: an unrequired parameter is only ever dereferenced behind a
  `hasKey` guard in the generated template, never bare.
- `enum` / `default`: `default` is only valid for `string`, `integer`,
  `number` and `boolean`.

A composed resource's `fields` map sets one field path to exactly one of:

```yaml
fields:
  region:         {value: "eu-north-1"}                 # value is ALWAYS emitted quoted — correct for a
                                                          # string field, wrong for a non-string one
  maxMessageSize: {from: params.maxMessageSize}          # {{ $spec.maxMessageSize }}; required params dereference
                                                          # directly, optional ones are hasKey-guarded
  fifoQueue:      {raw: "true"}                          # written verbatim, unquoted — the only way to emit a
                                                          # real (non-string) YAML scalar, or the escape hatch
                                                          # for anything else: a nested map, a template expression
```

Every field path is checked against the resolved CRD's own
`spec.forProvider` schema at generate time (branch paths too, so `raw:` can
still set a whole subtree); an unknown path fails with a nearest-match
suggestion rather than reaching the API server to be silently pruned. A
`from:` referencing a composite-typed (`object`) parameter is rejected for
the same silent-`fmt`-formatting reason `array` is.

`spec.xrd.scope` must be `Namespaced` — `Cluster` is parsed but not yet
implemented (the cluster-scoped managed-resource envelope differs and the
emitter doesn't render it), and `LegacyCluster` is not a valid v2 scope.
`providerName` is a required `string` parameter on every Namespaced XRD: the
Composition dereferences it unguarded for `providerConfigRef.name`.

There is no `forEach:` on a resource yet — one blueprint resource is exactly
one composed resource, with no fan-out over an array/map/count parameter.
`internal/blueprint.Resource` has no such field, so a hand-written
`forEach:` key is silently dropped on load (`sigs.k8s.io/yaml` ignores
unknown fields); the canvas's `.node` rendering has a dormant `r.forEach`
badge for it, but nothing on the engine side ever populates it. See
[Roadmap](#roadmap).

**Determinism.** Output is byte-identical for the same blueprint and cache
state: LF-only line endings, no trailing whitespace, sorted map keys
(parameters, fields, required lists), and exactly one trailing newline. Every
generated file opens with a three-line provenance comment (`Generated by
compositionfactory. Do not edit.` / the source blueprint path / `Regenerate
with: cf gen`) — comments, never annotations, so nothing here creates an
ArgoCD sync loop. This is treated as a correctness requirement, not a nicety:
on a `prune: true` GitOps repo, a churning generated file is a live-cluster
incident.

## Development

Go 1.25+. Node with `npm`/`npx` for the Playwright suite.

```sh
make build       # bin/cf, version stamped via -ldflags
make test        # go test ./... -short -count=1 — no Docker, no cluster, runs anywhere
make test-race   # the same, with -race
make test-docker # go test ./... -run Acceptance -v — needs Docker + the crossplane CLI
make test-e2e    # npx playwright test — needs `cf serve` already listening on :8080
make lint        # gofmt -l . && go vet ./...
make serve       # build + run `cf serve` over $(BLUEPRINT) (default testdata/xqueue.cf.yaml)
make dev         # web-proto's dev server (serve.py) on :5180
make clean       # remove bin/ and the default output directory
```

Two test suites:

- **Go.** `make test` is the lane that must pass anywhere: unit tests plus
  `TestAcceptanceXQueueRenders`, which skips itself (not a failure) when
  Docker or the `crossplane` CLI aren't on `PATH`. `make test-docker` is the
  same acceptance test forced to run, for CI's Docker-capable lane
  (`CF_REQUIRE_ACCEPTANCE=1` there turns a missing prerequisite into a hard
  failure, so a runner with a broken toolchain can't go green by skipping).
- **Playwright** (`tests/*.spec.js`, `playwright.config.js`). A behavior
  suite driving the live canvas against a live `cf serve`: core loop,
  add-provider, validate, duplicate/remove, content sizing, pan/zoom, shared
  parameters, guide tooltips, parameter types, column resize, undo/redo.
  `workers: 1` — the suite shares one live blueprint document and each test
  restores what it found, so parallel workers would corrupt each other's
  state. Playwright starts `serve.py` itself; `cf serve` on `:8080` is a
  precondition each spec checks and skips on if absent, so run `make serve`
  in another shell first.

## Roadmap

Not yet built, per
[`docs/superpowers/specs/2026-08-27-compositionfactory-design.md`](docs/superpowers/specs/2026-08-27-compositionfactory-design.md):

- **Status and ref wires beyond parameters** — drawing an edge from one
  resource's `status.atProvider` (or its native `<f>Ref`) into another
  resource's field, and `dependsOn:` compiling to a `function-sequencer`
  step.
- **`when:`** (conditional resources) **and `forEach:`** — a looped resource
  following Go template `range` semantics over an array, map or integer
  count, with an indexed `setResourceNameAnnotation` per iteration. The
  canvas currently refuses `type: array` parameters outright rather than
  half-supporting the fan-out this needs.
- **User-defined template functions** — a named `templates:` block
  (optionally bound to specific field names) emitted as go-template
  `define`/`include`, generalizing what would otherwise be a one-off
  "conventions" feature.
- **Native Kubernetes kinds** as a second schema source (vendored,
  per-minor-version OpenAPI; `cf k8s use <version>`), for composing plain
  Kubernetes objects alongside provider-managed resources.
- **An MCP server** (`cf mcp`) — full authoring parity over MCP, writes
  confined to a declared workspace root, `--read-only` for inspection.

Also not yet built: `cf validate`, `cf adopt`, `cf provider search|list|
versions|info|pin`, `cf index …`, RBAC/IAM emission (`emit: {rbac: true}`),
the aggregate connection Secret, and `Cluster`-scoped XRDs (`Namespaced` only
today).
