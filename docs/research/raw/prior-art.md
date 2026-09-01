Research complete. Returning the brief.

# Prior Art & Competitive Landscape: Crossplane Composition/XRD Authoring

**Method note:** Findings marked **[VERIFIED]** were produced by running commands against the live `kind-platform` cluster and local `crossplane` CLI v2.5.0. Findings marked **[DOCS]** come from web sources only. Repo metrics pulled from the GitHub REST API on 2026-08-27.

---

## Decisions this enables

1. **Do not build schema fetching/extraction — shell out to `crossplane dependency add` + the CLI's schema generator.** It already pulls provider packages from OCI, extracts every CRD, and emits JSON Schema + Go + KCL + Python models, including the Crossplane v2 namespaced `.m.` API variants. **[VERIFIED]** 13.6s cold for provider-aws-sqs, 39 JSON Schema files produced. But it **strips all `x-kubernetes-*` extensions**, so read the *live CRD* (or the package's raw CRD YAML) when you need `x-kubernetes-validations`/`preserve-unknown-fields` — not the CLI's JSON output.

2. **The headline gap is real and large: nothing generates a multi-resource composition body.** `crossplane composition generate` emits a **12-line empty stub** with only `function-auto-ready` and zero managed resources; `crossplane function generate --language go-templating` emits **two files that are 100% comments**. **[VERIFIED]** The only tool that reads provider CRDs and writes actual resource bodies is `crossplane-contrib/x-generation` — and it is strictly **1:1 (one CRD → one XRD + one Composition)**, emits **patch-and-transform**, not go-templating, and is 14 months stale.

3. **Position against `up`, not with it — `up` is now closed source.** `github.com/upbound/up` returns **HTTP 404** while the `up` CLI still ships (v0.53.2, 2026-08-20). The open-source successor of that DevEx surface is the upstream `crossplane` CLI (Apache-2.0), which is exactly what produces empty stubs. An open-source canvas that fills in the body is uncontested ground.

4. **Target upjet provider managed resources first — they are far more form-friendly than general CRDs.** **[VERIFIED]** Across the cluster's 81 CRDs, 46 (57%) contain form-hostile constructs (705 `preserve-unknown-fields`, 354 `int-or-string`, 62 CEL `validations`). But the AWS/SQS upjet MRs are **clean**: no `preserve-unknown-fields`, no `int-or-string`, 11–19 KB each. Scoping v1 to provider MRs sidesteps the hardest schema problems entirely.

