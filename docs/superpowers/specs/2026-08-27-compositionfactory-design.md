# compositionfactory — Design

**Status:** draft for review · **Date:** 2026-08-27 · **Repo:** `koorikla/compositionfactory`

Generate Crossplane Compositions and XRDs from provider schemas — through a drag-and-drop
canvas, a CLI, or an MCP server, all over one engine.

Grounded in two research documents in this repo, both built from measurement rather than
recollection:

- [`docs/research/2026-08-27-crossplane-generator-research.md`](../../research/2026-08-27-crossplane-generator-research.md)
  — schema sourcing, XRD v2, function-go-templating, validation tooling, prior art.
- [`docs/research/2026-08-27-composition-pattern-taxonomy.md`](../../research/2026-08-27-composition-pattern-taxonomy.md)
  — ~1,200–1,600 real Compositions across ~350–450 repos; 815 GCP + 563 AWS CRDs.

A clickable UX prototype exists at [`docs/design/canvas-prototype.html`](../../design/canvas-prototype.html).

---

## 1. Problem

Authoring a Crossplane Composition means hand-writing a Go template that produces managed
resources whose schemas live in an OCI package you cannot easily read. An EC2 `Instance` has 263
properties. The full AWS family is 2,065 CRDs and ~102 MB of YAML. Nothing in the ecosystem maps
those schemas into a composition body:

| Existing tool | What it does | Gap |
|---|---|---|
| `crossplane xrd generate` | XRD from an example XR or SimpleSchema | No provider awareness |
| `crossplane composition generate <xrd>` | A **12-line stub** with one `function-auto-ready` step | Emits zero composed resources |
| `crossplane composition render` | Full-fidelity local render | Our test harness, not a competitor |
| Upbound Console, KubeVela, Kratix | Adjacent | None generate a go-templating body from MR schemas |

The measured cost of the gap: **72–97% pairwise duplication between Compositions in five separate
repos**, and one file wasting 2,099 lines to a hand-unrolled loop.

## 2. Goals and non-goals

**Goals.** Provider-agnostic. One engine behind a GUI, a CLI, and an MCP server. Output is plain
YAML into a Git repo that ArgoCD already syncs. Reproducible: same blueprint + same lock = the same
bytes, forever.

**Non-goals, stated loudly** — each is a measured conclusion, not a deferral:

- **Round-tripping existing go-templates into the graph.** Proven infeasible: template AST TEXT
  nodes are not YAML, document shape is data-dependent, and indentation is semantic. We ship
  `adopt` instead (§9), which captures a foreign template verbatim as an opaque node.
- **Being the inner layer of someone else's templating system.** One corpus repo has 1,123 backtick
  escapes across 2,087 lines Helm-wrapping its Compositions. Users who need that use `variants:`
  or use something else. Said plainly rather than half-supported.
- **A cluster requirement.** `generate` touches no cluster and no Docker.

## 3. Architecture

```
                       ┌────────────────────────────────────────┐
  cf serve  ──HTTP──▶  │                                        │
  cf gen    ──direct─▶ │   internal/  — the only implementation │──▶ XRD · Composition
  cf mcp    ──MCP────▶ │                                        │    functions.yaml · RBAC
                       └────────────────────────────────────────┘
   schema/     blueprint/    emit/       refs/      render/     adopt/
   xpkg pull   IR + verify   templater   inference  crossplane  opaque
   CRD index                            (data)      render      import
```

**The load-bearing rule:** HTTP and MCP are thin adapters over `internal/emit`, never parallel
implementations. `POST /api/generate` unmarshals a blueprint and calls the same function `cf gen`
calls. A code path that exists only in the UI means the CLI cannot reproduce a UI-authored
artifact, and the GitOps story collapses. Public surface is `pkg/blueprint` and nothing else,
mirroring the Crossplane CLI's own `internal/` vs `pkg/` discipline.

**Stack.** Go 1.25+, `net/http` + `embed.FS` (no database — state is blueprint files in Git),
`alecthomas/kong` (the Crossplane CLI's own parser, so contributors read `cmd/` without a context
switch), `google/go-containerregistry` v0.22.0. Frontend: React 19 + `@xyflow/react` 12 (MIT; the
only canvas library rendering nodes as real DOM, which custom node forms require), `@rjsf/core` 6
(JSONForms rendered 15 inputs to rjsf's 88 on a real `LaunchTemplate` schema, and **silently
rendered nothing** for maps and `oneOf` — disqualifying), CodeMirror 6 (251 KB gzip vs Monaco's
899 KB minimum), `@dagrejs/dagre` (elkjs is 466 KB gzip and EPL/GPL). Measured first paint: **120
KB gzip** with the inspector and editor lazy.

