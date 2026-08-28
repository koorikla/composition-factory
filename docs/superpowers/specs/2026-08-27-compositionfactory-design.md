# compositionfactory — Design

**Status:** draft for review · **Date:** 2026-08-27 · **Repo:** `koorikla/compositionfactory`

Generate Crossplane Compositions and XRDs from provider schemas — through a drag-and-drop
canvas, a CLI, or an MCP server, all over one engine.

Grounded in four research documents in this repo, all built from measurement rather than
recollection:

- [`docs/research/2026-08-27-crossplane-generator-research.md`](../../research/2026-08-27-crossplane-generator-research.md)
  — schema sourcing, XRD v2, function-go-templating, validation tooling, prior art.
- [`docs/research/2026-08-27-composition-pattern-taxonomy.md`](../../research/2026-08-27-composition-pattern-taxonomy.md)
  — ~1,200–1,600 real Compositions across ~350–450 repos; 815 GCP + 563 AWS CRDs.
- [`docs/research/2026-08-28-permissions-derivation.md`](../../research/2026-08-28-permissions-derivation.md)
  — RBAC and cloud-IAM derivation: feasibility, exact mechanism, coverage, fidelity — verified against
  a live cluster.
- [`docs/research/2026-08-28-provider-discovery.md`](../../research/2026-08-28-provider-discovery.md)
  — catalogue channel comparison, static-index design, reference resolution, caching.

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
   the packages named in the blueprint fails to resolve a third of its edges. Load the family — cheaply:
   `GET api.upbound.io/v2/packageMetadata/{acct}/{repo}/{ver|latest}` returns `familyRepoKey` in one
   anonymous request, no `crossplane.yaml` parse.
2. **Pick the `storage: true` version, skip `deprecated: true`, never `versions[0]`.** 14 of 102
   legacy EC2 CRDs serve two versions with inconsistent storage flags.
3. **Digest-pin in a lockfile — resolve tag→digest once.** `:v2` is a moving tag; without pinning
   the same blueprint emits a different Composition next month. Reuse that digest for every later
   request on the pull, including the signature check — closes the TOCTOU gap in Crossplane advisory
   GHSA-wfqx-gjrf-g28r.

**Discovery is a first-class feature, not a naming exercise** — users browse and search providers;
they do not need to arrive already knowing an exact xpkg reference. But no channel permits anonymous
run-time enumeration of the provider *namespace*: `xpkg.upbound.io/v2/_catalog` and
`api.github.com/orgs/{org}/packages` both return **401** unauthenticated. So the catalogue is built
once daily by CI in a separate repo (`cf-index`), every entry validated against the registry before
publication so nothing unresolvable reaches a picker, and shipped two ways: `go:embed`ed as a seed
(offline on first run) and fetched from a mirrorable URL (**~40 KB gzipped** at the 900-package upper
bound). `cf` itself never calls the marketplace API or GitHub at request time — only the daily build
does. Two sources were ruled out as inputs to it: **GitHub is a wrong *version* source**, not merely
incomplete — `provider-upjet-aws`'s latest GitHub release is `v2.7.0` while every registry ships
`v2.7.1` (no Git tag, no GitHub release), and GitHub is blind to the ~350 family packages with no
repository at all (`upbound/provider-aws-sqs` → 404); **Artifact Hub indexes zero Crossplane
packages** (21,142 packages, 29 kinds, none of them ours) — settled, not reopened.

**Retraction:** `xpkg.upbound.io/v2/<repo>/tags/list` does not return an empty tag list. Unauthenticated
it is **401**; with a repository-scoped anonymous bearer minted at `https://xpkg.upbound.io/service/token`
(note `/service/token`, not `/token`, which 404s) it is **200 with 446 tags** for
`upbound/provider-aws-sqs` (344 cosign sidecars to filter, 102 real — tags sort lexicographically, so a
naive first page is 100% noise). Use `go-containerregistry`'s `remote.List`, never hand-rolled — it
unmarshals the 401 body into `struct{Tags []string}` and silently returns `nil`; treat `len(tags)==0`
as a bug to investigate, never as "no versions."

