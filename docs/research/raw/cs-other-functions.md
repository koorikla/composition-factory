# What other composition functions express — the DSL feature ceiling

Survey area: function-patch-and-transform's complete vocabulary, KCL/CUE, extra/required
resources, the long tail of functions, and multi-step pipeline reality.
Method: local corpus of **1,409 YAML files** = 600 harvested real GitHub composition files
(356 of them contain Compositions; 364 Composition docs; ~250 distinct repos) **plus 13
cloned repos** (awslabs/crossplane-on-eks 58 compositions, crossplane-contrib/function-kcl
35, upbound platform-ref-{aws,gcp,azure}, upbound configuration-*, vshn/appcat,
Azure-Samples/aks-platform-engineering, aws-samples/psp-reusable-artifacts, the function
repos themselves). Structural counts come from `yaml.safe_load_all` parsing, not grep.
Breadth counts come from authenticated GitHub code search (file counts, Aug 2026).

**Corpus bias warning:** the 600-file harvest was collected with a go-templating bias
(99.2% of its compositions use function-go-templating). Use GitHub code search, not the
corpus, for *relative popularity between functions*; use the corpus for *what people write
inside* a function.

---

## What this means for the DSL — 5 bullets

1. **P&T's real vocabulary is tiny. Model 6 things, not 40.** Across 1,899 parsed patches
   and 426 transforms in 73 P&T compositions: `FromCompositeFieldPath` 61%, `PatchSet` 18%,
   `ToCompositeFieldPath` 13%, `CombineFromComposite` 7% — that's **99.1% of all patches**.
   Transforms: `string` 89% (and 95% of those are plain `fmt` Format), `map` 9%, `convert`
   1.4%. **`match` = 0 occurrences. `math` = 0 occurrences. `Join`/`Replace`/`TrimSuffix`
   = 0. `ForceMergeObjects*` = 0. All four `*Environment` combine patch types = 0.** Ship
   fromField→toField, fmt-combine, dict map, convert, and TrimPrefix/Regexp as first-class;
   everything else is rawTemplate.