## 4. Schema subsystem

**Providers come from xpkg OCI images — one layer, not the image.** The package layer is named in
the image config label `io.crossplane.xpkg:<digest>=base`. Fetching `provider-aws-sqs:v2` costs
**20,071 bytes across 5 requests, 1.84 s, anonymous, no Docker** — out of a 271,482,755-byte image.
A **13,500:1** reduction. Output is byte-identical (same SHA-256) to `crossplane xpkg extract`.

Three things the naive version gets wrong:

1. **Follow `spec.dependsOn` yourself.** `provider-aws-sqs` ships **zero** ProviderConfig CRDs —
   they live in `provider-family-aws`. `crossplane xpkg get-crds` does *not* resolve dependencies.
   Worse, **34.1% of GCP references cross API groups** (30.0% on AWS), so an editor that loads only
   the packages named in the blueprint fails to resolve a third of its edges. Load the family.
2. **Pick the `storage: true` version, skip `deprecated: true`, never `versions[0]`.** 14 of 102
   legacy EC2 CRDs serve two versions with inconsistent storage flags.
3. **Digest-pin in a lockfile.** `:v2` is a moving tag; without pinning the same blueprint emits a
   different Composition next month.

**Native Kubernetes kinds come from vendored OpenAPI**, pinned per minor version (`cf k8s use 1.34`),
because Crossplane v2 composes any Kubernetes object directly — **36% of v2 Compositions in the
corpus do**. This is a second source family, not an afterthought: `provider-kubernetes` solves a v1
problem.

**Delivery.** All 204 EC2 CRDs are **54 KB brotli** (18:1 — CRD schemas contain zero `$ref` and are
massively repetitive), so bundling was never the blocker. Schemas stay server-side for *structural*
reasons: N providers per family, descriptions are 71% of payload and are the field help text, and
the cluster is authoritative when present. Index eager (**1.0 KB brotli for 204 CRDs**), full schema
per-kind on demand (median 4.5 KB). Gzip/brotli middleware is mandatory — `http.FileServer` does not
compress.

## 5. Scope: the `.m.` fork

Every upjet provider ships each managed resource **twice** — verified 405/405 pairs on GCP and
equivalently on AWS. The envelopes are structurally different, so emitting the wrong one gets fields
**silently pruned by the API server**:

| Path | legacy (`*.upbound.io`, Cluster) | v2 (`*.m.upbound.io`, Namespaced) |
|---|---|---|
| `spec.deletionPolicy` | present | **absent** (0/102 EC2 m-variants) |
| `spec.providerConfigRef` | `{name, policy}` | `{kind, name}`, **both required** |
| `spec.writeConnectionSecretToRef` | `{name, namespace}` | `{name}` only |
| `<f>SecretRef` | `{key, name, namespace}` | `{key, name}` |

Fork on `.m.` in the API group, driven by the XRD's `scope`. **Never hard-code the envelope** —
`provider-kubernetes`'s `ObservedObjectCollection` has no `forProvider` at all. Compute
`envelope = spec.properties − {forProvider, initProvider}` and render what remains from its own
schema.

`LegacyCluster` is **not** a valid v2 `scope` (the docs are wrong), and omitting `scope` makes the
server default to `Namespaced` while `crossplane xrd convert` defaults to `LegacyCluster`. Always
emit `scope:` explicitly. Tolerate `LegacyCluster` on read; refuse to emit it.

## 6. Reference inference — the actual product, and the actual risk

There is **no machine-readable cross-resource link in any CRD**: zero vendor extensions across 344
CRDs from four providers. The target kind exists only as English prose in `description`.

**Detection is a 3-way conjunction** — shape, name suffix, *and* a parsing description. Exact on
both corpora: **1,042 true positives, 0 false positives, 0 false negatives.** Name-only admits
`nodeSelector`/`configMapRef`/`secretKeyRef`; shape-only admits `iam.Role.inlinePolicy`.

**Corrected grammar**, validated 2,084/2,084 across AWS and GCP:

```
^(?P<plural>References?) to (?:a )?(?P<Kind>\w+)(?: in (?P<group>\w+))? to populate (?P<field>\w+)\.(?:\n[\s\S]*)?$
^Selector for (?:(?P<list>a list of )|a )?(?P<Kind>\w+)(?: in (?P<group>\w+))? to populate (?P<field>\w+)\.(?:\n[\s\S]*)?$
```

