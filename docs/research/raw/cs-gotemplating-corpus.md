# function-go-templating in the wild — the canonical corpus

**Method.** GitHub code search (`gotemplating.fn.crossplane.io/composition-resource-name`, `setResourceNameAnnotation`,
`gotemplating.fn.crossplane.io/ready`, `source: FileSystem`) → 900 raw files downloaded; plus full clones of 50+
Crossplane platform repos. Split into YAML documents, kept every doc that is `kind: Composition` **and** references
`gotemplating.fn.crossplane.io/v1beta1`, deduped by whitespace-normalised content hash.

**Corpus: 381 distinct real Compositions across ~127 repos / ~102 distinct orgs & users**, of which **347 carry inline
templates** totalling **53,069 template lines / 21,036 template actions**. Plus the upstream repo at v0.12.4
(`crossplane-contrib/function-go-templating`, HEAD `5d48403`, tags through v0.12.4) read in full: README, all 24
`example/` compositions, `function_maps.go`, `fn.go`, `extraresources.go`, `input/v1beta1/input.go`.

Scratch data: ``
(`corpus/`, `repos/`, `docs2.json`, `freq.txt`, `actions.txt`, `struct.txt`).

---

## What this means for the DSL — 5 bullets

1. **The DSL's centre of gravity is a variable prelude + static YAML with scalar interpolation, not clever templating.**
   61% of all template lines are pure static YAML; 19% are pure control/assign lines; only 19% interpolate a value.
   Of the 10,103 value-bearing expressions, **73% are a bare path or variable reference** (`{{ $name }}`,
   `{{ $spec.location }}`) and another 9% are a path plus one trivial pipe (`| quote`, `| default X`, `| b64enc`).
   **47% of compositions open with a ≥3-assignment prelude** (`{{- $xr := .observed.composite.resource }}`,
   `{{- $spec := $xr.spec }}`, `{{- $name := $xr.metadata.name }}`). Your `fieldMapping` primitive should compile to
   *prelude assignment + reference*, exactly the shape humans already write — that alone covers ~82% of expressions.

2. **Conditionals beat loops 3:1 and both are usually whole-resource, not whole-field.** `if` appears in **57%** of
   compositions (2,371 occurrences) vs `range` in **40%** (580 occurrences). Critically: **23% have an `{{if}}` that
   wraps one or more entire `---` resource documents** (315 such blocks) and **25% have a `{{range}}` that does**
   (165 blocks). So a graph GUI needs *node-level* `includeIf` and *node-level* `forEach` as first-class edges/props,
   plus *field-level* `omitIfEmpty`. **60% of the corpus is fully expressible with mappings + conditionals alone**
   (no loop, no `define`, no `set`).

3. **Three idioms are far more common than the docs suggest and must be first-class:** (a) `| default` — **56%** of
   compositions call `default` (1,530 occurrences), it is the single most common transform; (b) **dict-literal lookup
   tables** (t-shirt sizing) — **17%** build `$sizeMap := dict "sm" (dict ...) ...` then `get/index $sizeMap $key`,
   often with variable *reassignment* inside `if` (`$x = ...` appears in **22%**, 947 occurrences); (c) **conditional
   field omission** — `{{- if ne $x "" }}fieldName: {{ $x }}{{- end }}` is how every status block and every optional
   spec field is written. Note **`hasKey` is used 376 times across 25 compositions** as the presence test — because
   `missingkey=default` silently yields `<no value>`.

4. **Reference wiring is overwhelmingly *name/ref*, not selectors — and readiness is mostly delegated.**
   `<x>Ref: {name: …}` in **56%** (972 occurrences) vs `<x>Selector: matchLabels` **8%** and `matchControllerRef`
   **6%**; `providerConfigRef` in **57%**; `crossplane.io/external-name` in **24%**. Only **32%** read an observed
   composed resource at all (`.observed.resources` / `getComposedResource` / `dig "resources"`) — cross-resource data
   flow is the minority case. **67% just add `function-auto-ready`** to the pipeline; the `gotemplating.../ready`
   annotation appears in only **12%**, and when it does it is **hardcoded `"True"` in 11% of compositions (218
   occurrences)** — almost always to force provider-kubernetes `Object`s ready. Only **8 compositions** derive
   readiness from `availableReplicas`/`readyReplicas`. Your "derive readiness from a Deployment" feature is *rarer
   than you think but exactly the right shape* — model it, but default the graph to auto-ready.

5. **The rawTemplate escape hatch only has to catch ~5% of the corpus, but that 5% is unbounded.** Nested templates
   (`define`/`template`/`include`) appear in **2%** (6 compositions, 25 occurrences total); `set`/`mergeOverwrite`/
   `regexSplit`-driven recursive template engines in **5%**. Conversely the *meta* features are also rare and can be
   simple structured nodes: `ExtraResources` **4%** (14), `CompositeConnectionDetails` **5%** (19, v1-only — the
   function refuses it for v2 XRs), `ClaimConditions` **1%** (4), `Context` write **1%** (3). Context *reads*
   (`.context`, environment key) are much more common at **18% / 13%** — model that as an input source, not an escape.

---

## Part 1 — Upstream: every technique the maintainers demonstrate (v0.12.x)

Source: <https://github.com/crossplane-contrib/function-go-templating> (HEAD `5d48403`, tags to `v0.12.4`).

### 1.1 Input surface (`input/v1beta1/input.go`)

```go
type GoTemplate struct {
	Delims     *Delims                    `json:"delims,omitempty"`   // {left,right}
	Source     TemplateSource             `json:"source"`             // Inline | FileSystem | Environment
	Inline     *TemplateSourceInline      `json:"inline,omitempty"`   // XOR: template (string) | templates ([]string)
	FileSystem *TemplateSourceFileSystem  `json:"fileSystem,omitempty"` // dirPath
	Environment *TemplateSourceEnvironment `json:"environment,omitempty"` // key
	TTL        string                     `json:"ttl"`                // default "1m0s"
	Options    *[]string                  `json:"options,omitempty"`  // e.g. missingkey=error
}
```
Corpus reality: **Inline 472 steps, FileSystem 40, Environment 2**. `templates: []` (array form) exists chiefly so
Kustomize can JSON-patch templates in one at a time (`example/inline-templates/kustomization.yaml`). Custom `delims`
used by **4/381**. `options: [missingkey=error]` set by **3/381** — nobody opts into strict mode.

### 1.2 The 11 function-specific template functions (`function_maps.go`)

`randomChoice`, `toYaml`, `fromYaml`, `getResourceCondition`, `setResourceNameAnnotation`, `getComposedResource`,
`getCompositeResource`, `getComposedConnectionDetails`, `getExtraResources`, `getExtraResourcesFromContext`,
`getCredentialData`, plus `include` (Helm-style) — on top of **all of Sprig except `env` and `expandenv`**, which are
deleted for security:

```go
// Sprig's env and expandenv can lead to information leakage (injected tokens/passwords).
sprigFuncs := sprig.FuncMap()
delete(sprigFuncs, "env"); delete(sprigFuncs, "expandenv")
```
<https://github.com/crossplane-contrib/function-go-templating/blob/main/function_maps.go>

`getExtraResources` now checks the **Crossplane v2 `requiredResources` key first**, falling back to the v1
`extraResources` key:
```go
path := fmt.Sprintf("requiredResources[%s].items", name)
if err := fieldpath.Pave(req).GetValueInto(path, &ers); err != nil {
    path := fmt.Sprintf("extraResources[%s].items", name)
```

### 1.3 The four meta kinds (`apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1`)

`fn.go` hard-codes exactly four: `CompositeConnectionDetails`, `ClaimConditions`, `Context`, `ExtraResources` —
anything else is a fatal error:
```go
default:
  response.Fatal(rsp, errors.Errorf("invalid kind %q for apiVersion %q - must be one of CompositeConnectionDetails, Context or ExtraResources", ...))
```

`ExtraResources` gained a `Namespace` field for v2 (issue #483); note it only registers the namespaced selector under
the new `Resources` map, and the legacy `ExtraResources` map only when cluster-scoped:
```go
requirements.Resources[k] = v.ToResourceSelector()
if v.Namespace == "" { requirements.ExtraResources[k] = v.ToResourceSelector() }
```

### 1.4 The composite-resource special case (the *"same kind = status update"* rule)

```go
if cd.Resource.GetAPIVersion() == observedComposite.Resource.GetAPIVersion() &&
   cd.Resource.GetKind() == observedComposite.Resource.GetKind() && !nameFound {
   // ... mergo.Merge(&dst, src, mergo.WithOverride) into desiredComposite.status
   if ready != nil { desiredComposite.Ready = *ready }
   continue
}
```
**Emitting a document of the XR's own kind with no resource-name annotation updates the XR status; with one, it
recursively composes another XR.** This is a genuine DSL-visible mode switch (`example/recursive/`), and
`mergo.WithOverride` means status is *merged*, not replaced — pipelines can each contribute status keys.

Missing annotation on any *other* kind is fatal:
```go
response.Fatal(rsp, errors.Errorf("%q template is missing required %q annotation", obj.GetKind(), annotationKeyCompositionResourceName))
```

### 1.5 v1 vs v2 connection details — the README is explicit

> For **v2 composite resources**, the `CompositeConnectionDetails` resource is not supported. Instead, you should
> compose an explicit Kubernetes `Secret` resource that aggregates connection details from the other composed
> resources.

```yaml
apiVersion: v1
kind: Secret
metadata:
  annotations:
    {{ setResourceNameAnnotation "connection-secret" }}
{{ if eq $.observed.resources nil }}
data: {}
{{ else }}
data:
  server-endpoint: {{ (index $.observed.resources "my-server").resource.status.atProvider.endpoint | b64enc }}
{{ end }}
```
<https://github.com/crossplane-contrib/function-go-templating/blob/main/README.md>
**This is the exact pattern your v2/Namespaced generator must emit** — and note the mandatory `eq $.observed.resources nil`
guard for the first reconcile.

### 1.6 Enumerated upstream examples (all 24, distinct techniques)

| example | technique demonstrated |
|---|---|
| `example/inline` | `range $i := until (… \| int)` numeric loop; `setResourceNameAnnotation (print "test-user-" $i)`; `dig` with default fallback; `userSelector.matchLabels`; `writeConnectionSecretToRef`; nil-observed guard; XR status doc |
| `example/filesystem` | identical template served from `/templates` dir; raw annotation form instead of the helper |
| `example/inline-templates` | `templates: []` + Kustomize `op: add /spec/pipeline/0/input/inline/templates/-` |
| `example/custom-delims` | `[[ ]]` delimiters, `randomChoice`, `b64enc` |
| `example/recursive` | XR composing XRs with `compositionRef.name` to terminate recursion |
| `example/conditions` | `ClaimConditions` with `target: CompositeAndClaim` |
| `example/context` | write `Context`; update `apiextensions.crossplane.io/environment`; read it back with `index .context "…/environment" "complex" "c" "d"`; `toYaml \| nindent 6` |
| `example/environment` | `source: Environment`, `environment.key: template` — the whole template comes from an EnvironmentConfig |
| `example/extra-resources` | `kind: ExtraResources` + `{{- with .extraResources }}{{ $x := index . "bucket" }}{{- range $i, $er := $x.items }}` |
| `example/functions/getExtraResources` | same via helper: `{{- range $i, $er := default (list) (getExtraResources . "bucket") }}` |
| `example/functions/getExtraResourcesFromContext` | two gotemplating steps: step 1 declares requirements, step 2 consumes from context |
| `example/functions/getComposedResource` | `{{ $flexServer := getComposedResource . "flexServer" }}` … `serverId: {{ get $flexServer.status "id" }}` |
| `example/functions/getCompositeResource` | `{{ $xr := getCompositeResource . }}` |
| `example/functions/getComposedConnectionDetails` | v2 `.m.upbound.io` groups + `matchControllerRef: true` + Secret assembly |
| `example/functions/getResourceCondition` | `{{ if eq (getResourceCondition "Ready" .observed.resources.project).Status "True" }}`; pipeline form `{{ .observed.resources.project \| getResourceCondition "Ready" \| toYaml \| nindent 4 }}` |
| `example/functions/getCredentialData` | step-level `credentials: [{name, secretRef, source: Secret}]` → `(getCredentialData . "foo-creds").username \| toString` |
| `example/functions/toYaml` / `fromYaml` | `\| toYaml \| nindent 7`; `(… \| fromYaml).key2` |
| `example/functions/include` | `{{- define "labels" -}}` + `{{- include "labels" $vals \| nindent 4}}`, with a comment admitting the alternative is awful: *"weird indentation to make it work"* / *"without include, you must define a template per indentation setting"* |

---

## Part 2 — Frequency table over 381 real Compositions

`#comps` = compositions containing the pattern (n=381); `#occ` = total occurrences; `#repos` = distinct repos.

### 2.1 Control flow & data access

| technique | #comps | % | #occ | #repos |
|---|---:|---:|---:|---:|
| reads `.observed.composite.*` | 318 | 83% | 3302 | 118 |
| multi-document output (`---`) | 312 | 82% | 1369 | 115 |
| variable assignment `{{ $x := … }}` | 247 | 65% | 2866 | 85 |
| `{{ if }}` | 218 | **57%** | 2371 | 87 |
| `default` (any form) | 215 | **56%** | 1530 | 85 |
| `printf` / `print` | 164 | 43% | 858 | 60 |
| `index` | 157 | 41% | 936 | 60 |
| `{{ range }}` | 152 | **40%** | 580 | 72 |
| `eq` / `ne` | 140 | 37% | 588 | 63 |
| `\| default` pipe form | 132 | 35% | 797 | 51 |
| `{{ else }}` / `else if` | 113 | 30% | 336 | 47 |
| `and` / `or` / `not` | 110 | 29% | 453 | 49 |
| `\| quote` | 101 | 27% | 855 | 41 |
| reads `.observed.resources` | 89 | 23% | 267 | 41 |
| `range $k, $v :=` (map or indexed) | 87 | 23% | 243 | 49 |
| `dict` | 85 | 22% | 1091 | 29 |
| variable **reassignment** `{{ $x = … }}` | 83 | 22% | 947 | — |
| `dig` | 75 | 20% | 539 | 29 |
| reads `.context` | 70 | 18% | 123 | 27 |
| `b64enc`/`b64dec` | 65 | 17% | 185 | 19 |
| `set`/`merge`/`keys`/`values`/`pluck` | 64 | 17% | 292 | 31 |
| `range $x :=` (single var) | 60 | 16% | 223 | 35 |
| `list` | 56 | 15% | 356 | 24 |
| `toYaml` | 51 | 13% | 228 | 28 |
| reads `apiextensions.crossplane.io/environment` | 50 | 13% | 82 | 16 |
| `trim*`/`replace`/`split*`/`upper`/`lower` | 48 | 13% | 250 | 32 |
| `nindent` | 46 | 12% | 203 | 28 |
| `sha1sum`/`sha256sum`/`uuidv4`/`rand*` | 44 | 12% | 72 | 18 |
| `toJson`/`fromJson` | 42 | 11% | 290 | 21 |
| `{{ with }}` optional guard | 40 | **10%** | 161 | 20 |
| reads `status.atProvider` | 38 | 10% | 132 | 24 |
| `range` over a pipeline (no vars) | 37 | 10% | 69 | 22 |
| `len` | 36 | 9% | 139 | 21 |
| `required` / `fail` | 26 | 7% | 40 | 17 |
| `hasKey` | 25 | 7% | **376** | 13 |
| `getComposedResource` | 25 | 7% | 57 | 16 |
| `until` (numeric loop) | 23 | 6% | 54 | 17 |
| `getResourceCondition` | 21 | 6% | 51 | 15 |
| `toYaml \| nindent` combo | 21 | 6% | 58 | 13 |
| `getCompositeResource` | 16 | 4% | 17 | 5 |
| `regexMatch`/`regexReplaceAll`/`regexSplit` | 15 | 4% | 31 | 5 |
| `\| indent` | 14 | 4% | 45 | 6 |
| `getCredentialData` | 12 | 3% | 13 | 2 |
| `ternary` | 7 | 2% | 16 | 7 |
| `define` | 6 | 2% | 9 | 5 |
| `include "x"` | 6 | 2% | 29 | 5 |
| `getExtraResources*` | 3 | 1% | 4 | 2 |
| `fromYaml` | 2 | 1% | 4 | 2 |
| `template "x"` | 2 | 1% | 7 | 2 |
| `getComposedConnectionDetails` | 1 | 0% | 2 | 1 |
| `semver`, `coalesce` | 0 | 0% | 0 | 0 |

### 2.2 Crossplane-specific structure

| technique | #comps | % | #occ |
|---|---:|---:|---:|
| raw `gotemplating.fn.crossplane.io/composition-resource-name:` annotation | 268 | **70%** | 1073 |
| `providerConfigRef:` | 218 | 57% | 802 |
| `<x>Ref: { name: … }` | 215 | **56%** | 972 |
| `metadata.name` templated | 287 | 75% | 2106 |
| `namespace:` templated (v2 namespaced) | 175 | 46% | 743 |
| `labels:` block present | 150 | 39% | 550 |
| namespaced `.m.upbound.io` / `.m.crossplane.io` MR group | 109 | **29%** | 313 |
| templated resource-name annotation (name computed) | 105 | 28% | 474 |
| emits a composite `status:` document | 105 | **28%** | 112 |
| `crossplane.io/external-name` | 90 | 24% | 236 |
| `setResourceNameAnnotation` helper (vs raw string) | 85 | 22% | 258 |
| `writeConnectionSecretToRef` | 62 | 16% | 77 |
| `deletionPolicy` / `managementPolicies` | 59 | 15% | 257 |
| `gotemplating.fn.crossplane.io/ready` annotation | 47 | 12% | 226 |
| — of which hardcoded `"True"` | 43 | 11% | 218 |
| — of which hardcoded `"False"` | 4 | 1% | 6 |
| — of which templated | 2 | 1% | 2 |
| emits a `kind: Secret` for connection details (v2 style) | 47 | 12% | 73 |
| `providerConfigRef: {kind: ClusterProviderConfig}` | 44 | 12% | 132 |
| `<x>IdRef` / `<x>IdSelector` | 33 | 9% | 162 |
| `<x>Selector: matchLabels` | 32 | 8% | 100 |
| `<x>Selector: matchControllerRef` | 24 | 6% | 116 |
| `kind: CompositeConnectionDetails` | 19 | 5% | 19 |
| `kind: ExtraResources` | 14 | 4% | 14 |
| `kind: ClaimConditions` | 4 | 1% | 9 |
| `kind: Context` (write) | 3 | 1% | 5 |
| `compositionRef` / `compositionSelector` (recursion) | 3 | 1% | 4 |
| reads `.desired.*` | 1 | 0% | 1 |

### 2.3 Structural shape (the numbers a graph GUI cares about)

| measure | value |
|---|---|
| template lines total | 51,588 non-blank |
| — pure **static YAML** lines | 31,702 (**61%**) |
| — pure control/assign lines | 10,023 (19%) |
| — value-interpolated lines | 9,863 (19%) |
| value-bearing expressions | 10,103 |
| — bare path/var `{{ $x.y }}` | 7,414 (**73%**) |
| — path + one simple pipe (`quote`/`default`/`b64enc`/`int`) | 871 (9%) |
| — richer (printf/index/dig/toYaml/fn calls) | 1,818 (18%) |
| compositions with a ≥3-assignment **variable prelude** | 179 (**47%**) |
| compositions where `{{if}}` wraps ≥1 **whole resource doc** | 88 (**23%**) — 315 blocks |
| compositions where `{{range}}` wraps ≥1 **whole resource doc** | 94 (**25%**) — 165 blocks |
| compositions with a **nested** `range` inside `range` | 38 (10%) |
| compositions with **no control flow at all** | 108 (**28%**) |
| compositions with **no loop and no `define`** | 227 (**60%**) |
| compositions that read an observed composed resource (any way) | 121 (32%) |
| compositions needing a true escape (`define`/`set`/`mergeOverwrite`/`regexSplit`) | 18 (**5%**) |
| distinct composed **kinds** per composition | median 3, mean 3.6, max 29 |
| inline template length (lines) | median 71, mean 139, p90 314, max 1738 |
| `.observed.resources nil` guard present | 49 (13%) |

### 2.4 Pipeline composition (which other functions sit next to it)

| function | steps |
|---|---:|
| function-go-templating (all naming variants) | 493 |
| function-auto-ready | 257 comps (**67%**) |
| function-patch-and-transform | 66 comps (**17%**) — P&T and go-templating coexist routinely |
| function-environment-configs | 68 comps (**18%**) |
| function-extra-resources | 20 |
| function-sequencer | 9 |
| others (function-shell, function-status-transformer, function-cel-filter, custom in-house fns) | ~20 |
| compositions with **>1 gotemplating step** | 56 (**15%**) |
| ready annotation **and** auto-ready both present | 18 (5%) |

### 2.5 What gets composed

Top composed kinds: `Object` (351, provider-kubernetes), `ClusterProviderConfig` (157), `Secret` (126),
`Release` (84, provider-helm), `Service` (77), `ProviderConfig` (64), `Deployment` (56), `ConfigMap` (51),
`Role` (49), `ExternalSecret` (29), `Namespace` (27), `ServiceAccount` (25), `Bucket` (24), `Job` (23).

Top composed apiVersions: `v1` (289), `kubernetes.crossplane.io/v1alpha2` (203), `ec2.aws.upbound.io/v1beta1` (75),
`kubernetes.m.crossplane.io/v1alpha1` (72), `helm.crossplane.io/v1beta1` (66), `apps/v1` (58),
`iam.aws.upbound.io/v1beta1` (53), `ec2.aws.m.upbound.io/v1beta1` (30), `s3.aws.m.upbound.io/v1beta1` (28).

**26% compose `provider-kubernetes` `Object`s** (yaml-in-yaml), **12% compose Helm `Release`s**, **11% compose native
`apps/v1 Deployment`s.** The `.m.` namespaced migration is visibly underway (29% of compositions touch a `.m.` group).

---

## Part 3 — The named patterns, with real code

### P1. Variable prelude (47%) — *GUI: structural*
```gotemplate
{{ $xr := .observed.composite.resource }}
{{ $xrName := $xr.metadata.name }}
{{ $namespace := $xr.metadata.namespace }}
{{ $params := $xr.spec.parameters }}
{{ $environment := index $.context "apiextensions.crossplane.io/environment" }}
{{ $resourceGroup := index $environment "resourcegroup" }}
```
<https://github.com/platformplane/catalog-crossplane-azure/blob/d45365aa9f3b6a42bdd5a11faed935399f9e22a7/package/azurestorage/v2/composition.yaml>
Aliases: `$xr := .observed.composite.resource` in 21%, a `$spec`/`$params` alias in 26%.
**GUI: structural** — this is the emitter's job entirely. The user never writes it; the generator materialises one
assignment per referenced XRD path and rewrites references. Also fixes footgun F3 (root context inside `range`) for
free, because `$xr` survives dot-rebinding.

### P2. Field mapping with default (56%) — *GUI: structural*
```gotemplate
location: {{ default "Switzerland North" $params.location }}
accountReplicationType: {{ default "LRS" $params.accountReplicationType }}
publicNetworkAccessEnabled: {{ default "false" $params.public }}
```
Both operand orders are in the wild: `X | default "d"` (797 occ) and `default "d" X` (~600 occ). Also
`$v | default "x" | quote` (17 occ) — default-then-quote ordering matters.
**GUI: structural** — `{from: spec.location, to: forProvider.location, default: "…", quote: bool}`.

### P3. Conditional whole-resource inclusion (23%, 315 blocks) — *GUI: structural (node property)*
```gotemplate
{{ if $spec.interoperability.enabled }}
---
apiVersion: cloudplatform.gcp.m.upbound.io/v1beta1
kind: ServiceAccount
metadata:
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: bucket-service-account
…
{{ else }}
---
apiVersion: storage.gcp.m.upbound.io/v1beta1
kind: BucketIAMMember
…
{{ end }}
```
<https://github.com/hbjydev/phoebe/blob/07f2d25a953ad3caee58c9dc2f95884ae0133d4b/kubernetes/apps/crossplane-system/crossplane/gcp/storage-bucket/composition.yaml>
**GUI: structural** — a boolean/enum condition on the node, with `else` modelled as a sibling node bound to the
negation. Common condition shapes (from 2,371 `if`s): `if $v.field` (584 = truthiness/presence),
`if hasKey $xr.spec "k"` (332), `if eq $v "literal"` (152), `if gt (len $v) 0` (51), `if and A B` (49),
`if eq .observed.resources nil` (40).

### P4. Conditional *field* omission (very high; the dominant shape in status blocks) — *GUI: structural*
```gotemplate
  protection:
    state: {{ $protectionState }}
    {{- if ne $protectionValidUntil "" }}
    validUntil: {{ $protectionValidUntil | quote }}
    {{- end }}
    {{- if ne $protectionEvidenceRef "" }}
    evidenceRef: {{ $protectionEvidenceRef | quote }}
    {{- end }}
```
<https://github.com/openkubes/openkubes/blob/15d339517b4b79ff1bee5584ac5e5d60c29ff178/platform/database/postgresql/crossplane/composition.yaml>
**GUI: structural** — `omitEmpty: true` on the mapping. The whitespace-chomp discipline (`{{-` on the guard lines) is
the #1 source of hand-authoring bugs and is exactly what a generator should own.

### P5. `{{- with }}` optional guard (10%) — *GUI: structural, but flag it*
```gotemplate
{{- with .extraResources -}}
{{ $someExtraResources := index . "bucket" }}
{{- range $i, $extraResource := $someExtraResources.items }}
```
<https://github.com/crossplane-contrib/function-go-templating/blob/main/example/extra-resources/composition.yaml>
**GUI: structural** — but emit `if` + explicit alias, not `with`. `with` rebinds `.`, which is the direct cause of
open issue #579 (silent resource drops) and #142.

### P6. Range over an XRD array → N resources (25% of compositions emit resources from a loop) — *GUI: structural*
```gotemplate
{{ range .observed.composite.resource.spec.parameters.databases }}
---
apiVersion: postgresql.sql.crossplane.io/v1alpha1
kind: Database
metadata:
  name: {{ $.observed.composite.resource.spec.id }}-{{ . }}
  annotations:
    crossplane.io/external-name: {{ . }}
    gotemplating.fn.crossplane.io/composition-resource-name: {{ $.observed.composite.resource.spec.id }}-{{ . }}
spec:
  providerConfigRef:
    name: {{ $.observed.composite.resource.spec.id }}
  forProvider: {}
{{ end }}
```
<https://github.com/anistajouri/crossplane-tutorial/blob/9af11a0b7c0fcc6570929eed0130733c12e39bef/compositions/sql-v10/google.yaml>
**GUI: structural** — `forEach: spec.parameters.databases`, item alias, and a **name template** (the per-item
resource-name annotation is mandatory and must be unique — `printf`/`print`-built names appear in 9%).

### P7. Range over a map → repeated *fields* (tags) — *GUI: structural*
```gotemplate
    tags:
      Name: {{ $.observed.composite.resource.spec.name }}
    {{ range $key, $value := .observed.composite.resource.spec.tags }}
      {{ $key }}: {{ $value }}
    {{ end }}
```
<https://github.com/oopsmyops/crossplane-aws-demo/blob/0bff5a850f1a462966c21de34cb1e3568d6c00de/4-compositions/networking-composition.yaml>
**GUI: structural** — `mapMerge` / `passthroughMap` mapping type. 23% of compositions use the two-variable range form
and the overwhelming majority of those are tags/labels/config maps.

### P8. Range over a static list to fan out identical resources — *GUI: structural*
```gotemplate
{{ range $service := list "blob" "file" "queue" "table" }}
---
apiVersion: network.azure.m.upbound.io/v1beta1
kind: PrivateEndpoint
metadata:
  name: {{ $xrName }}-{{ $service }}
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: endpoint{{ $service }}
```
<https://github.com/platformplane/catalog-crossplane-azure/blob/d45365aa9f3b6a42bdd5a11faed935399f9e22a7/package/azurestorage/v2/composition.yaml>
**GUI: structural** — `forEach: literal list`.

### P9. Dict lookup table / t-shirt sizing (17%) — *GUI: structural*
```gotemplate
{{- $sizeResourceMap := dict "xs" (dict "cpuReq" "25m" "cpuLim" "100m" "memReq" "32Mi" "memLim" "64Mi")
      "sm" (dict "cpuReq" "50m" …) "md" (…) "lg" (…) }}
{{- $sizeKey := $xr.spec.parameters.size | default "sm" }}
{{- $sizeResources := get $sizeResourceMap $sizeKey | default (get $sizeResourceMap "sm") }}
```
<https://github.com/cujarrett/homelab/blob/5ab10ce6af91dafd8a77e47abfafdc84bead72a7/platform/api/composition.yaml>
**GUI: structural** — a `valueMap` node: `{key: spec.size, table: {...}, fallback: "sm"}`. The variant that mutates a
prelude variable inside `if` is the same concept, and appears at similar frequency:
```gotemplate
{{- $instances := 1 }}{{- $storageSize := "5Gi" }}{{- $retention := "7d" }}
{{- if eq $spec.availability.mode "ha" }}{{- $instances = 3 }}{{- end }}
{{- if $isProduction }}{{- $storageSize = "20Gi" }}{{- $retention = "30d" }}{{- end }}
```
(openkubes, same file). **Reassignment appears in 22% of compositions, 947 occurrences** — model it as
"profile/preset" rows, not as raw template.

### P10. Reference triad (56% `Ref`, 8% `matchLabels`, 6% `matchControllerRef`) — *GUI: structural*
```gotemplate
spec:
  forProvider:
    bucketSelector:
      matchControllerRef: true
    member: {{ printf "serviceAccount:sa-phb-%.6s-%.6s-s3@%s.iam.gserviceaccount.com" $namespace $name $projectID | quote }}
  providerConfigRef:
    kind: ClusterProviderConfig
    name: gcp-storage
```
(hbjydev/phoebe, above). Note **`providerConfigRef.kind: ClusterProviderConfig`** — the v2 form, in 12% of the corpus
and rising. **GUI: structural** — an edge between two nodes materialising `xSelector.matchControllerRef: true` (or
`xRef.name` from the other node's computed name), plus a per-node `providerConfigRef {kind,name}` property.

### P11. Reading an observed composed resource (32%) — *GUI: structural (an edge), with a path picker*
Three interchangeable spellings, all present:
```gotemplate
{{ ( index $.observed.resources "sample-access-key-0" ).connectionDetails.username }}       # index    (41%)
{{- $accountName := dig "resources" "account" "resource" "metadata" "annotations" "crossplane.io/external-name" "" .observed }}   # dig+default (20%)
{{ $flexServer := getComposedResource . "flexServer" }}{{ get $flexServer.status "id" }}    # helper   (7%)
```
Guarded by `{{ if eq $.observed.resources nil }}data: {}{{ else }}…{{ end }}` in 13%.
**GUI: structural** — a *data edge* `nodeA.status.atProvider.id → nodeB.spec.forProvider.x`; the emitter picks `dig`
with a default (safest) and adds the nil guard automatically.

### P12. Connection-secret assembly (12% emit a Secret; 5% still use CompositeConnectionDetails) — *GUI: structural*
```gotemplate
{{ if eq $.observed.resources nil }}
data: {}
{{ else }}
{{- $accountConnection := dig "resources" "account" "connectionDetails" dict .observed }}
{{- $password := index $accountConnection "attribute.primary_access_key" }}
{{- if $accountName }}
data:
  blob-host: {{ printf "%s.blob.core.windows.net" $accountName | b64enc }}
  username: {{ $accountName | b64enc }}
  {{ if $password }}
  password: {{ $password }}
  {{ end }}
{{- else }}
data: {}
{{- end }}
{{ end }}
```
(platformplane azurestorage v2). **GUI: structural** — a dedicated "connection secret" node: list of
`{key, source: node.connectionDetails.X | literal | expr, alreadyB64: bool}`. Note the b64 asymmetry the README warns
about: values lifted from `connectionDetails` are *already* base64; literals need `| b64enc`.

### P13. Composite status derivation (28%) — *GUI: structural*
```gotemplate
---
apiVersion: platform.openkubes.ai/v1alpha1
kind: Database
status:
  evidence:
    operational:
      state: {{ $operationalState }}
      reason: {{ $operationalReason }}
      evidenceRef: {{ printf "Cluster/%s/%s" $namespace $dbName | quote }}
```
(openkubes). Remember the fn.go rule: same apiVersion+kind, **no** resource-name annotation ⇒ status merge.
**GUI: structural** — a "status" pseudo-node whose fields are bound to observed paths.

### P14. Readiness override (12%; hardcoded True in 11%) — *GUI: structural*
```gotemplate
metadata:
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: mesh-rules
    gotemplating.fn.crossplane.io/ready: "True"
```
(cujarrett/homelab, 218 occurrences across 43 compositions). The derived form the user already writes is rare in the
wild but supported:
```gotemplate
gotemplating.fn.crossplane.io/ready: {{ ( getResourceCondition "Ready" $resource).Status }}
```
— and is **broken** for `Unknown` (issue #461: k8s `Unknown` ≠ the function's `Unspecified`; fn.go coerces only via
the explicit branch and rejects anything else: `must be True, False, or Unspecified`).
**GUI: structural** — a per-node `readiness: auto | always | never | fromField(path, op, value)`; emit the mapped
string, never the raw condition status.

### P15. ExtraResources / required resources (4%) — *GUI: structural*
```gotemplate
apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
kind: ExtraResources
requirements:
  service-project:
    apiVersion: infra.hayden.moe/v1alpha1
    kind: ServiceProject
    matchName: {{ $spec.serviceProjectRef.name | quote }}
    namespace: {{ $namespace | quote }}
{{ if not $projectID }}
---
apiVersion: infra.hayden.moe/v1alpha1
kind: StorageBucket
metadata:
  annotations:
    gotemplating.fn.crossplane.io/ready: "False"
{{ end }}
```
(hbjydev/phoebe). Note the **two-phase idiom**: declare the requirement, and if the lookup has not resolved yet, emit
*only* an XR status/ready-False doc so nothing is composed on the first pass.
**GUI: structural** — a "lookup" input node feeding other nodes, with an implicit gate.

### P16. Context / EnvironmentConfig reads (18% / 13%) — *GUI: structural*
```gotemplate
{{ $envCtx := index .context "apiextensions.crossplane.io/environment" | default dict }}
{{ $awsAccountId := index $envCtx "awsAccountId" | default "" }}
```
(cujarrett/homelab). **GUI: structural** — a second input source alongside the XR, same field-mapping machinery.

### P17. ClaimConditions (1%) — *GUI: structural*
```gotemplate
apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
kind: ClaimConditions
conditions:
  - type: UnifiNetworkMatch
    status: {{ if $ok }}"True"{{ else }}"False"{{ end }}
    reason: {{ if $ok }}Matches{{ else if not $networkFound }}NetworkNotFound{{ else }}Mismatch{{ end }}
    message: {{ if $ok }}"…"{{ else }}{{ printf "%q" $problem }}{{ end }}
    target: CompositeAndClaim
```
<https://github.com/estenrye/flux-platform-src/blob/51426485942bb9048fa619528b8dbb5023b54764/applications/crossplane-resources/xnetworksegment/composition.yaml>
**GUI: structural** — a condition node with `{type, when: expr, trueReason/message, falseReason/message, target}`.
(Rare, but cheap, and it is the only user-facing error channel in v2 with no claim.) `Healthy`/`Ready`/`Synced` are
rejected by the function.

### P18. Validation via `fail` (5%) — *GUI: structural*
```gotemplate
{{- if not (hasKey $store "endpointURL") }}
{{- fail (printf "no reviewed backup store is registered for cluster %q: refusing to compose a Database with no protection destination." $provider) }}
{{- end }}
```
(openkubes). **GUI: structural** — a "precondition" list on the composition.

### P19. Nested templates `define`/`include` (2%) — *GUI: RAW*
Only 6 compositions. Upstream's own example concedes the indentation problem. **Escape hatch.**

### P20. Recursive/meta template engines (5%) — *GUI: RAW, whole-step*
See Complex #2 below.

---

## Part 4 — The 5 most complex real compositions

### C1. `platformplane/catalog-crossplane-azure` — Azure Storage v2 (102 template lines, 16 techniques)
<https://github.com/platformplane/catalog-crossplane-azure/blob/d45365aa9f3b6a42bdd5a11faed935399f9e22a7/package/azurestorage/v2/composition.yaml>

Best single specimen in the corpus for your target stack: v2 `.m.upbound.io` groups, EnvironmentConfig via context,
deterministic external names, tag propagation, list fan-out, connection-secret assembly, and `function-sequencer`
ordering.

```gotemplate
{{ $postfix := trunc 6 (sha256sum (printf "%s" $xr.metadata.uid)) }}
{{ $storageAccountExternalName := printf "%s%s" (trunc 18 (regexReplaceAll "[^a-z0-9]" ($xrName | lower) "")) $postfix }}
{{ $networkResourceGroupID := printf "/subscriptions/%s/resourceGroups/%s" (index $environment "network-subscription") (index $environment "network-resourcegroup") }}
{{ $commonTags := dict "source" "crossplane" "xr-name" $xrName "xr-kind" $xr.kind "xr-api-version" $xr.apiVersion "xr-namespace" $namespace "composite" $xr.metadata.uid }}
…
    tags:
{{ $commonTags | toYaml | nindent 18 }}
```

- **Structural:** the prelude, every `default`-ed forProvider field, `crossplane.io/external-name`, the `range … list
  "blob" "file" "queue" "table"` fan-out with per-item resource names, the `$commonTags` dict → `toYaml|nindent`
  passthrough (a "map merge into field" mapping), the whole connection Secret, and the `function-sequencer` step
  (which is literally a DAG — draw it from the graph's edges).
- **Raw:** the *name-mangling expression* `trunc 18 (regexReplaceAll "[^a-z0-9]" (lower name)) + trunc 6 (sha256sum uid)`.
  This is a per-field `rawTemplate` — every provider has its own naming constraint and you cannot enumerate them.
  The `index $accountConnection "attribute.primary_access_key"` key names are also provider-specific literals: model
  as a string, not as a schema-derived path.

### C2. `livewyer-ops/crossplane-configuration-aws-elemental` — Workflow (a generic MR engine)
<https://github.com/livewyer-ops/crossplane-configuration-aws-elemental/blob/f7b436fa2de079f1a8d1be00095abf66073236ab/apis/workflow/composition.yaml>

A **recursive Go-template interpreter** that renders arbitrary managed resources described in the XR spec, wiring
dependencies by dotted path:

```gotemplate
{{- define "setNestedValue" -}}
  {{- $parts := regexSplit "\\." $path -1 -}}
  {{- if eq (len $parts) 1 -}}
    …{{- $_ := set $dict (first $parts) (append $valueList $value) -}}
  {{- else -}}
    {{- template "setNestedValue" (list (index $dict $firstKey) (join "." (rest $parts)) $pathType $value $subKey $subKeyMerge) -}}
  {{- end -}}
{{- end -}}
…
{{- range $step := $.observed.composite.resource.spec.steps }}
{{- range $resource := $step.resources }}
{{- $resourceId := regexReplaceAll "(.*)/(.*)" $resource.spec.apiVersion (printf "%s.${1}/%s" ($resource.spec.kind | lower) $resource.name) }}
---
apiVersion: {{ $resource.spec.apiVersion }}
kind: {{ $resource.spec.kind }}
…
{{- range $depend := (default list $resource.dependsOn) }}
{{- $dependResource := dig "resources" $dependResourceId "resource" dict $.observed }}
{{- $dependResourceValue := include "digNestedPath" (list $depend.source.key $dependResource) -}}
{{- template "setNestedValue" (list $parameters $depend.key $depend.type $dependResourceValue …) }}
{{- end }}
{{ toYaml $parameters | indent 4 }}
```

- **Structural:** essentially nothing. The composed `apiVersion`/`kind` are *data*, not schema.
- **Raw:** the entire step. **This is the canonical case for a whole-step `rawTemplate`, not a per-field one** — your
  DSL needs both granularities. It is also a warning: people build meta-compositions when the authoring tool is too
  rigid; compositionfactory should make the structural path good enough that nobody writes this.

### C3. `openkubes/openkubes` — PostgreSQL (849 template lines, 15 techniques)
<https://github.com/openkubes/openkubes/blob/15d339517b4b79ff1bee5584ac5e5d60c29ff178/platform/database/postgresql/crossplane/composition.yaml>

Preset profiles, an in-composition policy registry, hard validation, provider-kubernetes yaml-in-yaml, and an
elaborate derived status.

```gotemplate
{{- $claimName := index $xr.metadata.labels "crossplane.io/claim-name" | default $xr.metadata.name }}
{{- if gt (len $dbName) 52 }}
{{- $dbName = printf "%s-%s" (trunc 43 $dbName | trimSuffix "-" | trimSuffix ".") (sha256sum $dbName | trunc 8) }}
{{- end }}
{{- $isProduction := eq $policy "production" }}
{{- $instances := 1 }}{{- $storageSize := "5Gi" }}{{- $retention := "7d" }}
{{- if eq $spec.availability.mode "ha" }}{{- $instances = 3 }}{{- end }}
{{- if $isProduction }}{{- $storageSize = "20Gi" }}{{- $retention = "30d" }}{{- end }}
{{- $backupStores := dict "ok-robotics" (dict "bucket" "ok-db-backups" "endpointURL" "https://minio.minio.svc:9000" "endpointCA" "minio-backup-store-ca") }}
{{- if not (hasKey $backupStores $provider) }}{{- fail (printf "no reviewed backup store is registered for cluster %q…" $provider) }}{{- end }}
{{- $clusterManifest := dig "status" "atProvider" "manifest" (dict) $clusterObject }}
…
                    instances: {{ ternary 2 1 $isProduction }}
```

- **Structural:** the profile/preset table (`policyRef` × `availability.mode` → instances/storage/retention), the
  `$backupStores` registry (a `valueMap` keyed on `clusterRef`), the `fail` preconditions, every provider-kubernetes
  `Object` wrapper (`spec.forProvider.manifest` is just a nested node — the GUI should render CNPG `Cluster`,
  `ScheduledBackup`, `Backup`, `Pooler` as child nodes), the `{{- if $spec.connectivity.pooling.enabled }}` gate, and
  the ~60 `{{- if ne $x "" }}` status field guards.
- **Raw:** the `canonicalRFC3339` `define` block (regex + `toDate`/`date` round-trip validation) and the ~120 lines of
  timestamp/freshness arithmetic that derive `$protectionState`. That is *business logic*, not composition — a
  per-node `rawPrelude` escape (raw template that runs before the node and binds variables) would let the rest stay
  structural. **Design note: your escape hatch needs a "raw prelude" flavour, not just "raw field value".**

### C4. `hbjydev/phoebe` — GCP StorageBucket (132 lines, 13 techniques)
<https://github.com/hbjydev/phoebe/blob/07f2d25a953ad3caee58c9dc2f95884ae0133d4b/kubernetes/apps/crossplane-system/crossplane/gcp/storage-bucket/composition.yaml>

The cleanest v2-native example: namespaced XR, `.m.` MR groups, `ClusterProviderConfig` refs, `ExtraResources`
cross-XR lookup, an if/else that swaps whole subtrees of resources, and a Secret built from an observed HMAC key.

```gotemplate
{{- $projectID := "" -}}
{{- with .extraResources -}}
{{- range $project := (index . "service-project").items -}}
{{- $projectID = default "" $project.resource.status.projectId -}}
{{- end -}}{{- end -}}
…
{{- if ne $.observed.resources nil }}
{{- $hmac := index $.observed.resources "bucket-service-account-hmac-key" }}
{{- if and $hmac $hmac.connectionDetails }}
---
apiVersion: v1
kind: Secret
data:
  AWS_ACCESS_KEY_ID: {{ $hmac.resource.status.atProvider.accessId | b64enc | quote }}
  AWS_SECRET_ACCESS_KEY: {{ index $hmac.connectionDetails "attribute.secret" | quote }}
```

- **Structural:** everything except the two `printf` identity strings. The `if $projectID` / `else` split is exactly
  a graph "branch" and `interoperability.enabled` is a node-inclusion toggle; the "not resolved yet ⇒ emit only
  `ready: False` on the XR" arm is a generatable *pattern*, not user code.
- **Raw:** `printf "principal://iam.googleapis.com/projects/754336396991/locations/global/workloadIdentityPools/kube-talos-phoebe/subject/system:serviceaccount:%s:%s"` — a hardcoded external identity URI. Per-field raw.

### C5. `cujarrett/homelab` — platform/api (1,233 lines, 17 techniques, the corpus's densest)
<https://github.com/cujarrett/homelab/blob/5ab10ce6af91dafd8a77e47abfafdc84bead72a7/platform/api/composition.yaml>

An entire application platform in one template: Deployment + Service + Istio + Entra/Azure AD + AWS IRSA + cert-manager,
driven by a rich XR spec with `provides`/`consumes` interface lists.

```gotemplate
{{- $trustPolicy := dict "Version" "2012-10-17" "Statement" (list (dict "Effect" "Allow" "Principal" (dict "Federated" $oidcProviderArn) "Action" "sts:AssumeRoleWithWebIdentity" "Condition" (dict "StringEquals" (dict (printf "%s:aud" $oidcIssuerHost) "sts.amazonaws.com" (printf "%s:sub" $oidcIssuerHost) (printf "spiffe://homelab.local/ns/%s/sa/%s" $ns $name))))) | toJson }}
{{- $appRoles := list }}{{- $delegatedScopes := list }}
{{- range $iface := $provides }}
{{- if eq $iface.auth "user" }}{{- $delegatedScopes = append $delegatedScopes $iface }}
{{- else if eq $iface.auth "workload" }}{{- $appRoles = append $appRoles $iface }}{{- end }}
{{- end }}
{{- $entraNeeded := or (gt (len $appRoles) 0) (gt (len $delegatedScopes) 0) }}
{{- range $c := $consumes }}{{- if or $c.app $c.entraApp }}{{- $entraNeeded = true }}{{- end }}{{- end }}
{{- $sizeResourceMap := dict "xs" (dict "cpuReq" "25m" …) … }}
{{- $sizeResources := get $sizeResourceMap $sizeKey | default (get $sizeResourceMap "sm") }}
```

- **Structural:** ~20 defaulted scalar mappings; the size map; every `gotemplating.fn.crossplane.io/ready: "True"`
  override (18 of them); the Deployment/Service/ServiceEntry/Certificate nodes; `range $pr := $proxies` fan-out to
  ServiceEntries.
- **Raw:** the **list-partitioning prelude** (`range $provides` → append into two lists, then derive `$entraNeeded`)
  and the `$trustPolicy` JSON document construction. Both are "compute a derived collection from an XR array" — a
  shape a node graph cannot draw without becoming a programming language. **This is the strongest argument for
  `rawPrelude` returning named variables that downstream structural mappings can reference by name.**

---

## Part 5 — Anti-patterns and footguns the community complains about

**F1. `missingkey=default` silently yields `<no value>`.** The default option is `missingkey=default`
(README; overridable via `--default-options` / `FUNCTION_GO_TEMPLATING_DEFAULT_OPTIONS`). Only **3/381** compositions
set `missingkey=error`. Consequence: a typo'd XR path renders the literal string `<no value>` into the MR spec and the
provider takes it. → *Generator should default to `options: [missingkey=error]` and emit `dig`/`default` for every
optional path.*

**F2. Missing fields on first reconcile ⇒ silently dropped resources.** Open issue #579:
> "On the first reconcile after XR creation, `observed.composite.resource.spec` may not contain all user-specified
> fields. The `desired.composite.resource.spec` always has the full spec." — resources inside `{{- with .field }}`
> are never created. <https://github.com/crossplane-contrib/function-go-templating/issues/579>
→ *Another reason to emit `if` with explicit aliases instead of `with`, and to consider reading from `.desired`.*

**F3. `range` rebinds `.`, breaking every helper.** Closed issue #142 ("Get the root context within loops"):
`getComposedResource` etc. take the request as the first argument, so inside a loop you must write `$` not `.`.
Also #114. <https://github.com/crossplane-contrib/function-go-templating/issues/142>
→ *A prelude that binds `$xr`/`$root` at the top eliminates this class of bug entirely.*

**F4. Indexing untyped nil.** Open issue #78:
> `at <index .observed.resources.pod.resource.status.atProvider.manifest.status.hostIPs 0>: error calling index: index of untyped nil`
<https://github.com/crossplane-contrib/function-go-templating/issues/78> — a template error is fatal for the whole
step, so one un-guarded observed read breaks every resource in the composition.
→ *Always emit `dig …  <default>` for observed reads; never a bare chained path.*

**F5. `getResourceCondition` returns `Unknown`, the ready annotation demands `Unspecified`.** Closed issue #461; fn.go
validates `must be True, False, or Unspecified`. Wiring the two together naively fails.

**F6. Sprig randomness is re-evaluated on every reference.** Closed issue #471 —
`{{ $rand1 := randAlphaNum 5 }}` then `{{ $rand1 }}` twice gives two different values in some shapes.
→ *Never generate random values; derive from `metadata.uid` via `sha256sum`/`trunc`, which is what the good
compositions in the corpus do.*

**F7. Two incompatible resource-name annotations.** Open issue #41 — P&T uses
`crossplane.io/composition-resource-name`, this function uses `gotemplating.fn.crossplane.io/composition-resource-name`.
17% of the corpus runs both functions in one pipeline, so both spellings coexist in one file.

**F8. provider-kubernetes `Object` readiness doesn't propagate.** Open issue #99 — which is *precisely* why 11% of the
corpus hardcodes `gotemplating.fn.crossplane.io/ready: "True"` (218 occurrences).

**F9. ExtraResources behave differently under `crossplane render` vs a live cluster.** Open issues #536, #501
(`matchName` fails where `matchLabels` works). → *Any generated ExtraResources block should be flagged as
"verify in-cluster".*

**F10. YAML-in-Go-template indentation.** The upstream `include` example ships the comment
*"## weird indentation to make it work"* and *"without include, you must define a template per indentation setting"*.
`toYaml | nindent N` with a hand-counted `N` appears 58 times. → *The single biggest ergonomic win a generator has:
it knows N.*

**F11. Structural drift as a design smell.** Open issue #513 ("Templating in the Style of a RunFunctionResponse")
argues the YAML-document-with-magic-annotations output format is itself the problem. Worth watching: if that lands,
the emitter target changes.

---

## Appendix — provenance

Corpus repos with ≥3 compositions: platformplane/catalog-crossplane (28), crossplane-contrib/crossplane-diff (13),
0xayf/homelab-idp (12), livewyer-ops/gardener-allotment (12), stuttgart-things/crossplane (11+5),
netclab/netclab-xp (11), crossplane-contrib/function-go-templating (11+9), cdelgehier/tf2crossplane (9),
gentian-org/gentian-os (8), vrabbi/crossplane-as-cloud-api (8), QuantumDancer/idp-crossplane-compositions (8),
cujarrett/homelab (7), novelcore/function-kubecore-schema-registry (6),
livewyer-ops/crossplane-configuration-aws-elemental (6+5), cujarrett/learning-krm (5),
shlapolosa/health-service-idp (5), repldriven/queenswood (5), loafoe/crossplane-compositions (5),
vfarcic/crossplane-tutorial (5), crh225/ARMServicePortal (5), anistajouri/crossplane-tutorial (4),
upbound/composition-testing (4), openkubes/openkubes (4), estenrye/flux-platform-src (4), jherreros/shoulders (4),
nimishmehta8779/idp-gitops (3), oopsmyops/crossplane-aws-demo (3), cr7258/hands-on-lab (3),
giantswarm/crossplane-examples (3), asanexample/platform (3), gentian-org/gentian-apps (3), tomernos/pavedplane (3),
infralovers/training-crossplane-examples (3), SimonTheLeg/crossplane-and-kcp-demo (3), K-FOSS/CoRE-Backplane (3),
deliveryhero/asya (3), homystack/homy-stack (3), datametal/k8s-crossplane-demo (3),
twplatformlabs/psk-platform-ext-crossplane (3), crossplane-contrib/xprin (3), TeraSky-OSS/declarative-conversion-operator (3),
konflux-ci/crossplane-control-plane (2), liferay/liferay-portal (10 files), hops-ops/* (~100 files, mostly XRDs).

**Caveats.** (1) GitHub code search caps at 1,000 results per query and ranks by relevance, so the corpus over-samples
public homelab/IDP repos and under-samples private enterprise platforms. (2) A handful of repos are clearly
AI-assisted or tutorial code (cujarrett/learning-krm, several `*-tutorial` repos); I kept them because the *idioms*
they use are copied from upstream docs and therefore representative of what new users write. (3) Percentages are
per-composition presence, not per-line weight — a technique at 10% presence can still dominate line count in the
compositions that use it (`toYaml|nindent` is the clearest case).