5. **RJSF is usable but only with AJV in non-strict mode, and needs custom widgets.** **[VERIFIED]** AJV strict mode **hard-fails** on a real Crossplane CRD: `strict mode: unknown keyword: "x-kubernetes-map-type"`. Non-strict compiles fine and fast (10 ms for 17.6 KB, 111 ms for 320 KB), silently ignoring `int64`/`int32`/`date-time` formats. Budget for: an `x-kubernetes-preserve-unknown-fields` free-form editor (RJSF issue #3824 — no built-in field accepts arbitrary objects), an `int-or-string` widget, and int64 range validation you write yourself.

---

## 1. The closest prior art: `crossplane` CLI v2.5.0 DevEx (formerly `up`)

This is the single most important comparison, and it is **much weaker than its marketing implies.**

### 1.1 What I ran, and exactly what came out **[VERIFIED]**

```
crossplane project init pa -d <dir>
```
Creates `crossplane-project.yaml` (`apiVersion: dev.crossplane.io/v1alpha1`, `kind: Project`) plus empty dirs: `apis/ examples/ functions/ operations/ tests/`.

```
crossplane composition generate apis/xqueue/definition.yaml
```
Output — **this is the entire file**:

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.platform.sparky.ee
spec:
  compositeTypeRef:
    apiVersion: platform.sparky.ee/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
  - functionRef:
      name: crossplane-contrib-function-auto-ready
    step: crossplane-contrib-function-auto-ready
```

It **never reads the XRD's schema fields**. The source XRD declared `location` (enum EU/US), `maxMessageSize` (minimum 1024), `providerName`, `tags`, `visibilityTimeoutSeconds` — none of it influences the output. It does not know what a provider is, and it has no concept of a managed resource. It also mutates `crossplane-project.yaml` to add a `function-auto-ready` dependency at `version: '>=v0.0.0'`.

*(`apiextensions.crossplane.io/v1` is correct here, not a bug — **[VERIFIED]** `kubectl api-resources` shows `compositions` is served only at v1, while `compositeresourcedefinitions` serves v1 + v2 with v1 as storage.)*

```
crossplane function generate compose-queue apis/xqueues/composition.yaml --language go-templating
```
Produces exactly two files, both entirely comments:

- `functions/compose-queue/00-prelude.yaml.gotmpl` — one line: `{{ $xr := getCompositeResource . }}`
- `functions/compose-queue/01-compose.yaml.gotmpl` — a header (`# code: language=yaml`, `# yaml-language-server: $schema=../../schemas/json/index.schema.json`) then a **commented-out** `nop.crossplane.io/v1alpha1 NopResource` example. Zero executable template content.

It does correctly insert the pipeline step *before* `function-auto-ready`, and supports `--language` in `go-templating` (default) | `go` | `kcl` | `python`.

> **Contradiction flagged:** Upbound's docs page for go-templating implies the generator emits a "basic template structure with placeholder managed resources" showing an `s3.aws.upbound.io/v1beta1 Bucket`. **[VERIFIED]** against crossplane CLI v2.5.0, that is *not* what is generated — the S3 example in the docs is illustrating what *you* write. Treat any claim that these generators produce working resource bodies as false.

### 1.2 The schema pipeline — the genuinely reusable part **[VERIFIED]**

```
crossplane dependency add xpkg.upbound.io/upbound/provider-aws-sqs:v2   # 13.6s
crossplane function generate probe --language go-templating              # triggers "Generating schemas..."
```

Produced **39** files in `schemas/json/`, plus parallel `schemas/go/`, `schemas/kcl/`, `schemas/python/` trees and a `schemas/.lock.json`. Naming is reverse-DNS: `io-upbound-aws-sqs-v1beta1-Queue.schema.json` (cluster-scoped) and **`io-upbound-m-aws-sqs-v1beta1-Queue.schema.json`** (the v2 namespaced `.m.` variant) — both are emitted, along with `io-upbound-aws-v1beta1-ProviderConfig.schema.json`. An `index.schema.json` `anyOf`-refs everything for `yaml-language-server`.

Caches live at `~/.crossplane/cache/xpkg.upbound.io/` and `~/Library/Caches/crossplane/xpkg/` (override via `--cache-dir` / `CROSSPLANE_XPKG_CACHE`).

**Critical limitation:** `grep -o 'x-kubernetes-[a-z-]*'` on the generated `Queue.schema.json` returns **nothing** — every `x-kubernetes-*` extension is stripped. The live CRD has 4 `x-kubernetes-map-type`. Generated schema is 17,829 bytes vs 17,652 for the live CRD schema.

### 1.3 `crossplane xrd generate` is lossy **[VERIFIED]**

Round-tripping an XR through `xrd generate` against the real XRD lost: the `location` **enum**, the `maxMessageSize` **minimum**, and `tags`' **`additionalProperties: {type: string}`** (it inferred a literal `env: string` property from the example's data). It also emitted **`scope: Cluster`** where the live XRD is `scope: Namespaced`. It is example-inference, not schema derivation.