**Publisher tier is not a licence signal — label, don't hide.** OCI image labels carry no licence
data at all (zero `org.opencontainers.image.licenses` across every image inspected); the real signal
is `meta.crossplane.io/license` inside the in-band Crossplane package meta (9 of 12 packages probed),
GitHub's API as fallback. "Official" and "community" can be the same build wearing different badges:
at v2.4.0, `upbound/provider-aws-sqs` declares `license: Apache-2.0` and names
`crossplane-contrib/provider-upjet-aws` as its source; all 8 CRDs are byte-identical between the two,
and the entire delta is ~17 lines of annotations plus a `dependsOn` pin. Show `source:`, licence and
signature verdict as separate facts — never collapse them into one "official" badge implying
proprietary. `xpkg.crossplane.io` is itself a **partial mirror** of `ghcr.io` (authenticate against
whatever `WWW-Authenticate` names, never the hostname) and can carry fewer tags than the marketplace
(`provider-kubernetes`: 9 vs 27).

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

**A second schema source is a candidate, not yet trusted** —
`.../v1/packages/{acct}/{repo}/{ver}/resources/{group}/{kind}` returns a full CRD anonymously in
~20 KB, cheaper than a pull for one or two kinds, but whether it matches the xpkg-extracted CRD
byte-for-byte is untested (§14). Cache is content-addressed below the tag: an exact semver tag is
immutable and cached forever, a floating tag is revalidated hourly, and a failed refresh never
invalidates what is already cached.

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
nothing in the ecosystem derives them. Full analysis, datasets and licences in
[`docs/research/2026-08-28-permissions-derivation.md`](../../research/2026-08-28-permissions-derivation.md).
The two halves ship on different timelines because they earn very different confidence.

**Kubernetes RBAC — fully derivable offline, at exact fidelity, and no prior art competes.**
`emit: {rbac: true}` produces one aggregated `ClusterRole` per XRD, labelled
`rbac.crossplane.io/aggregate-to-crossplane: "true"` (exact string, quoted lowercase — aggregation
*is* the binding mechanism, no `RoleBinding` needed): one un-merged rule per canvas node,
comment-attributed (RBAC is an additive union with no deny, so N rules cost nothing and buy the
attribution `controller-gen` throws away), all seven verbs
(`get,list,watch,create,update,patch,delete`), never subresources on composed objects — the composite
controller only `Patch`es/`Get`s/`Delete`s composed resources, never `Update`s or touches
`/status`/`/finalizers` (that's the XR's, already covered by rbac-manager). Kind → resource-plural
resolves offline from vendored OpenAPI v3 **paths** (the `post` operation's
`x-kubernetes-group-version-kind` on the collection path) — **177/177** correct, zero guessing —
falling back to Kubernetes' own `UnsafeGuessKindToResource` for a bare Kind — **148/148**.