Both originate in two `fmt.Sprintf` calls in `upjet/pkg/types/reference.go`. That single point of
generation is both the risk and its mitigation: **pin a golden test that parses one CRD per provider
family and asserts 100%**, converting "an unspecified convention we depend on" into a build-time
assertion.

**Use the `to populate <field>` capture for value-field resolution** — right 578/578 on GCP and
464/464 on AWS. The `{stem, stem+"s"}` heuristic is an AWS artifact (34 hand-written `RefFieldName:`
overrides in `provider-upjet-aws/config/`; GCP ships two lines) and would silently generalise a
provider-private decision.

**Default edge semantics are `matchControllerRef: true`** — 578/578 and 464/464 triads.
`<x>Ref: {name}` is the explicit-coupling form, and selecting it **auto-forces the target node to
`naming: explicit`**; the generator refuses to leave it generated.

**Prefer the native `<f>Ref` over templating `status.atProvider.id`.** What a reference resolves to
(`arn` vs `id` vs an arbitrary param path) lives in the provider's Go config and is unrecoverable
from the CRD. The controller knows; we do not. So: draw the edge, emit the ref, and never claim in
the UI to know which value crosses it. Reserve `status.atProvider` for values with no triad — and
never emit one unguarded, since they are absent on first reconcile.

**Five things must be per-provider data, not code:**

| # | Thing | Failure if hard-coded |
|---|---|---|
| 1 | Value-field resolution | Generalises an AWS-only artifact |
| 2 | **The location field** | `region` is required on 246/279 AWS MRs and **0/405 GCP**. GCP needs `location` (57), `region` (38), `zone` (11); `project` is never required. Emitting `region:` unconditionally produces **schema-invalid GCP compositions** |
| 3 | ProviderConfig schema + `credentials.source` enum | Fully disjoint between AWS and GCP |
| 4 | Short-group → API-group mapping | The description never carries scope → unresolvable edges |
| 5 | Package-set discovery | A third of edges unresolved |

**Portability verdict:** GCP is *cleaner* than AWS, but GCP and AWS share the upjet generator — so
testing GCP is **not** independent validation. The real question is a non-upjet provider
(`provider-helm`, `provider-terraform`), where the convention may not exist at all. Untested.

## 7. The blueprint DSL

Source of truth is a Kubernetes-shaped `Blueprint` (`apiVersion: factory.crossplane.io/v1alpha1`).
Generated files carry a do-not-edit header and **embed their blueprint**, so the tool reopens its own
output with perfect fidelity even if the blueprint is lost.

29 T1 patterns are first-class in v1. The ones that most shape the design:

- **`parameters:` is single-source.** One line emits both the XRD schema and the template's
  `| default`. 428 `default:` declarations across 117 XRDs, and "defaults written twice" is a
  documented pain in ≥2 repos.
- **Type-aware quoting**, derived from the CRD schema type — quote iff the type is string, always
  quote annotations and labels. Getting this wrong is fatal and it is not a style question.
- **`when:`** (23% of compositions wrap whole documents) and **`forEach:`** (25%, 165 blocks).
  A looped node's `setResourceNameAnnotation` **must** be indexed — a constant name inside a range
  collapses every iteration into one resource.
- **Node identity is the `composition-resource-name` annotation** — never user-editable, stable
  across regeneration. Absent, it is fatal.
- **`dependsOn:`** → one `function-sequencer` step from the transitive closure. Called the
  "highest-value single feature" by one brief.
- **Aggregate connection Secret**, not `CompositeConnectionDetails` — the latter is parsed and then
  **silently ignored** for v2 XRs. The generator refuses to emit it.
- **`emit: {rbac: true}`** — an aggregated ClusterRole over every composed native GVK. Uncounted in
  the corpus but named the "#1 why-nothing-happens failure".
- **`emit: {options: [missingkey=error]}` default on.** Only 3 of 381 compositions opt in today.

**`forEach:` follows Go template `range` semantics**, not a count-only loop. It ranges over an XRD
array, a map, or an integer count — because real fan-outs differ per item. Aurora reader instances
vary by `promotionTier`; a count-only loop cannot express that.

```yaml
forEach: {over: params.readers, as: r, name: "reader-{r.name}"}   # array of item specs
forEach: {over: params.replicas, as: i, name: "reader-{i}"}       # integer count
```

**Users define their own template functions.** `conventions:` was a special case of a more general
idea, and it should not be the only one: a named template the user writes once and calls anywhere,
emitted as go-template `define` / `include`. A convention is simply a function with a `bindTo`.

