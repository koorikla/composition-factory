# Blueprint vs. Crossplane Configuration — decision memo

**Date:** 2026-09-02 · **Question (verbatim):** *"Would it make sense to replace this DOM with
Crossplane Configurations? It seems the Configuration spec is exactly the DOM we reinvented."*

**Answer: no — and the intuition is half right in a useful way.** The Configuration package is a
**distribution format for our compiler's output**, not an authoring model that could replace our
compiler's input. Its content model is *exactly* what `cf gen` emits (XRDs + Compositions, nothing
else is even permitted), which means we should **emit** one — `cf package` — not become one.

## 1. What a Configuration actually is

A Configuration package is "an OCI container image containing a collection of Compositions,
Composite Resource Definitions and any required Providers or Functions"
([docs.crossplane.io/latest/packages/configurations](https://docs.crossplane.io/latest/packages/configurations/)).
Its parts:

- **`crossplane.yaml` meta** — `apiVersion: meta.pkg.crossplane.io/v1, kind: Configuration`, with
  `spec.dependsOn` (Provider/Function/Configuration entries: `package` = registry path without tag,
  `version` = semver constraint) and `spec.crossplane.version` (minimum Crossplane).
- **Content** — the xpkg spec is strict: a Configuration's `package.yaml` stream must contain
  "exactly one `Configuration.meta.pkg.crossplane.io` object" plus XRDs and Compositions, and
  "Zero (0) other object types may be defined in the YAML stream"
  ([crossplane/crossplane/contributing/specifications/xpkg.md](https://github.com/crossplane/crossplane/blob/main/contributing/specifications/xpkg.md)).
- **Tooling** — `crossplane xpkg build --package-root=<dir>` bundles the YAML into a `.xpkg` OCI
  image; `crossplane xpkg push` pushes it ([CLI reference](https://docs.crossplane.io/latest/cli/command-reference/)).
- **In-cluster resolution** — installing a `Configuration.pkg.crossplane.io/v1` makes the package
  manager install `dependsOn` packages automatically, tracked in the singleton cluster-scoped
  `Lock` resource; `skipDependencyResolution: true` opts out
  ([v1.20 packages doc](https://docs.crossplane.io/v1.20/concepts/packages/)).

Crucially: inside a Composition, the go-templating body is an **opaque string** to Crossplane. The
Configuration spec's "DOM" bottoms out exactly where ours begins.

## 2. Mapping Blueprint ↔ Configuration

**Maps ~1:1 (Blueprint → package):**

| Blueprint | Configuration |
|---|---|
| `spec.sources` (`ghcr.io/...-sqs:v2.7.0`, digest-pinned in `.cf.lock`) | `spec.dependsOn` Provider entries (path + `version: "=v2.7.0"`) |
| Effective pipeline packages (`internal/emit/functions.go` — go-templating, auto-ready, declared steps) | `spec.dependsOn` Function entries |
| Generated XRD + Composition (`cf gen` output) | The package content — the *only* content allowed |
| `metadata.name` | `crossplane.yaml` `metadata.name` |

**Exists only in the Blueprint — no home in a Configuration.** Everything that makes cf a product
exists *before* compilation and is erased by it: `parameters:` as single-source (one line → XRD
schema + template default), the `{value|from|raw|template}` field grammar, wires (`from:
params.x` / `resources.x.status...` with their hasKey guard discipline), `conventions:` and
`templates:` as authoring constructs, `forEach`/`when` sugar, `envelope:`, `annotations:` authoring,
and the `k8s` native-kind family (vendored schemas — not a package, so not even expressible as a
`dependsOn`). After `cf gen`, all of it is one gotpl string Crossplane never parses.

**Exists in Configuration but not in the Blueprint:** package identity metadata
(`meta.crossplane.io/{maintainer,source,license,description,readme}` annotations), semver *range*
constraints (we pin exact digests), `spec.crossplane.version`, OCI distribution/marketplace
listing, in-cluster dependency auto-install via the Lock, and Configuration-on-Configuration
dependencies. This is real value we currently leave on the table: today a `cf` user must hand-apply
`out/` **and** hand-install providers/functions; a Configuration makes one `kubectl apply` install
all of it.

**The ecosystem corroborates the layering.** Nobody authors *in* the Configuration format: they
hand-write the YAML, or they use a layer above it — Upbound's project format (`upbound.yaml` +
KCL/Python/Go-templating "embedded functions", where `up project build` *generates* "Crossplane
configuration package metadata based on your project" and bundles the YAML into a package;
[docs.upbound.io/manuals/cli/howtos/building-pushing](https://docs.upbound.io/manuals/cli/howtos/building-pushing),
[embedded functions](https://docs.upbound.io/manuals/cli/concepts/embedded-functions/)). Upbound
hit the same gap and built the same shape of answer: an authoring model that *compiles to* a
Configuration. The Blueprint is our `upbound.yaml`.

## 3. Three architectures

**(a) Keep Blueprint; add `cf package` emitting a valid Configuration package. Cost: small
(days-to-a-week).** All inputs already exist: sources + pinned versions/digests (`.cf.lock`,
`internal/xpkg/fetch.go` already records digests), the function list (`emit.Functions`), and the
generated manifests. `go-containerregistry v0.22.0` is already in `go.mod` (we use it to fetch
provider CRD layers), so building the xpkg image natively — a base layer containing the
`package.yaml` stream, annotated `io.crossplane.xpkg: base`, deterministic (fixed timestamps,
sorted keys, same discipline as `cf gen`) — needs no new dependency and no Docker, preserving the
"no cluster, no Docker" goal. Risk: near zero; purely additive; the Blueprint can ride along as a
propagated annotation (the xpkg spec propagates unknown annotations unmodified), extending the
"generated files embed their blueprint" recovery property into the distributed artifact.

**(b) Make the Composition the source of truth (parse/edit generated-style Compositions; drop the
Blueprint). Cost: rewrite of load/validate/edit/canvas. Verdict: breaks.** The design doc already
proved the general case infeasible (§2 non-goal: "template AST TEXT nodes are not YAML, document
shape is data-dependent, and indentation is semantic" —
`docs/superpowers/specs/2026-08-27-compositionfactory-design.md`). Even restricted to cf-emitted
Compositions, the compiled form is lossy in one direction: a wire compiles to a guard chain + a
dereference, but many hand-edits to that text have no Blueprint preimage; `parameters:`
single-sourcing decompiles into "defaults written twice" (XRD `default` vs template `| default` can
now diverge); templates/conventions are already inlined at every call site; `forEach`/`when` become
raw `range`/`if` blocks whose bounds are un-derivable in general. And since generated files already
embed their Blueprint, "Composition as source of truth" degenerates in practice to reading the
embedded Blueprint back out — architecture (a) wearing a costume.

**(c) Full replacement by Configuration-native authoring. Cost: the whole product. Verdict:
category error.** The Configuration format has *no authoring surface*: its content is raw XRD +
Composition YAML. "Configuration-native authoring" means hand-writing gotpl Compositions — the
measured problem cf exists to remove (72–97% pairwise duplication, a 2,099-line hand-unrolled
loop). We would be deleting the DOM and keeping the compiled binary.

## 4. Recommendation and next milestone

**Adopt (a).** The Blueprint stays the authoring source of truth; a Configuration package becomes a
third output family alongside `compositions/`, `xrds/`, `functions.yaml`.

**`cf package <blueprint> [-o out.xpkg]`** — runs the existing emit engine, then:

1. Synthesizes `crossplane.yaml` deterministically from the blueprint:

   ```yaml
   apiVersion: meta.pkg.crossplane.io/v1
   kind: Configuration
   metadata:
     name: xqueue                      # blueprint metadata.name
     annotations:
       factory.crossplane.io/blueprint: |   # embedded source, same recovery story as cf gen headers
         <blueprint YAML, verbatim>
   spec:
     crossplane:
       version: ">=v2.0.0"
     dependsOn:
       - apiVersion: pkg.crossplane.io/v1
         kind: Provider
         package: ghcr.io/crossplane-contrib/provider-aws-sqs
         version: "=v2.7.0"           # source tag; exact pin, matching .cf.lock discipline
       - apiVersion: pkg.crossplane.io/v1
         kind: Function
         package: xpkg.crossplane.io/crossplane-contrib/function-go-templating
         version: "=v0.10.0"          # every fn from the effective pipeline (functions.go)
   ```

   Rules: one Provider entry per `spec.sources` (split `ref` into path + `=tag`); the `k8s` native
   provider label contributes **no** entry (vendored, not a package — `Validate` already refuses it
   in sources); one Function entry per effective-pipeline function, which needs pinned versions —
   today `functions.yaml` refs are unversioned, so `cf package` either takes `--function-version`
   pins or we add optional versions to `PipelineStep.Package` (small, honest schema addition).

2. Builds the `.xpkg` natively with go-containerregistry: single base layer, `package.yaml` =
   crossplane.yaml + XRD + Composition stream, `io.crossplane.xpkg: base` layer annotation,
   reproducible bytes (goldens like every other emitter). `crossplane xpkg build` compatibility is
   the acceptance test: our package must install identically to one built by the upstream CLI from
   the same `out/` tree.

**`cf push <ref>`** — pushes the built image with the same registry auth path `cf provider add`
already uses. Optional sugar; `crossplane xpkg push` works on our output from day one, which is the
real compatibility bar.

Acceptance: `cf package` on `testdata/xqueue.cf.yaml` → install the `.xpkg` in a kind cluster with
Crossplane v2 → the Configuration reports healthy, the Lock shows provider-aws-sqs and both
functions resolved, and `crossplane composition render` output matches `cf gen` goldens byte-exactly.

## Sources

- https://docs.crossplane.io/latest/packages/configurations/ — Configuration packages, crossplane.yaml, dependsOn, build/push
- https://docs.crossplane.io/latest/cli/command-reference/ — `crossplane xpkg build` / `push` flags and behavior
- https://github.com/crossplane/crossplane/blob/main/contributing/specifications/xpkg.md — xpkg image format, content restrictions, annotation propagation
- https://docs.crossplane.io/v1.20/concepts/packages/ — Lock resource, `skipDependencyResolution`, in-cluster dependency install
- https://docs.upbound.io/manuals/cli/howtos/building-pushing and https://docs.upbound.io/manuals/cli/concepts/embedded-functions/ — Upbound project format as prior art for authoring-layer-above-Configuration
- Local: `internal/blueprint/types.go`, `internal/emit/functions.go`, `internal/xpkg/fetch.go`, `README.md` (Blueprint DSL), `docs/superpowers/specs/2026-08-27-compositionfactory-design.md`