Also **[VERIFIED]** a path bug: `--path apis/fromxr/definition.yaml` wrote to `apis/apis/fromxr/definition.yaml` (the flag is re-rooted under the project's APIs dir).

### 1.4 Upbound's proprietary layer

| Thing | Status | Note |
|---|---|---|
| `github.com/upbound/up` | **HTTP 404** | Source closed/removed. **[VERIFIED]** |
| `up` CLI releases | **v0.53.2, 2026-08-20** | Still actively shipping. No deprecation notice in release notes. **[DOCS]** |
| `open-crossplane/up` | 0 stars, pushed **2023-05-12** | Stale fork of the old Apache-2.0 tree. Not a viable base. **[VERIFIED]** |
| `upbound/vscode-up` | 16 stars, Apache-2.0, pushed **2024-10-10** | Extension v0.0.6, 4,530 installs, last updated Sept 2024. xpls-backed **YAML diagnostics only** — validation of `crossplane.yaml` deps, XRC/composed-resource/patch/XRD schema. **No visual authoring.** **[VERIFIED metrics / DOCS features]** |
| Upbound Console | Proprietary SaaS | Graph/list views, control-plane CRUD, event logs, composition-pipeline *inspection*. **No drag-and-drop or form-based Composition/XRD authoring.** **[DOCS]** |

**Gap in one sentence:** the entire Upbound/Crossplane DevEx stack gives you an empty pipeline skeleton plus IDE autocomplete, and then expects a human to hand-write every managed resource body in YAML.

---

## 2. Composition generators that read provider CRDs

| Repo | Stars | License | Last push | Verdict |
|---|---|---|---|---|
| [crossplane-contrib/x-generation](https://github.com/crossplane-contrib/x-generation) | 46 | Apache-2.0 | **2025-07-01** | The real competitor — and it's 1:1 only |
| [benagricola/crossplane-composition-generator](https://github.com/benagricola/crossplane-composition-generator) | 4 | MIT | **2023-02-17** | Dead |
| [moneyforward/crossplane-poc-x-generation](https://github.com/moneyforward/crossplane-poc-x-generation) | 0 | Apache-2.0 | 2024-10-28 | **ARCHIVED** |
| [crossplane-contrib/crossplane-cdk8s](https://github.com/crossplane-contrib/crossplane-cdk8s) | 49 | Apache-2.0 | **2023-01-05** | Dead (3.5 yrs) |

**`x-generation`** is the one to study. Config-driven Go CLI (run via `make`): a global `generator-config.yaml` (provider baseURL/name/version, default labels/tags) plus a per-composition `generate.yaml` (`group`, `name`, `version`, `provider.crd.file`, `labels`, `tags`, `overrideFieldsInClaim`). It fetches the provider CRD from a templated URL and emits both an XRD and a Composition. `usePipeline: true` switches to `mode: Pipeline` but still uses **function-patch-and-transform** (overridable via `patchAndTransfromFunction`) plus `autoReadyFunction`. **[DOCS]**

**Why it does not solve the problem:** `provider.crd.file` is singular — each `generate.yaml` wraps **exactly one** managed resource, so it produces thin 1:1 passthrough APIs, never a composed multi-resource abstraction; it targets patch-and-transform rather than go-templating; and it's a YAML-config CLI with no UI. 14 months without a commit.

**`crossplane-cdk8s`** generates XRDs + Compositions from TypeScript/Python/Java via cdk8s — the right *idea* (code → composition), but abandoned since Jan 2023, so it predates composition functions entirely and emits legacy patch-and-transform.

**Upstream has explicitly declined this space:** [crossplane/crossplane#4989 "XRD and Claim Generation Tools"](https://github.com/crossplane/crossplane/issues/4989) was **closed as not planned**. **[DOCS]** That's an opening, not a warning — but it does mean no upstream blessing.

---

## 3. crossplane-contrib functions (these are the *output targets*, not competitors)

**[VERIFIED]** metrics, 2026-08-27:

| Repo | Stars | License | Latest release | Last push |
|---|---|---|---|---|
| function-go-templating | 99 | Apache-2.0 | **v0.12.4** (2026-08-25) | 2026-08-27 |
| function-kcl | 87 | Apache-2.0 | v0.12.2 (2026-07-19) | 2026-08-26 |
| function-patch-and-transform | 44 | Apache-2.0 | v0.10.10 (2026-08-25) | 2026-08-26 |
| function-sequencer | 37 | Apache-2.0 | v0.6.0 (2026-06-23) | 2026-08-27 |
| function-auto-ready | 35 | Apache-2.0 | v0.7.0 (2026-06-24) | 2026-08-26 |
| function-extra-resources | 33 | Apache-2.0 | v0.3.0 (2026-01-10) | 2026-08-27 |
| function-environment-configs | 27 | Apache-2.0 | v0.7.4 (2026-08-25) | 2026-08-25 |
| **function-cue** | 25 | Apache-2.0 | **no releases** | **2026-01-08** |
| function-python | 20 | Apache-2.0 | v0.5.0 (2026-06-23) | 2026-08-26 |

Notes: all Apache-2.0, all actively maintained except **function-cue** (no GitHub release at all, 7 months stale — do not target it). The whole function ecosystem is small (20–99 stars); **function-go-templating at 99 is the most-starred**, which validates it as the default output format. `function-go-templating` v0.12.4 is available but **[VERIFIED]** the cluster runs v0.12.0.

Cluster state **[VERIFIED]**: `function-go-templating:v0.12.0` and `function-auto-ready:v0.5.0`, both `Healthy=True`; providers `provider-aws-sqs:v2` and `upbound-provider-family-aws:v2.4.0`.

**None of these are competitors** — they are runtimes that execute what the new tool would write. The gap: every one of them assumes a human already wrote the template/KCL/CUE/Python.

---

## 4. Visualizers and dashboards — all read-only

| Repo | Stars | License | Last push | What it is |
|---|---|---|---|---|
| [komodorio/komoplane](https://github.com/komodorio/komoplane) | 386 | Apache-2.0 | 2026-08-25 | Crossplane resource graph + troubleshooting UI, port 8090 |
| [crossplane-contrib/crossview](https://github.com/crossplane-contrib/crossview) | 265 | Apache-2.0 | 2026-06-29 | React/Chakra + Go/Gin dashboard; multi-cluster, OIDC/SAML, informer+WebSocket live view |
| [upbound/xgql](https://github.com/upbound/xgql) | 47 | Apache-2.0 | 2026-01-27 | GraphQL API over Crossplane. Latest release **v0.1.5, 2021-11-19** |

**[VERIFIED]** `crossplane-contrib/xgql` is a 404 — it moved to `upbound/xgql`. Its last release is nearly 5 years old; effectively dead as a product, though the GraphQL-over-Crossplane *schema design* is worth reading.

**Why none solve it:** all three render Crossplane objects that **already exist in a cluster**. None accept authoring input, none write YAML, none touch provider CRD schemas for composition purposes. Komoplane and crossview are the "after" picture; the new tool is the "before".

---

## 5. Adjacent platform-abstraction tools

| Repo | Stars | License | Last push | Relation |
|---|---|---|---|---|
| [kubevela/kubevela](https://github.com/kubevela/kubevela) | 7,888 | Apache-2.0 | 2026-08-27 | CNCF **Incubating** (since 2023-02-27). OAM app model + CUE-based ComponentDefinitions |
| [radius-project/radius](https://github.com/radius-project/radius) | 1,665 | Apache-2.0 | 2026-08-27 | App-graph platform w/ Bicep-based recipes |
| [syntasso/kratix](https://github.com/syntasso/kratix) | 770 | Apache-2.0 | 2026-08-27 | Promises = API + workflow pipelines across clusters |
| [score-spec/spec](https://github.com/score-spec/spec) | 8,088 | Apache-2.0 | 2026-07-27 | Workload spec only; `score-compose` (457), `score-k8s` (48), `score-helm` (365) |

All four are **replacements for the Crossplane abstraction layer, not authoring tools for it.** They ask you to adopt their own model (OAM ComponentDefinition, Radius recipe, Kratix Promise, Score workload) instead of writing an XRD + Composition. Notably Kratix ships `kratix init crossplane-promise`, i.e. it *wraps* Crossplane rather than helping you author it. **[DOCS]** None generate a go-templating Composition. `score-spec/score-humanitec` and `score-helm-charts` are **archived**.

---

## 6. Visual / drag-and-drop Kubernetes editors

| Repo | Stars | License | Last push | Verdict |
|---|---|---|---|---|
| [cyclops-ui/cyclops](https://github.com/cyclops-ui/cyclops) | 3,321 | Apache-2.0 | 2026-01-22 | Forms from **Helm `values.schema.json`**. No Crossplane, no canvas, no CRD-schema path |
| [kubernetes-sigs/headlamp](https://github.com/kubernetes-sigs/headlamp) | 7,160 | Apache-2.0 | 2026-08-27 | Extensible K8s UI; **most relevant plugin host** (see below) |
| [kubeshop/monokle](https://github.com/kubeshop/monokle) | 2,140 | MIT | **2026-02-26** | YAML/manifest validation IDE. 409 open issues, 6 months stale |
| [kubevious/kubevious](https://github.com/kubevious/kubevious) | 1,706 | Apache-2.0 | **2026-06-13** | Read-only config graph + rule engine |
| [same7ammar/kube-composer](https://github.com/same7ammar/kube-composer) | 480 | **NONE** | **2025-08-16** | Drag-drop builder for **core workloads only**; **no license** ⇒ unusable as a base |
| [weaveworks/weave-gitops](https://github.com/weaveworks/weave-gitops) | 1,128 | Apache-2.0 | 2026-08-27 | Flux GitOps UI. Weaveworks the company shut down in 2024 |
| [datreeio/datree](https://github.com/datreeio/datree) | 6,333 | Apache-2.0 | 2024-04-23 | **ARCHIVED** — policy linting only |
| [kalmhq/kalm](https://github.com/kalmhq/kalm) | 431 | Apache-2.0 | **2022-05-13** | **Dead 4 years** (docs still say "Closed Beta" — ignore them) |
| ConfigHub | — | Proprietary SaaS | — | Config-as-data plane (kpt/Porch lineage), self-hosted since 2026-04-17. Not a Crossplane authoring UI **[DOCS]** |

**The one to study closely:** [orange-cloudfoundry/Headlamp-plugin](https://github.com/orange-cloudfoundry/Headlamp-plugin) — a Headlamp plugin that renders **CRD-driven forms using `@rjsf/core`** (form components under `src/Components/Form/`), explicitly framed as a Crossplane marketplace/consumption UI. **[DOCS]** Upstream Headlamp is tracking the same idea in [kubernetes-sigs/headlamp#2087 "Generate Forms from Kubernetes Schema to Create and Edit Resources"](https://github.com/kubernetes-sigs/headlamp/issues/2087).

**Why it still doesn't solve it:** it generates a form to **instantiate one existing CR** (i.e. fill in a claim/XR), which is the *consumer* side. The new tool's job is the *producer* side — emitting a Composition + XRD that don't exist yet. Nobody is doing producer-side.

---

## 7. Schema-driven form generators — fitness for CRD schemas

**[VERIFIED]** metrics:

| Library | Stars | License | Last push | Fit |
|---|---|---|---|---|
| [rjsf-team/react-jsonschema-form](https://github.com/rjsf-team/react-jsonschema-form) | 15,877 | Apache-2.0 | 2026-08-27 | Best bet; needs custom widgets |
| [eclipsesource/jsonforms](https://github.com/eclipsesource/jsonforms) | 2,736 | MIT | 2026-08-27 | Needs hand-authored UISchema → poor for 1000s of CRDs |
| [vazco/uniforms](https://github.com/vazco/uniforms) | 2,104 | MIT | **2026-01-12** | 7 months stale |
| [tommy351/kubernetes-models-ts](https://github.com/tommy351/kubernetes-models-ts) | 163 | MIT | 2026-08-12 | Ships `@kubernetes-models/crd-generate` — CRD → TS types |

### The empirical test **[VERIFIED]**

I compiled two real CRD schemas with AJV 8 (the validator RJSF and jsonforms both depend on):

```
sqs-queue.json (17,631 B)  strict=true : FAILED -> strict mode: unknown keyword: "x-kubernetes-map-type"
sqs-queue.json            strict=false: COMPILED ok in 10ms
big.json (320,182 B)      strict=true : FAILED -> strict mode: unknown keyword: "x-kubernetes-map-type"
big.json                  strict=false: COMPILED ok in 111ms
```

Plus silent degradation in both modes:
```
unknown format "date-time" ignored ... status.conditions.items.lastTransitionTime
unknown format "int64"     ignored ... status.observedGeneration
unknown format "int32"     ignored ... spec.deploymentTemplate.spec.replicas
```

**Conclusions:** performance is a non-issue (111 ms for a 320 KB schema). Strict mode is a **hard blocker** and must be disabled. `int64`/`int32` get no range validation unless you add custom formats — real risk for fields like `maxMessageSize`.

### What's actually in the schemas **[VERIFIED]** (all 81 CRDs, 110 versions)

```
x-kubernetes-* extension counts:        schema features:        schema sizes (bytes):
  2562  list-type                         5747  items             max    376,061
   933  map-type                          1277  format            p90     81,264
   705  preserve-unknown-fields           1266  additionalProps   median  17,819
   354  int-or-string                      765  default           min      1,264
   274  list-map-keys                      639  enum
    62  validations (CEL)                  354  anyOf           max nesting depth: 34
     5  embedded-resource                  107  oneOf
```
**46 of 81 CRDs (57%)** contain at least one of `preserve-unknown-fields` / `int-or-string` / CEL `validations`.

### The decisive nuance **[VERIFIED]**

Restricting to the **upjet-generated AWS provider MRs** — the actual drag-onto-canvas targets — the picture inverts:

```
queues.sqs.aws.m.upbound.io                    Namespaced   17652b  hostile=NONE
queues.sqs.aws.upbound.io                      Cluster      19337b  hostile=NONE
queuepolicies.sqs.aws.m.upbound.io             Namespaced   11801b  hostile=['validations']
queueredrivepolicies.sqs.aws.m.upbound.io      Namespaced   12040b  hostile=['validations']
providerconfigs.aws.m.upbound.io               Namespaced   13745b  hostile=NONE
```
No `preserve-unknown-fields`, no `int-or-string`, modest size. Because upjet derives them from Terraform schemas, they are flat, typed, and regular. The hostile constructs concentrate in hand-written platform CRDs (ArgoCD, Kyverno, cert-manager, Crossplane's own `DeploymentRuntimeConfig`) that a composition author rarely composes.

**Recommendation:** RJSF with `ajv strict:false`, plus three custom widgets (`preserve-unknown-fields` → JSON/YAML editor per [RJSF #3824](https://github.com/rjsf-team/react-jsonschema-form/issues/3824), `int-or-string` → dual-mode input, int64 → bigint-safe number) covers the target domain. jsonforms is the wrong shape — its UISchema-per-form model can't scale to thousands of auto-discovered CRDs. uniforms is stale.

### Adjacent CRD-codegen (schema-extraction reference implementations)
[pulumi/crd2pulumi](https://github.com/pulumi/crd2pulumi), [IvanJosipovic/KubernetesCRDModelGen](https://github.com/IvanJosipovic/KubernetesCRDModelGen) (.NET), [yaacov/crdtoapi](https://github.com/yaacov/crdtoapi) (CRD → OpenAPI), `@kubernetes-models/crd-generate` (CRD → TS). All are **type generators, not UI or composition generators** — useful as references for CRD-schema normalization edge cases.

---

## 8. One-line "why it doesn't already solve this"

| Project | Why not |
|---|---|
| `crossplane composition generate` | Emits a 12-line stub with zero managed resources; never reads the XRD schema. **[VERIFIED]** |
| `crossplane function generate --language go-templating` | Emits two files that are entirely comments. **[VERIFIED]** |
| `crossplane xrd generate` | Infers a lossy XRD from one example XR — drops enum/minimum/additionalProperties, wrong scope. **[VERIFIED]** |
| `up` CLI / Upbound DevEx | Same generators, plus proprietary: `upbound/up` source is now 404. **[VERIFIED]** |
| Upbound Console | Observability and control-plane management; no authoring canvas. **[DOCS]** |
| `upbound/vscode-up` | YAML diagnostics via xpls; v0.0.6, untouched since Sept 2024; no visual authoring. |
| x-generation | 1:1 CRD wrapper, patch-and-transform not go-templating, YAML-config CLI, 14 months stale. |
| crossplane-cdk8s | Right idea (code → XRD+Composition), dead since Jan 2023, pre-functions. |
| komoplane / crossview / xgql | Read-only views of resources that already exist. |
| function-* (all 9) | Runtimes that execute templates a human already wrote. |
| KubeVela / Radius / Kratix / Score | Competing abstraction models that replace XRDs rather than author them. |
| Cyclops | Forms from Helm `values.schema.json`; no Crossplane, no CRD path, no canvas. |
| Headlamp + orange-cloudfoundry plugin | RJSF forms to *instantiate* one CR (consumer side), not to *emit* a Composition (producer side). |
| kube-composer | Core workloads only, **no LICENSE file**, stale a year. |
| Monokle / Kubevious / Weave GitOps | Validation/lint/GitOps views; no generation. |
| Datree | **Archived** 2024. |
| Kalm | **Dead** since May 2022 despite live-looking docs. **[VERIFIED]** |
| RJSF / jsonforms / uniforms | Form libraries — an input, not a solution; none know Crossplane. |

---

## 9. Bonus finding worth designing around **[VERIFIED]**

Crossplane v2 gates which managed resources exist via `ManagedResourceDefinition` (`mrd`, `apiextensions.crossplane.io/v1alpha1`) + `ManagedResourceActivationPolicy` (`mrap`):

```
$ kubectl get mrd
NAME                                  STATE    ESTABLISHED
queues.sqs.aws.m.upbound.io           Active   True
queues.sqs.aws.upbound.io             Active   True
...  (8 total, all Active)

$ kubectl get mrap default -o yaml
spec:
  activate: ['*']
status:
  activated: [queues.sqs.aws.m.upbound.io, ...]
```

A canvas must populate its palette from **`mrd` with `state: Active`** (or the package's CRDs when working offline), not blindly from `kubectl get crd`. With large providers where most MRs ship **Inactive** by default, a naive CRD listing shows resources the cluster will not accept. Nothing surveyed above handles this — it's new enough that all the prior art predates it.

Also note every upjet MR exists in **two variants**: cluster-scoped `queues.sqs.aws.upbound.io` and namespaced `queues.sqs.aws.m.upbound.io`. The user's existing Composition targets the `.m.` (namespaced) one with a `ClusterProviderConfig` ref — the tool must pick the variant matching the XRD's `scope`, and the CLI's schema generator helpfully emits both.

---

## 10. Could not confirm

- **Upbound Console's authoring UI** — assessed from public docs only; no Upbound account available. If it has gained a composition builder behind login, that changes the positioning materially. Worth a manual check.
- **`up` CLI internals** — source is 404, so I could not verify whether `up composition generate` differs from `crossplane composition generate`. Given the shared lineage and identical docs, I assess them as equivalent, but this is inference, not verification.
- **A newer Upbound VSCode extension** — Upbound blog posts announce go-templating IDE integration, but the only marketplace listing I could resolve is `Upboundio.upbound` v0.0.6 (Sept 2024). There may be a second, newer extension ID I did not find.
- **`x-generation` current behavior** — assessed from README only; not run. Its claimed pipeline mode may have gained go-templating support in an unreleased commit.
- **Large-provider CRD counts** (e.g. provider-aws-ec2 with 100+ MRs and much larger schemas) — only provider-aws-sqs is installed on this cluster, so my "upjet MRs are form-friendly" conclusion is verified on 8 CRDs and should be re-tested against a big provider before it's load-bearing.
- **`gh` CLI** hung on the macOS keyring; all GitHub metrics came from the unauthenticated REST API instead (rate-limited, so a few repos were assessed via WebFetch rather than the API).