```yaml
templates:
  - name: stdLabels                       # callable as {{ include "cf.stdLabels" . }}
    bindTo: [labels, matchLabels]         # optional: auto-applies to fields with these names
    params: [slot]
    body: |
      app.kubernetes.io/managed-by: crossplane
      app.kubernetes.io/part-of: {{ .xr }}
      {{- range $k, $v := .tags }}
      {{ $k }}: {{ $v | quote }}
      {{- end }}
```

The generator owns `nindent`, which removes the main reason people hand-write `define` blocks
(upstream's own example concedes the indentation problem). Function bodies are raw template text, so
they carry the `rawPrelude` caveat from §7: variables cross the raw/structural boundary and a rename
inside a body is not type-checked. They get the same lint rule.

**Escape hatches are a T1 mechanism**, because they enable every T3: per-field `rawTemplate`,
per-node `rawPrelude`, whole-step `rawStep`. 5% of real compositions need a true escape.

**15 T3 patterns degrade to text.** Two cost a whole node or step; thirteen cost one field. The
largest by frequency is **references bound to a specific array element — 17.8% on GCP vs 6.2% on
AWS**, nearly 3×. `rawPrelude` is the escape flavour that most needs a lint rule, because variables
cross the raw/structural boundary and a rename breaks downstream mappings silently.

## 8. Correctness

**The `<no value>` defence.** A missing XR field renders the literal string `<no value>` into a live
managed resource at any depth — and because it is a legal string, the whole validate → render →
validate pipeline **exits 0**. This is the only defect class that reaches production. Five layers,
all from day one:

1. `options: ["missingkey=error"]` emitted **top-level**, not under `inline` (the function's README
   is wrong; the nested form is a fatal error).
2. Every optional field wrapped in `{{- with }}`.
3. Every dereferenced field marked `required` in the generated XRD, so the XR gate catches it upstream.
4. `grep -rn '<no value>\|<nil>'` in every generated Makefile.
5. The same guard in our own golden tests.

**Determinism is a correctness requirement, not a nicety.** On a `prune: true` + `selfHeal: true`
ArgoCD repo, a churning file is a live-cluster incident. Sorted keys, stable field order, LF only,
trailing newline, no version stamps, whitespace stripped from every template line. **Provenance in
YAML comments, never annotations** — an annotation creates a perpetual sync loop. Never emit a
default `kustomization.yaml`: it flips ArgoCD from Directory to Kustomize, after which any file
absent from `resources:` is **deleted** under `prune: true`.

## 9. Permissions from the canvas

The set of resources on the canvas determines the permissions the control plane will need, and
nothing in the ecosystem derives them. Two artifacts, two very different confidence levels:

**Kubernetes RBAC — derivable, high confidence.** Composing native objects requires the control
plane to hold rights on every composed GVK; without it the failure is the "#1 why-nothing-happens"
class. The GVKs are known at design time, which is precisely the case existing tools (`audit2rbac`,
`rbac-tool`) cannot serve, since they infer from observed traffic. `emit: {rbac: true}` produces an
aggregated ClusterRole over every composed native GVK.

**Cloud IAM — derivable only as a reviewed starting point.** Upjet providers are generated from
Terraform providers, so `Queue@sqs.aws.m.upbound.io` traces to `aws_sqs_queue` and from there to an
action set. The chain is real but each hop loses fidelity, and upjet additionally needs read/list
permissions for drift detection and tag permissions on most resources.

**A wrong policy is worse than none** — it either blocks provisioning or silently over-grants. So
every entry is attributed to the node that caused it and carries a confidence marker, and the
generated file says in a header comment that it is a starting point for review, not an authority.
Exact mechanism, dataset, licence and coverage are being researched separately
(`docs/research/2026-08-28-permissions-derivation.md`); §11 sequences RBAC ahead of IAM because the
first is cheap and certain and the second is neither.

## 10. Testing

`crossplane composition render` is **byte-for-byte deterministic** — verified three runs to an
identical `sha256:fd6085db8829…` against the live composition. Golden files are a real strategy.

Four layers, and CI splits into a Docker-free lane and a Docker lane because `render` needs a daemon
with network-create privileges many runners cannot give:

| Layer | What | Docker |
|---|---|---|
| 1 | Byte-exact goldens on emitted YAML (`testdata/` + `-update`) | no |
| 2 | `crossplane resource validate` (~0.5 s) | no |
| 3 | Render goldens behind a docker probe + `testing.Short()` | yes |
| 4 | The `<no value>` grep | no |

`functions.yaml` is a **required** third argument (`error: functions argument is required when not
in a project`) and is therefore a generated artifact. `render.crossplane.io/*` annotations are
**not** required — verified — but we emit `runtime-docker-name` because renders otherwise **leak
containers** (4 found still running after earlier renders).

`cf gen --check` exits `0` in sync, `1` tool error, `2` drift — so CI distinguishes "your generator
crashed" from "someone hand-edited generated YAML".

## 11. Interfaces

**CLI.** `cf provider add <xpkg-ref>` · `cf k8s use <ver>` · `cf gen [--check]` · `cf validate` ·
`cf adopt <composition.yaml>` · `cf serve` · `cf mcp`.

**GUI.** Node graph; wires are data dependencies compiled to template expressions. Four wire hues
chosen for **hue** separation (blue XRD 215°, teal status 173°, gold shared 51°, rust ref 15°), with
the ref wire dashed so the distinction never rests on hue alone. Required-first inspector with a
path-addressed tree — the mass of provider schemas sits at depth 3–5, so design for depth 5 and give
a raw-YAML escape below it. Do not build a virtualised form renderer: rjsf defaults arrays to zero
items, so Kyverno's 1,445-property `ClusterPolicy` renders in 19 ms producing 10 inputs.

A **Permissions** panel lists what the current canvas requires, attributed per node — Kubernetes
RBAC for native GVKs, cloud IAM for managed resources — so the answer to "why is nothing happening"
is visible before you apply rather than after.

**MCP.** Full authoring parity, writes confined to a declared workspace root, `--read-only` for
inspection. The context-window problem an agent has is the *same* problem the browser has — a 1.7 MB
CRD fits neither — so `schema_search` / `kind_describe(required_only)` / `kind_fields(path, depth)`
serve all three front doors.

## 12. Milestones

You asked for the full product and I am not scoping it down; it still has to be built in an order.
**The in-cluster web UI is out of v1** — it would drag in an OCI image, its own RBAC, and an auth
story, and `cf serve` on a laptop covers the authoring case.

| | Milestone | Proves |
|---|---|---|
| **M1** | xpkg ingest + index + lock; `cf gen` single resource | Reproduces your `XQueue` and passes `crossplane composition render` |
| **M2** | XRD builder; `parameters:`; required-first schema API | The API shared by GUI, CLI and MCP |
| **M3** | Canvas + wires + reference inference (data-driven, golden-tested) | The central risk, retired early |
| **M4** | Pipelines, `when`, `forEach`, `dependsOn`, user-defined templates, other functions as nodes | Full T1 |
| **M5** | MCP server, `adopt`, `functions.yaml` + **K8s RBAC** emission, distribution | Ship |
| **M6** | Cloud IAM derivation, once §9's dataset question is settled | Reviewed-starting-point policies |

## 13. Risks

1. **The upjet convention trap.** The whole reference layer rests on `upjet/pkg/types/reference.go`
   — one file, two `fmt.Sprintf` calls. *Mitigation:* data-driven layer + per-provider overrides +
   a golden test per provider family that asserts 100% parse.
2. **`<no value>` silent corruption.** *Mitigation:* §8, five layers, day one.
3. **Round-trip expectation.** Users will hand-edit generated YAML and expect the graph to follow.
   *Mitigation:* say non-goal loudly — README, `--help`, and a header comment on every emitted file;
   ship `adopt` so onboarding is lossless.
4. **GitOps churn cascade.** *Mitigation:* §8 determinism, enforced by byte-exact goldens.
5. **The Docker cliff.** *Mitigation:* §10 three-lane CI, `--timeout=5m`, named container reuse.

## 14. Open questions

1. **Is a non-upjet provider the portability anchor, and when?** GCP does not validate portability —
   it shares the upjet generator with AWS. `provider-helm` / `provider-terraform` are the real test.
2. **Does `cf` ever read a cluster?** `generate` needs none. But a cluster is the only way to populate
   the palette from **Active** `ManagedResourceDefinition`s and to resolve `functionRef.name` against
   installed Functions. This decides whether `client-go` is core or an optional adapter.
3. **How much IAM fidelity is worth shipping?** If the best dataset covers, say, 70% of AWS MRs, is a
   70%-complete reviewed policy useful or actively misleading? Answer depends on §9's research.

**Resolved since first draft:** `forEach` follows `range` semantics over array/map/count (§7);
user-defined template functions generalise conventions (§7); the in-cluster UI is out of v1 (§12).