2. **"Read something outside the composition" is a real, distinct node type — and it is
   about to change shape.** `extra-resources.fn.crossplane.io` (79 files) + go-templating's
   inline `meta.gotemplating.fn.crossplane.io/v1alpha1 ExtraResources` (35 files, 19 in
   corpus) are the v1 way. Crossplane **v2.4 (the user's server) replaces it with
   `spec.pipeline[].requirements.requiredResources`** — a *step-level* field, zero
   occurrences in the corpus because it is new. compositionfactory should emit the v2 form
   and model it as a **step property, not a resource node**.
3. **The canvas must model steps as first-class, because 34% of real compositions have ≥3
   steps and the dominant authoring unit is one go-templating step per composed resource**
   (53.5% of 486 inline go-templating steps emit exactly one named resource; only 12% emit
   6+). A "one big template" generator would produce something that does not look like what
   people write or diff.
4. **Steps talk to each other over exactly three channels, and the DSL must name all
   three:** (a) `.observed.resources.<name>` — 422 occurrences/121 files, the overwhelming
   favourite; (b) pipeline **context** (`index .context "..."`, 130/75) written by
   `meta.gotemplating .../Context` or function-environment-configs; (c) the **XR status as
   scratchpad** (`desired.composite.resource.status.atFunction.*`, upbound/function-cidr).
   `.desired.resources` cross-step reads are essentially nonexistent (1 file of 1,409).
5. **Two functions earn first-class canvas nodes; the rest are a generic "function step"
   node with a YAML blob.** Worth modelling: **function-environment-configs** (163 files,
   18% of corpus compositions — an "EnvironmentConfig lookup" node) and **required/extra
   resources** (an "external lookup" node). **function-sequencer** (62 files) deserves an
   *edge type* (dependency arrows) rather than a node. Everything else — cel-filter (19),
   status-transformer (14), conditional-P&T (14), shell (9), python (41), cue (10) — is
   long-tail: a generic step node with raw input is correct.

---

## 1. function-patch-and-transform: the complete vocabulary, and what is actually used

Source of truth read directly:
`https://github.com/crossplane-contrib/function-patch-and-transform/blob/main/input/v1beta1/resources_patches.go`
and `.../resources_transforms.go` and `.../resources_common.go`.

### 1.1 Complete patch-type enum (9 types) with real distribution

| Patch type | Declared where | Occurrences in corpus | % of 1,899 patches |
|---|---|---|---|
| `FromCompositeFieldPath` (default) | ComposedPatch, PatchSetPatch, EnvironmentPatch, ConnectionSecretPatch | **1,163** | 61.2% |
| `PatchSet` | ComposedPatch only | **339** | 17.9% |
| `ToCompositeFieldPath` | ComposedPatch, PatchSetPatch, EnvironmentPatch | **247** | 13.0% |
| `CombineFromComposite` | all four | **133** | 7.0% |
| `FromEnvironmentFieldPath` | ComposedPatch, PatchSetPatch, EnvironmentPatch | **15** | 0.8% |
| `CombineToComposite` | ComposedPatch, PatchSetPatch, EnvironmentPatch | **2** | 0.1% |
| `ToEnvironmentFieldPath` | ComposedPatch, PatchSetPatch, EnvironmentPatch | **0** | 0% |
| `CombineFromEnvironment` | ComposedPatch, PatchSetPatch | **0** | 0% |
| `CombineToEnvironment` | ComposedPatch, PatchSetPatch | **0** | 0% |

GitHub code-search cross-check (file counts): `"type: PatchSet"` 608,
`"type: ToCompositeFieldPath"` 1,130, `"type: CombineFromComposite"` 510.

`spec.environment.patches` (P&T's own alpha environment block): **0 of 72 P&T steps** used
it. Everyone uses function-environment-configs + context instead. Do not model it.

Real `PatchSet` + `CombineFromComposite` (the two non-obvious ones), from
`https://github.com/awslabs/crossplane-on-eks/blob/main/compositions/aws-provider/s3/multi-tenant.yaml`:

```yaml
  patchSets:
    - name: common-fields
      patches:
        - type: FromCompositeFieldPath
          fromFieldPath: spec.resourceConfig.deletionPolicy
          toFieldPath: spec.deletionPolicy
        - type: FromCompositeFieldPath
          fromFieldPath: spec.resourceConfig.name
          toFieldPath: metadata.annotations[crossplane.io/external-name]
  resources:
    - name: s3-bucket
      patches:
        - type: CombineFromComposite
          policy:
            fromFieldPath: Required
          fromFieldPath: metadata.labels[crossplane.io/claim-namespace]
          toFieldPath: spec.providerConfigRef.name
          combine:
            variables:
              - fromFieldPath: metadata.labels[crossplane.io/claim-namespace]
            strategy: string
            string:
              fmt: "%s-provider-config"
```

Note the shape: `combine.strategy` has **exactly one legal value, `string`** (`CombineStrategy`
enum has one member), and `combine.string.fmt` is a Go `fmt` format string. `CombineFromComposite`
is therefore not a general "combine" — it is *sprintf over N field paths*. 108 `combine:` blocks
in the corpus, all 108 with `strategy: string`.

*Graph GUI:* structural. A combine patch is an N-input → 1-output node whose only config is a
format string. Draw N edges into one "format" node.

### 1.2 Complete transform vocabulary (5 types, ~25 sub-variants) with real distribution

426 transforms parsed. **`match` and `math` never appeared, in any of 1,409 files.**

| Transform | Sub-variant | Count | Note |
|---|---|---|---|
| `string` (381, 89.4%) | `Format` (`fmt:`) | **361** | 84.7% of ALL transforms |
| | `TrimPrefix` | 8 | |
| | `Regexp` (`match:`, `group:`) | 7 | |
| | `Convert` → `ToBase64` | 4 | |
| | `Convert` → `ToUpper` | 1 | |
| | `TrimSuffix`, `Join`, `Replace` | **0** | declared in API, unused |
| | `Convert` → `ToLower`/`FromBase64`/`ToJson`/`ToSha1`/`ToSha256`/`ToSha512`/`ToAdler32` | **0** | |
| `map` (39, 9.2%) | flat `key: value` pairs | 39 | |
| `convert` (6, 1.4%) | `toType: string` | 5 | |
| | `toType: bool` | 1 | |
| | `toType: int/int64/float64/object/array` | **0** | |
| | `format: quantity` / `format: json` | **0** | |
| `match` | `patterns[].type: literal` / `regexp`, `fallbackValue`, `fallbackTo: Value\|Input` | **0** | |
| `math` | `Multiply` / `ClampMin` / `ClampMax` | **0** | |

Canonical `map` transform, from the P&T repo's own example
(`https://github.com/crossplane-contrib/function-patch-and-transform/blob/main/example/multistep/composition.yaml`):

```yaml
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: "spec.location"
                toFieldPath: "spec.forProvider.region"
                transforms:
                  - type: map
                    map:
                      EU: "eu-north-1"
                      US: "us-east-2"
```

This is exactly the user's existing "dict-based value mapping" go-template idiom. Same feature,
two syntaxes.

### 1.3 Policies

`PatchPolicy.fromFieldPath`: `Optional` (default) | `Required`.
`PatchPolicy.toFieldPath`: `Replace` (default) | `MergeObjects` | `MergeObjectsAppendArrays` |
`ForceMergeObjects` | `ForceMergeObjectsAppendArrays` | deprecated `MergeObject` | deprecated
`AppendArray`.

Real counts: `fromFieldPath: Optional` **186** (explicit), `fromFieldPath: Required` **111**,
`toFieldPath: MergeObject` (deprecated spelling) **8**, `toFieldPath: MergeObjects` **3**,
everything else **0**. GitHub-wide `MergeObjectsAppendArrays` = 22 files.

**`policy.fromFieldPath: Required` is the single most semantically important P&T feature we
must not lose**: it means "do not create this composed resource until the source field exists."
In go-templating the equivalent is `{{- with ... }}` around the whole resource doc — which the
user already uses. That equivalence is the key mapping.

### 1.4 readinessChecks and connectionDetails (ComposedTemplate-level)

`ReadinessCheckType` enum: `MatchCondition` (schema default `{type: Ready, status: True}`),
`MatchString`, `MatchInteger`, `MatchTrue`, `MatchFalse`, `NonEmpty`, `None`.
Real counts: **`None` 56**, `MatchString` 16, `NonEmpty` 2, everything else 0 (MatchCondition
is the implicit default so it is rarely written; GitHub-wide `"type: MatchCondition"` +
`"readinessChecks"` = 187 files).

`ConnectionDetailType` enum: `FromConnectionSecretKey` (12), `FromFieldPath` (7), `FromValue` (4).

New in the v2-era P&T: top-level `writeConnectionSecretToRef {name, namespace, patches}` where
patches are restricted to `FromCompositeFieldPath|CombineFromComposite` and `toFieldPath` must
be literally `"name"` or `"namespace"`.

### 1.5 DSL mapping table — P&T operation → compositionfactory blueprint

| P&T construct | Frequency | Blueprint YAML (proposed) | Canvas |
|---|---|---|---|
| `FromCompositeFieldPath` (no transforms) | 61% | `from: spec.region` / `to: spec.forProvider.region` | edge XR-field → MR-field |
| `+ policy.fromFieldPath: Required` | 6% of patches | `required: true` → emits `{{- with $x }}` guard | edge badge |
| `PatchSet` | 18% | `mixins: [common-fields]` at resource level; `mixins:` block at blueprint top | reusable node group |
| `ToCompositeFieldPath` | 13% | `statusFrom: {resource: bucket, path: status.atProvider.arn, to: status.arn}` | edge MR-status → XR-status |
| `CombineFromComposite` (`fmt`) | 7% | `from: [a, b]` + `fmt: "%s-%s"` | N→1 format node |
| transform `string`/`Format` | 85% of transforms | `fmt: "%s-provider-config"` | inline field on edge |
| transform `map` | 9% | `map: {EU: eu-north-1, US: us-east-2}` | lookup-table node |
| transform `convert` | 1.4% | `castTo: string\|bool` | inline field |
| transform `string`/`TrimPrefix`, `Regexp` | 3.5% | `trimPrefix:` / `regexp: {match:, group:}` | inline field |
| transform `match`, `math`, `Join`, `Replace`, `ToSha*` | **0%** | **rawTemplate** (`printf`, `mul`, `sha256sum` are all sprig builtins anyway) | raw escape |
| `readinessChecks: [{type: None}]` | 56 uses | `ready: never` | node badge |
| `readinessChecks: MatchString` | 16 uses | `ready: {field: status.atProvider.state, equals: available}` | node badge |
| `connectionDetails` | 23 uses | `connectionDetails:` list (emit `meta.gotemplating .../CompositeConnectionDetails`) | node output port |
| `environment.patches`, `*Environment` combines | **0 uses** | not supported | — |

---

## 2. function-kcl and function-cue: what people express there that go-templating makes awkward

**Prevalence:** `krm.kcl.dev/v1alpha1` = **666 files** on GitHub (KCL is genuinely the #3
function after P&T 1,188 and go-templating 914). `cue.fn.crossplane.io` = **10 files**;
`"function-cue"` = 81. CUE is niche; KCL is not.

### 2.1 KCL — four things go-templating cannot do cleanly

**(a) Post-hoc mutation of already-desired resources (`target: PatchDesired` / `PatchResources`).**
KCL is the only mainstream function besides P&T that can *edit resources another step created*.
From `https://github.com/crossplane-contrib/function-kcl/blob/main/examples/patch_desired/patching_multiple/composition.yaml`:

```yaml
    - step: generate desired resources
      functionRef: {name: kcl-function}
      input:
        apiVersion: krm.kcl.dev/v1alpha1
        kind: KCLInput
        spec:
          source: |
            items = [{
                apiVersion: "v1"
                kind: "XR"
                metadata.annotations: {
                    "krm.kcl.dev/composition-resource-name" = "bucket1"
                }
                spec.forProvider.network: "some-network"
            }, ...]
    - step: patch desired resources
      functionRef: {name: kcl-function}
      input:
        apiVersion: krm.kcl.dev/v1alpha1
        kind: KCLInput
        spec:
          target: PatchDesired
          source: |
            items = [{
                metadata.name = "bucket1"
                spec.forProvider.network: "some-override-network1"
            }, ...]
```

Full `target` enum from its README: `Default` (create + set XR fields), `Resources`,
`PatchDesired`, `PatchResources`, `XR`.

**(b) Assertions / input validation with a real error message.**
`https://github.com/crossplane-contrib/function-kcl/blob/main/examples/resources/regex/composition.yaml`:

```yaml
          source: |
            import regex
            name: str = option("params").name or ""
            assert regex.match(name, r"[A-Za-z_][A-Za-z0-9_]*"), "invalid name: ${name}, expected the regex [A-Za-z_][A-Za-z0-9_]*"
```

go-templating's equivalent is `{{ fail "..." }}` or emitting a `ClaimConditions` doc — clumsier
and it does not stop the pipeline.

**(c) Typed reusable schemas (real functions/structs), not string templates.**
`https://github.com/crossplane-contrib/function-kcl/blob/main/examples_kcl/eks/composition.k` defines
`schema usage:` and `schema chart:` with typed optional fields (`_chartVersion?: str`) and
branches inside a schema body:

```
schema usage:
    _nameSuffix: str
    _kind: str = "Object"
    spec = {
        by = {
            if _kind == "Object":
                apiVersion = "kubernetes.crossplane.io/v1alpha2"
            elif _kind == "Release":
                apiVersion = "helm.crossplane.io/v1beta1"
            kind = _kind
        }
    }
```

**(d) OCI-distributed shared logic.** `source: oci://ghcr.io/kcl-lang/crossplane-xnetwork-kcl-function`
(`examples/resources/network/oci_composition.yaml`) — the whole composition body is a versioned
artifact. go-templating has no equivalent (`source: Inline | FileSystem` only).

KCL also has its own readiness escape (`krm.kcl.dev/ready: "True"` annotation), exactly
paralleling `gotemplating.fn.crossplane.io/ready`.

### 2.2 CUE — the relevant lesson is architectural, not syntactic

`https://github.com/crossplane-contrib/function-cue/blob/main/examples/simple/pkg/compositions/s3bucket.cue`
generates **the XRD and the Composition together, from a generated OpenAPI schema**, with the
Crossplane API types imported as CUE definitions:

```cue
import (
	xp "github.com/crossplane/crossplane/apis/apiextensions/v1"
	schemas "cue-functions.io/examples/simple/zz_generated/schemas"
	scripts "cue-functions.io/examples/simple/zz_generated/scripts"
)
let version = "v1alpha1"
let pluralName = "xs3buckets"
let groupName = "simple.cuefn.example.com"

_xrds: s3Bucket: xp.#CompositeResourceDefinition & {
	spec: versions: [{ schema: openAPIV3Schema: schemas.components.schemas.S3BucketV1alpha1 }]
}
_compositions: s3Bucket: xp.#Composition & {
	spec: pipeline: [
		{ step: "run cue composition", functionRef: name: "fn-cue-examples-simple",
		  input: {source: "Inline", script: scripts.s3bucket} },
		{ step: "run auto ready", functionRef: name: "fn-auto-ready" },
	]
}
```

**This is compositionfactory's competition and its validation.** CUE users pay a large syntax
tax to get exactly what we give for free: one source of truth generating XRD + Composition,
with the XRD schema and the field mappings type-checked against each other. Our differentiator
is that we derive the *provider* side from CRD schemas too, and we do not require learning CUE.

*Graph GUI:* KCL/CUE steps are opaque. Model them as a generic "function step" node with a
code blob and declared inputs/outputs. Do not try to parse them.

---

## 3. function-extra-resources / required resources — and how a canvas draws an external lookup

### 3.1 There are now THREE ways to read a resource outside the composition

**(A) function-extra-resources** — `extra-resources.fn.crossplane.io/v1beta1 Input`, **79 files**
GitHub-wide, **20 of 356** corpus compositions (5.6%). Exact schema
(`https://github.com/crossplane-contrib/function-extra-resources/blob/main/input/v1beta1/resource_select.go`):

```
InputSpec:
  context: {key: string}                    # default "apiextensions.crossplane.io/extra-resources"
  policy:  {resolution: Required|Optional}  # default Required
  extraResources[]:
    type: Reference | Selector              # default Reference
    apiVersion, kind, into (required key), namespace?
    ref:      {name}
    selector: {maxMatch?, minMatch?, sortByFieldPath (default metadata.name),
               matchLabels[]: {key, type: FromCompositeFieldPath|Value,
                               valueFromFieldPath?, value?,
                               fromFieldPathPolicy: Required|Optional}}
```

Real production use — a label-selector lookup whose *label value comes from an XR field*, with
`Optional` so a missing ref degrades gracefully
(`https://github.com/estenrye/flux-platform-src/blob/main/applications/crossplane-resources/xnetworksegment/composition.yaml`):

```yaml
  - step: fetch-unifi-network
    functionRef:
      name: function-extra-resources
    input:
      apiVersion: extra-resources.fn.crossplane.io/v1beta1
      kind: Input
      spec:
        extraResources:
          - kind: XUnifiNetwork
            apiVersion: platform.rye.ninja/v1alpha1
            into: XUnifiNetwork
            type: Selector
            selector:
              maxMatch: 1
              minMatch: 0
              matchLabels:
                - key: platform.rye.ninja/unifi-network-name
                  type: FromCompositeFieldPath
                  valueFromFieldPath: spec.unifiNetworkRef.name
                  fromFieldPathPolicy: Optional
```

Consumed downstream out of context:
`{{ $extraResources := index .context "apiextensions.crossplane.io/extra-resources" }}` then
`{{ $networks := index $extraResources "XUnifiNetwork" }}`.

**(B) go-templating's inline `ExtraResources` meta doc** — **35 files** GitHub-wide, **19 in
corpus**. The requirement itself is *templated*, so the lookup key can be computed from the XR.
From `https://github.com/crossplane-contrib/function-go-templating/blob/main/example/extra-resources/composition.yaml`:

```yaml
          template: |
            ---
            apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
            kind: ExtraResources
            requirements:
              bucket:
                apiVersion: s3.aws.upbound.io/v1beta1
                kind: Bucket
                matchName: my-awesome-{{ .observed.composite.resource.spec.environment }}-bucket
            {{- with .extraResources }}
            {{ $someExtraResources := index . "bucket" }}
            {{- range $i, $extraResource := $someExtraResources.items }}
            ---
            apiVersion: kubernetes.crossplane.io/v1alpha1
            kind: Object
            metadata:
              annotations:
                gotemplating.fn.crossplane.io/composition-resource-name: bucket-configmap-{{ $i }}
            ...
```

Requirement selectors here are `matchName` **or** `matchLabels` (README lines 171-192).
Results land at `.extraResources.<key>.items` **and** in context under
`apiextensions.crossplane.io/extra-resources`. Note the two-pass semantics: the function runs
once returning requirements, Crossplane re-invokes it with `.extraResources` populated — hence
the `{{- with .extraResources }}` guard is mandatory.

**(C) Crossplane v2 `requirements.requiredResources` — step-level, and this is what the user
should emit.** `https://github.com/crossplane/docs/blob/master/content/v2.4/composition/compositions.md`
("Crossplane v1 called this feature 'extra resources.' The v2 API uses the name 'required
resources' and adds support for bootstrap requirements"):

```yaml
  pipeline:
  - step: create-deployment-from-config
    functionRef:
      name: crossplane-contrib-function-python
    requirements:
      requiredResources:
      - requirementName: app-config
        apiVersion: v1
        kind: ConfigMap
        name: app-configuration
        namespace: default
    input: ...
```

**0 occurrences in the corpus** — it is new in v2.x and nobody has published it yet. But
function-go-templating v0.12.0 already supports it: `getExtraResources` resolves
`requiredResources[<name>].items` (`function_maps.go:156`) and `mergeRequiredResourcesToContext`
handles `req.GetRequiredResources()` (`extraresources.go:85`). function-kcl documents it too
(README "Required resources"). Crossplane's own docs say: *"Use bootstrap requirements when
possible for better performance."*

### 3.2 How a canvas represents an external lookup

A **Lookup node** with no incoming resource edge and a distinct visual class (dashed border /
"read-only" icon), carrying:
- identity: `apiVersion` + `kind` (+ `namespace` for v2 namespaced XRs)
- selection: exactly one of `matchName` (literal or bound to an XR field) or `matchLabels`
  (each label value literal or bound to an XR field — the `type: FromCompositeFieldPath` case)
- cardinality: `minMatch`/`maxMatch`/`sortByFieldPath` (extra-resources only)
- an output port named by `into`/`requirementName`, whose type is **a list**, feeding edges into
  ordinary resource nodes and into loop nodes.

Because the output is a list, a Lookup node's edge should default into a **range node**, and a
`maxMatch: 1` lookup should render as a scalar port (`index $list 0`). The `Optional`/`Required`
resolution policy is a node badge. Emit v2 `requirements.requiredResources` by default with a
"v1 compatibility" toggle that emits function-extra-resources instead.

**Structural, not raw.** Everything above is enumerable.

---

## 4. The long tail — what each is, how common, whether it earns a canvas node

Prevalence figures = GitHub code-search file counts (Aug 2026) + corpus occurrences of 356
compositions.

### function-environment-configs — **163 files GitHub / 51 corpus compositions (14.3%)** → **YES, first-class node**
Resolves `EnvironmentConfig` objects and merges their `data` into context under
`apiextensions.crossplane.io/environment` (read 80 times/50 files in corpus). Input schema
(`function-environment-configs/input/v1beta1/input.go`):
`spec.defaultData` (static fallback map) + `spec.environmentConfigs[]` with
`type: Reference|Selector`, plus `policy.resolution: Required|Optional`. Merge order matters:
*"the values of EnvironmentConfigs with a larger index take priority over ones with smaller
indices."* Real use, the corpus's most common non-templating first step:

```yaml
  - step: environmentConfigs
    functionRef:
      name: function-environment-configs
    input:
      apiVersion: environmentconfigs.fn.crossplane.io/v1beta1
      kind: Input
      spec:
        environmentConfigs:
        - type: Reference
          ref:
            name: platform-kvm-hosts
```
(`https://github.com/estenrye/flux-platform-src/blob/main/applications/crossplane-resources/xnetworksegment/composition.yaml`)

Canvas: a "Platform Config" node, ordered list, output port = a merged dict; edges from it into
field slots. This is the single highest-value non-resource node after Lookup.

### function-sequencer — **62 files / 10 corpus compositions (2.8%)** → **node NO, edge type YES**
Blocks creation of a resource until earlier ones in a named sequence are Ready, and (optionally)
generates `Usage`/`ClusterUsage` for deletion ordering. Input
(`function-sequencer/input/v1beta1/input.go`): `rules[].sequence[]` of composition resource
names, plus `condition` (a CEL expression gating the whole rule), `createOnly`/`deleteOnly`,
`enableDeletionSequencing`, `replayDeletion`, `usageVersion: v1|v2`, `cacheTTL`,
`resetCompositeReadiness`. Real use:

```yaml
    - step: enforce-creation-sequence
      functionRef:
        name: crossplane-contrib-function-sequencer
      input:
        apiVersion: sequencer.fn.crossplane.io/v1beta1
        kind: Input
        rules:
          - sequence: ["account", "endpointblob"]
          - sequence: ["account", "endpointfile"]
          - sequence: ["account", "endpointqueue"]
```
(`https://github.com/platformplane/catalog-crossplane-azure/blob/main/package/azurestorage/v2/composition.yaml`)

**This is a graph.** The four rules above are one fan-out from `account`. Draw a dependency edge
between resource nodes; compile the transitive edge set into `rules[].sequence[]`. Note the
corpus contains 25 hand-written `Usage`/`ClusterUsage` resources across 17 files — people also
do this manually, which is the same edge.

### function-conditional-patch-and-transform — **14 files** → **NO node; but steal the idea**
A fork (`xpkg.upbound.io/borrelli-org/function-conditional-patch-and-transform`) that adds one
field to P&T: a per-resource CEL `condition`. Used in upbound/platform-ref-multi-k8s and
platform-ref-upbound-spaces:

```yaml
        resources:
          - name: XNetworkAWS
            condition: observed.composite.resource.spec.parameters.cloud == "aws"
            base:
              apiVersion: aws.platform.upbound.io/v1alpha1
              kind: XNetwork
```
(`https://github.com/upbound/platform-ref-multi-k8s/blob/main/apis/composition.yaml`)

Low adoption, but it proves the demand: **"include this resource only if X"** is a first-class
authoring concept. Our DSL must have `when:` on a resource node (compiling to `{{- if ... }}`
around the whole doc) — the corpus shows this idiom in ~23% of go-templating compositions.

### function-status-transformer — **14 files / 2 corpus compositions** → **NO node** (generic step)
Maps composed-resource conditions onto XR/claim conditions and Kubernetes Events, with regex
capture groups usable in the message template. Input
(`function-status-transformer/input/v1beta1/*.go`): `statusConditionHooks[].matchers[]` with
`type: AnyResourceMatchesAnyCondition | AnyResourceMatchesAllConditions |
AllResourcesMatchAnyCondition | AllResourcesMatchAllConditions`, `resources[].name` (regex),
`conditions[]`, `includeCompositeAsResource`, `includeExtraResources`; then
`setConditions[]{target: Composite|CompositeAndClaim, force, condition{type,status,reason,message}}`
and `createEvents[]`. Real use:

```yaml
      statusConditionHooks:
      - matchers:
        - type: AnyResourceMatchesAnyCondition
          resources:
          - name: deployment
          conditions:
          - type: Available
            status: 'True'
        setConditions:
        - target: Composite
          force: true
          condition:
            type: RolloutCompleted
            status: 'True'
            reason: ReplicaSetUpdated
```
(`https://github.com/dag-andersen/kubecon-us-2025-code/blob/main/tools/crossplane/compositions/composition.yaml`)

The user already covers 90% of this with `gotemplating.fn.crossplane.io/ready` +
`meta.gotemplating .../ClaimConditions`. The regex-capture-into-message feature is the only
unique bit and it is rare.

### function-cel-filter — **19 files / 1 corpus composition** → **NO node**
Post-filters desired resources produced by *earlier* steps. Input
(`function-cel-filter/input/v1beta1/input.go`): `filters[]{name (regex, auto-anchored ^…$),
expression (CEL over `observed`/`desired`/`context`)}`. *"Desired composed resources that don't
match any filter are always included"* — i.e. an allow-list-with-default-allow. Semantically
overlaps `when:` on a node; our `when:` is the better UX. Mention in docs, do not model.

### function-python — **41 files** → **NO node** (generic step)
`python.fn.crossplane.io/v1beta1 Script`, an arbitrary `compose(req, rsp)`. It is the function
Crossplane's own v2.4 docs use to demonstrate `requiredResources`, and it is the escape hatch
for genuinely imperative work (the repo's headline example opens a TLS socket to read a
certificate expiry). Also note the docs example runs it in an `ops.crossplane.io/v1alpha1
Operation`, not a Composition — Operations are a v2 sibling surface worth knowing about.

### function-shell — **9 files** → **NO node**, and flag it as an anti-pattern
`shell.fn.crossplane.io/v1beta1 Parameters`: `shellCommand`, `shellCommandField`,
`shellEnvVars[]{key, value|valueRef|fieldRef, type: Value|ValueRef|FieldRef}`,
`shellEnvVarsRef{name, keys[]}` (from a Secret via DeploymentRuntimeConfig), `stdoutField`,
`stderrField`, `cacheTTL`. Real example shells out to `curl | jq` against Datadog and writes
stdout to `status.atFunction.shell.stdout`
(`https://github.com/crossplane-contrib/function-shell/blob/main/example/datadog-dashboard-ids/composition.yaml`).
Nine files on all of GitHub. Not worth a node.

### function-auto-ready — **corpus: 262 of 356 (73.6%)** → not a node, an implicit tail step
Only 22 files GitHub-wide reference `autoready.fn.crossplane.io` because it takes no input —
people write `functionRef: {name: function-auto-ready}` and nothing else. Corpus step names are
overwhelmingly `automatically-detect-ready-composed-resources` (154) or `auto-ready` (90).
**The generator should append it automatically and let the user turn it off.**

---

## 5. The multi-step pipeline reality

**Distribution across the 356 real composition files:** 1 step **61 (17%)**, 2 steps **175
(49%)**, 3 steps **82 (23%)**, 4 steps 13, 5 steps 6, 6 steps 9, 7 steps 3, 9 steps 3, 10 steps
3, 12 steps 1. **≥3 steps = 120 files (34%).** The 2-step mode is almost always
`render + auto-ready`.

**Authoring granularity:** of 486 inline go-templating steps, **53.5% emit exactly one named
resource**, 13.8% emit two, 4.3% emit none (status/context only), 11.9% emit 6+. *One step per
resource is the dominant style.* A canvas whose resource nodes map 1:1 to pipeline steps is
therefore idiomatic, not a contrivance.

### 5.1 Real 9-step pipeline — Delivery Hero, `asyncactor-sqs` (758 lines)
`https://github.com/deliveryhero/asya/blob/main/deploy/helm-charts/asya-crossplane/templates/composition-sqs.yaml`

| # | Step | Contributes | Why the order |
|---|---|---|---|
| 1 | `extract-user-labels` | filters system-prefixed labels off the XR and **writes them into pipeline context** | must precede every render step that stamps labels |
| 2 | `resolve-flavors` (custom fn, conditional on a Helm value) | merges EnvironmentConfig "flavor" data and **rewrites the XR's desired spec** | later steps read `.spec` as if the user had written the resolved values |
| 3 | `render-sqs-queue` | the SQS Queue MR | must precede 4 (SA policy needs the queue) |
| 4 | `render-serviceaccount` | IRSA SA | |
| 5 | `render-triggerauthentication` | KEDA TriggerAuthentication | must precede 6 |
| 6 | `render-scaledobject` | KEDA ScaledObject | |
| 7 | `render-deployment` | the workload | |
| 8 | `patch-status-and-derive-phase` | reads `.observed.resources` for queue+scaledobject+deployment, derives `status.phase` ∈ {Creating, Ready, Napping} | **must be last before auto-ready** — it needs every prior step's resource to exist in `observed` |
| 9 | `automatically-detect-ready-composed-resources` | function-auto-ready | terminal |

Step 1 writing context (this is the `Context` meta kind — only 12 occurrences/8 files in the
whole corpus, so it is rare but load-bearing where used):

```yaml
            apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
            kind: Context
            data:
              "asya/user-labels":
                {{- $userLabels | toYaml | nindent 4 }}
```

Step 8's readiness derivation — the pattern the user already uses for `availableReplicas`,
generalised to three resources and a phase enum:

```yaml
            {{- if index .observed.resources "sqs-queue" -}}
              {{- $q := (index .observed.resources "sqs-queue").resource -}}
              {{- if and (hasKey $q "status") (hasKey $q.status "conditions") -}}
                {{- range $q.status.conditions -}}
                  {{- if and (eq .type "Ready") (eq .status "True") -}}
                    {{- $queueReady = true -}}
            ...
            {{- $phase := "Creating" -}}
            {{- if and $queueReady $kedaReady $workloadReady -}}
              {{- $phase = "Ready" -}}
              {{- if eq (int $workloadReplicas) 0 -}}
                {{- $phase = "Napping" -}}
```

### 5.2 Real 10-step pipeline — validation gates first
`https://github.com/estenrye/flux-platform-src/blob/main/applications/crossplane-resources/delegated-hosted-zone-aws/composition.yaml`

Steps: `environmentConfigs` → `validate-trust-anchor` → `validate-delegated-zone-provider-config`
→ `validate-iam-provider-configs` → `validate-cloudflare-inputs` → `create-zone` →
`create-iam-resources` → `create-ns-records` → `status-update` → `auto-ready`.

**Four dedicated validation steps that create no resources**, each emitting only a condition.
They must run after `environmentConfigs` (the fallback value lives in the environment) and
before any `create-*`:

```yaml
          {{ $trustAnchorArn := .observed.composite.resource.spec.trustAnchorArn }}
          {{ if not $trustAnchorArn }}
            {{ $environment := index .context "apiextensions.crossplane.io/environment" }}
            {{ if $environment }}
              {{ $trustAnchorArn = index $environment "trustAnchorArn" }}
            {{ end }}
          {{ end }}
          {{ if not $trustAnchorArn }}
          ---
          apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
          kind: ClaimConditions
          conditions:
            - type: TrustAnchorResolved
              status: "False"
              reason: MissingTrustAnchorArn
              message: "No trustAnchorArn was resolved. Set spec.trustAnchorArn on the claim or define data.trustAnchorArn in the platform-iam-rolesanywhere EnvironmentConfig."
              target: CompositeAndClaim
          {{ else }}
          ...
```

The "XR field, else EnvironmentConfig, else fail" resolution chain appears repeatedly. **This is
a DSL primitive candidate:** `source: [spec.trustAnchorArn, env.trustAnchorArn]` +
`onMissing: {condition: TrustAnchorResolved, reason: MissingTrustAnchorArn, message: "..."}`.

### 5.3 Real 6-step pipeline — data flowing through XR status
`https://github.com/upbound/function-cidr/blob/main/apis/composition-pipeline-context.yaml`

`pull-extra-resources` → `debug-context` → `cidr-subnets-partitions` → `cidr-subnets-private` →
`cidr-subnets-public` → `render-templates`. Steps 3-5 are *strictly serial data dependencies*
expressed by field paths, not by any dependency mechanism:

```yaml
    - step: cidr-subnets-partitions
      input:
        prefixField: context.apiextensions\.crossplane\.io/extra-resources.XCluster.0.spec.cidrBlock
        outputField: status.atFunction.cidr.partitions
    - step: cidr-subnets-private
      input:
        prefixField: desired.composite.resource.status.atFunction.cidr.partitions[0]
        outputField: status.atFunction.cidr.private.subnets
    - step: cidr-subnets-public
      input:
        prefixField: desired.composite.resource.status.atFunction.cidr.partitions[1]
        outputField: status.atFunction.cidr.public.subnets
```

Note `status.atFunction.*` as a convention for "function scratchpad on the XR" (function-shell
uses it too: `status.atFunction.shell.stdout`). Also note the escaped dot in the context key
(`apiextensions\.crossplane\.io/extra-resources`) — a fieldpath-escaping rule any generator
emitting context references must get right.

### 5.4 Cross-function step chaining — P&T patching what go-templating created
`https://github.com/crossplane-contrib/function-patch-and-transform/blob/main/example/multistep/composition.yaml`.
A P&T resource entry **with no `base`** attaches to a resource an earlier step produced
(`ComposedTemplate.Base` doc: *"If base is omitted, a previous Function within the pipeline must
have produced the named composed resource"*), keyed by the go-templating resource name:

```yaml
    - step: render-templates
      functionRef: {name: function-go-templating}
      input:
        ...
          template: |
            apiVersion: s3.aws.upbound.io/v1beta1
            kind: BucketACL
            metadata:
              annotations:
                {{ setResourceNameAnnotation "bucketACL" }}
              ...
                region: {{ .desired.resources.bucket.resource.spec.forProvider.region }}
    - step: patch-and-transform-again
      functionRef: {name: function-patch-and-transform}
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        resources:
          - name: bucketACL # matches setResourceNameAnnotation above, no `base` specified
            patches:
              - type: FromCompositeFieldPath
                fromFieldPath: "spec.acl"
                toFieldPath: "spec.forProvider.acl"
```

**The composed-resource *name* is the universal join key across every function** —
`gotemplating.fn.crossplane.io/composition-resource-name` (1,800 occurrences / 601 files),
`krm.kcl.dev/composition-resource-name`, P&T `resources[].name`, sequencer `rules[].sequence[]`,
status-transformer `resources[].name`, cel-filter `filters[].name`. **Node identity in the
canvas must be exactly this string, and it must be stable across regeneration.**

### 5.5 Inter-step data channels, ranked by real usage (1,409 files scanned)

| Channel | Occurrences / files | Notes |
|---|---|---|
| `.observed.resources.<name>` | **422 / 121** | the workhorse; requires the resource to already exist |
| `index .context "<key>"` | 130 / 75 | env-configs, extra-resources, custom `Context` |
| `getComposedResource` helper | 96 / 31 | sugar over `.observed.resources` |
| `apiextensions.crossplane.io/environment` key | 80 / 50 | function-environment-configs output |
| `getResourceCondition` helper | 58 / 30 | condition-aware readiness derivation |
| `apiextensions.crossplane.io/extra-resources` key | 29 / 23 | |
| XR `status.atFunction.*` scratchpad | (function-cidr, function-shell) | serial function chaining |
| `.desired.resources.<name>` | **1 / 1** | almost nobody reads another step's *desired* state |

**Ordering rules a generator must enforce:**
1. context producers (env-configs, extra/required resources, custom `Context`) **before** any
   step that reads context;
2. validation/gate steps **after** context producers, **before** resource renders;
3. resource renders in dependency order when a later template reads an earlier resource's
   `.observed` state;
4. status/condition derivation **second-to-last** (it reads `.observed.resources` for everything);
5. `function-auto-ready` **last**, always.

---

## Appendix — headline prevalence table (GitHub code search, authenticated, Aug 2026, file counts)

| Query | Files |
|---|---|
| `"pt.fn.crossplane.io"` | **1,188** |
| `"gotemplating.fn.crossplane.io"` | **914** |
| `"krm.kcl.dev/v1alpha1"` | **666** |
| `"environmentconfigs.fn.crossplane.io"` | 163 |
| `"extra-resources.fn.crossplane.io"` | 79 |
| `"sequencer.fn.crossplane.io"` | 62 |
| `"python.fn.crossplane.io"` | 41 |
| `meta.gotemplating.fn.crossplane.io` + `ExtraResources` | 35 |
| `"autoready.fn.crossplane.io"` (input rarely written) | 22 |
| `"cel.fn.crossplane.io"` | 19 |
| `"conditional-patch-and-transform"` | 14 |
| `"statusConditionHooks"` | 14 |
| `"cue.fn.crossplane.io"` | 10 |
| `"shell.fn.crossplane.io"` | 9 |