**The failure mode this replaces:** rbac-manager's static role *accidentally* authorizes a handful of
common kinds via unrelated package-management rules — **71% of a common-composable sample is denied**
the moment the canvas grows past those. The denial (`Forbidden` on `patch`, server-side apply)
degrades after one reconcile into a misleading informer timeout (open bug
crossplane/crossplane#7398) — surface that exact string in the GUI, mapped to "missing ClusterRole."
**The oracle no prior art has:** a read-only `SubjectAccessReview` turns each rule from *inferred* to
*verified* and yields `already-satisfied` for free when a kubeconfig is reachable — degrade to
`inferred`, and say so, when it is not.

**Cloud IAM — derivable only as an approximate, review-required draft, never a v1 feature.** The
chain is MR kind → Terraform resource (`zz_<kind>_terraformed.go`, exact) → CloudFormation type (the
lossy hop — Terraform and CFN carve resources up differently) → IAM actions (AWS's own CFN registry
schemas, mechanical). End to end over `provider-upjet-aws`: **53.2% automated coverage** with a real
per-resource action set and zero curation; where it completes, measured against real SDK call sites,
it **leaks both ways on every resource tested — recall 69–100%, precision 57–94%.** GCP: **40.6%**
via the naive convention join. A ~180-entry alias file is *estimated*, not measured, to lift AWS to
~70% — do not promise that number until it is.

**A wrong policy is worse than none**, so IAM output carries three visible tiers — `verified`
(AWS-authored or SAR-confirmed), `inferred` (heuristic, badged with the rule), `unknown` (an
actionable to-do list, never dropped or silently wildcarded) — plus a header comment that it is a
starting point for review, never an authority. Read (`Describe`/`Get`) actions are never trimmed even
at a precision cost: upjet re-reads on every reconcile, so a missing read permission is a silent,
continuous failure, not a loud one-time error.

**Neither artifact ships inside the Configuration package** — `crossplane xpkg build` hard-fails
parsing a `ClusterRole`, and no `permissionRequests` field exists on `Configuration` /
`ConfigurationRevision` / `ProviderRevision`. Both live as sibling files to commit and apply:
`permissions/rbac.yaml`, `permissions/iam-controlplane.json` (M6), `permissions/permissions.lock.json`
(provenance — comments and a lockfile, never annotations, §8), `permissions/overrides.yaml`
(hand-edited, never regenerated). **Control-plane vs workload is a second axis, and only the
control-plane half is a file:** workload IAM (what the *running app* needs, e.g. `sqs:SendMessage`)
is composition content — a composed `Policy`/`QueuePolicy` node on the canvas, attached to the
**edge** between two resources, AWS-SAM-connector style — not a side artifact. Deferred past M6.

**UNRESOLVED:** explicit seven verbs with no subresources (above), or mirror rbac-manager's XR
template (`["*"]` + `/status` + `update` on `/finalizers`)? The two source briefs disagree; decidable
only by applying the minimal role, composing a `Job` and an `Ingress`, and watching for
`RoleBasedAccessControl` warnings across reconcile cycles. Blocking for M5 (§14).

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

**CLI.** `cf provider search|list|versions|info|add|pin` ·
`cf index update|status|add|remove|list|export` · `cf k8s use <ver>` · `cf gen [--check]` ·
`cf validate` · `cf adopt <composition.yaml>` · `cf serve` · `cf mcp`. Global `--offline` /
`CF_OFFLINE=1` (a network attempt is an error, for CI reproducibility) and `CF_NO_AUTO_UPDATE=1` from
day one — Homebrew's blocking auto-update is the anti-pattern to avoid.

**Provider references.** Four forms, first match wins: a full OCI ref (needs no index);
`<index>/<publisher>/<name>`; `<publisher>/<name>` — the canonical short form, unambiguous by
construction; a bare name, resolved only if exactly one publisher ships it. `@` marks the version,
never `:`. **15% of short names are published by more than one account** with disagreeing signals
(downloads, signing, tier) — on ambiguity no default is applied: both options print side by side with
copy-pasteable fixes, and a `.cf/providers.yaml` pin (checked into git) is the only legitimate scope
for a default. `cf` always prints the full ref it resolved to; everything written to disk carries the
full ref **and** the digest, never the short name — a `.cf` directory reproduces byte-identically on
a machine with a different index, or none at all.

**GUI.** Node graph; wires are data dependencies compiled to template expressions. Four wire hues
chosen for **hue** separation (blue XRD 215°, teal status 173°, gold shared 51°, rust ref 15°), with
the ref wire dashed so the distinction never rests on hue alone. Required-first inspector with a
path-addressed tree — the mass of provider schemas sits at depth 3–5, so design for depth 5 and give
a raw-YAML escape below it. Do not build a virtualised form renderer: rjsf defaults arrays to zero
items, so Kyverno's 1,445-property `ClusterPolicy` renders in 19 ms producing 10 inputs.

The provider palette is **synchronous and local** (hundreds of records fuzzy-matched in
microseconds, no debounce, no spinner); a multi-publisher collision is one expandable row, never two
lookalikes; the staleness badge is always visible, never a modal; offline is a badge, not a blocker.
`api.upbound.io` sends **no CORS headers**, so `cf serve` proxies every discovery call — the same
"thin adapter over `internal/`" rule as everything else (§3), not an exception. Signature verdict and
vendor-tier claim must **never share a badge style** — one is cryptographic, the other marketing
(§4). Never hotlink a marketplace icon URL (a signed URL expiring in ~5 minutes; re-host at index
build time); HTML-escape every third-party description unconditionally.

A **Permissions** panel lists what the current canvas requires, attributed per node — Kubernetes
RBAC for native GVKs, cloud IAM for managed resources — so the answer to "why is nothing happening"
is visible before you apply rather than after.

**MCP.** Full authoring parity, writes confined to a declared workspace root, `--read-only` for
inspection. The context-window problem an agent has is the *same* problem the browser has — a 1.7 MB
CRD fits neither — so `schema_search` / `kind_describe(required_only)` / `kind_fields(path, depth)`
serve all three front doors, alongside `provider_search` / `provider_versions`, local-index-only, no
network from an MCP call ever.

## 12. Milestones

You asked for the full product and I am not scoping it down; it still has to be built in an order.
**The in-cluster web UI is out of v1** — it would drag in an OCI image, its own RBAC, and an auth
story, and `cf serve` on a laptop covers the authoring case.

| | Milestone | Proves |
|---|---|---|
| **M1** | xpkg ingest + index + lock; embedded seed provider index + `cf provider search`; `cf gen` single resource | Reproduces your `XQueue`, passes `crossplane composition render`, and `cf provider add aws-sqs` works with the network off |
| **M2** | XRD builder; `parameters:`; required-first schema API | The API shared by GUI, CLI and MCP |
| **M3** | Canvas + wires + reference inference (data-driven, golden-tested) | The central risk, retired early |
| **M4** | Pipelines, `when`, `forEach`, `dependsOn`, user-defined templates, other functions as nodes | Full T1 |
| **M5** | MCP server, `adopt`, `functions.yaml` + **K8s RBAC** emission (§9), `cf index export` air-gap bundle + in-process cosign verification, distribution | Ship |
| **M6** | Cloud IAM derivation, gated on all four of: the `zz_*_terraformed.go` extractor, a measured (not estimated) alias-file coverage number, the three-tier badge UI with a visible `unknown` to-do list, and a resolved CFN-dataset licence posture (§9) | Reviewed-starting-point policies |

Parallel, not gated on M1–M6: the `cf-index` build job (separate repo, daily CI, seven steps, §4),
whose own gate is that registry validation of every catalogue entry is non-optional before M1 trusts
the index.

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
6. **Discovery depends on an undocumented, single-consumer API.** `api.upbound.io/v2/search` has no
   published OpenAPI and one known caller — Upbound can change or gate it without notice, and
   `github.com/upbound/up` going 404 is a live precedent. *Mitigation:* only the daily `cf-index`
   build calls it, never `cf`; the embedded seed index and the GitHub+registry fallback (resolves
   49/60 repos plus every family member, no Upbound dependency) both work with zero marketplace
   access.
7. **The index is a mirror, not a live view** — freshness is a build-job SLO, not a client guarantee;
   a version can ship the morning after a build. *Mitigation:* age is shown and escalates by band
   (§4); `cf provider add` with no explicit version does a live `tags/list` rather than trust the
   index; a failed refresh never invalidates what is already cached.

## 14. Open questions

1. **Is a non-upjet provider the portability anchor, and when?** GCP does not validate portability —
   it shares the upjet generator with AWS. `provider-helm` / `provider-terraform` are the real test.
2. **Does `cf` ever read a cluster?** `generate` needs none. But a cluster is the only way to populate
   the palette from **Active** `ManagedResourceDefinition`s, to resolve `functionRef.name` against
   installed Functions, and to run the `SubjectAccessReview` that is the RBAC panel's strongest
   differentiator (§9). This decides whether `client-go` is core or an optional adapter — and whether
   the RBAC panel ships `inferred`-only when it is not.
3. **RBAC output shape — decidable only by cluster experiment.** Explicit seven verbs with no
   subresources, or mirror rbac-manager's XR template (`["*"]` + `/status` + `update` on
   `/finalizers`)? One rule per apiGroup, or one un-merged rule per node (proposed, needs a golden
   test proving byte-stability across regeneration and node reordering)? Both blocking for M5 (§9).
4. **What is the IAM alias table's real, measured coverage (the ~70% figure is estimated, not
   measured, and someone must own refreshing it per release), and does Magic Modules actually
   deliver for GCP** (the strongest untested lead — until measured, 40.6% stands)? **Both block M6
   scope-in** (§9, §12).
5. **Two integration facts are unverified before M5 ships:** whether
   `/v1/packages/{acct}/{repo}/{ver}/resources/{group}/{kind}` is byte-identical to an
   xpkg-extracted CRD, deciding a second schema source (§4); and whether `sigstore-go` verifies
   Upbound's cosign bundles end to end with no Rekor round-trip, blocking in-process signature
   verification (§12).

**Resolved since first draft:** `forEach` follows `range` semantics over array/map/count (§7);
user-defined template functions generalise conventions (§7); the in-cluster UI is out of v1 (§12);
Kubernetes RBAC is fully derivable offline at exact fidelity, Cloud IAM is not — 53.2% AWS / 40.6%
GCP automated coverage, gated into M6 behind four conditions (§9, §12); provider discovery has a
concrete design — static CI-built index, no live enumeration, GitHub and Artifact Hub ruled out as
catalogue sources (§4, §11, §12).
