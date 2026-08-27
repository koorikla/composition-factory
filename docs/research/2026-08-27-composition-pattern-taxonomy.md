# Composition Pattern Taxonomy & DSL Coverage Matrix

**Date:** 2026-08-27
**Purpose:** define the blueprint DSL of **compositionfactory**. This document is the contract:
§3 (the coverage matrix) and §4 (the blueprint sketch) are the parts to argue with. Everything
else is evidence for them.

**Notation used throughout**

| Tag | Meaning |
|---|---|
| **[C]** | Frequency **counted** by a brief (parsed or grepped, denominator stated) |
| **[I]** | **Impression** — a brief's qualitative claim, no denominator |
| **[V]** | Verified by running it (from the grounding doc) |
| **UNRESOLVED** | Two briefs disagree; recorded, not reconciled |
| **T1 / T2 / T3** | must be first-class in v1 / DSL-modelled, lower priority / rawTemplate escape only |

In §3 the "Blueprint DSL representation" column is the YAML the **user writes in the blueprint**.
The YAML we **emit** is §4.

---

## 1. Corpus

### 1.1 Briefs on disk

All six requested briefs exist and were read in full. Nothing is missing.

| Brief | Lines | Corpus it built |
|---|---:|---|
| `raw/cs-upbound-refs.md` | 1235 | `github.com/upbound` enumerated via API (210 repos) → 73 `configuration-*`/`platform-ref-*`, **71 live repos shallow-cloned**; 120 Composition docs, 117 XRDs, 34 KCL fns, 80 Python fn files, 1 TS, 1 gotmpl |
| `raw/cs-gotemplating-corpus.md` | 849 | GitHub code search (4 queries) → 900 files + 50 full clones → **381 distinct Compositions, ~127 repos, ~102 orgs**, deduped by whitespace-normalised content hash; 347 carry inline templates (53,069 template lines / 21,036 actions). Plus upstream `function-go-templating` @ `5d48403`, all 24 examples, `fn.go`, `function_maps.go` read in full |
| `raw/cs-v2-native.md` | 1017 | **68 cloned repos, 680 composition YAML files, 508 XRD docs**; plus Crossplane v2.4 source @ `0e4f8c1d`, function-auto-ready @ `5383ea04`, crossplane/docs v2.2–v2.4 |
| `raw/cs-community-platforms.md` | 725 | 26 candidate repos → **21 kept (≥2 Compositions each), 332 Composition files** parsed with Python |
| `raw/cs-other-functions.md` | 807 | **1,409 YAML files** = 600 harvested GitHub files (356 contain Compositions, 364 Composition docs, ~250 repos) + 13 cloned repos; 1,899 patches and 426 transforms parsed structurally; plus authenticated GitHub code-search file counts |
| `raw/cs-gcp-portability.md` | 795 | `upbound/provider-gcp-*` **v3.0.1, all 80 subpackages → 815 CRDs**; AWS control corpus `provider-aws-*` v2.7.1, 16 subpackages → 563 CRDs. Shape-first scan of 405 GCP + 279 AWS namespaced MRs. No cluster touched |

Grounding doc `2026-08-27-crossplane-generator-research.md` §1–§2 (lines 1–553) was read for
constraints; its own missing brief (`schema-sourcing.md`) is unrelated to this document — except
that `raw/schema-sourcing.md` **now exists on disk** (581 lines, written 23:02) and was *not* read
here because it was not in scope.

### 1.2 Honest coverage statement

**The corpora overlap heavily and I cannot deduplicate them.** Repos appearing in ≥2 briefs include
`cujarrett/homelab`, `openkubes/openkubes`, `platformplane/catalog-crossplane*`,
`livewyer-ops/*`, `tomernos/pavedplane`, `stuttgart-things/crossplane`, `estenrye/flux-platform-src`,
`deliveryhero/asya`, `back-stack/kubecon-na-2025`, `upbound/*`, `awslabs/crossplane-on-eks`,
`0xayf/homelab-idp`, `shlapolosa/health-service-idp`, `crossplane-contrib/*` example dirs.

Naively summing gives ~2,900 Composition documents. Only `cs-gotemplating-corpus` deduped by
content hash, and only within itself. **A defensible estimate of the union is 1,200–1,600 distinct
Composition documents across ~350–450 distinct repos.** Percentages from different briefs are
therefore *not* comparable to each other and must always be read with their denominator attached.
Every percentage below carries its denominator.

**Known biases, stated by the briefs themselves:**

- `cs-gotemplating` over-samples public homelab/IDP repos and under-samples private enterprise
  platforms (GitHub code search caps at 1,000 results and ranks by relevance). Some corpus repos
  are AI-assisted or tutorial code.
- `cs-other-functions`'s 600-file harvest is **99.2% go-templating** — it is unusable for
  *relative popularity between functions*; that brief uses GitHub code-search file counts instead.
- `cs-upbound` is a single vendor. It is 96% KCL/Python and only 4% go-templating, so its *idioms*
  had to be translated. Treat it as evidence about **what platform engineers express**, not about
  **how they spell it in Go templates**.
- `cs-gcp-portability` is a *provider schema* corpus, not a composition corpus. It says nothing
  about authoring style, only about what the reference-inference layer must handle.

**What is not in any corpus:** private enterprise platforms; anything using
`ops.crossplane.io` (0/680 [C]); anything using Crossplane v2 `requirements.requiredResources`
(0/1,409 [C] — it is too new to have been published).

---

## 2. The pattern catalogue

45 distinct authoring patterns. Each: name, real excerpt + URL, frequency with denominator, GUI verdict.
Verdicts are **structural** (the GUI owns it, user never sees a template), **structural-with-effort**
(expressible, but needs a real editor widget or a non-trivial emitter), **raw-escape-only**.

### A. Value & expression patterns

#### A-1. Variable prelude
```gotemplate
{{ $xr := .observed.composite.resource }}
{{ $xrName := $xr.metadata.name }}
{{ $namespace := $xr.metadata.namespace }}
{{ $params := $xr.spec.parameters }}
{{ $environment := index $.context "apiextensions.crossplane.io/environment" }}
```
<https://github.com/platformplane/catalog-crossplane-azure/blob/d45365aa9f3b6a42bdd5a11faed935399f9e22a7/package/azurestorage/v2/composition.yaml>

**Frequency:** 179/381 compositions open with a ≥3-assignment prelude (47%) [C]; 318/381 (83%) read
`.observed.composite.*` at all, 3,302 occurrences [C]; 1,189 variable assignments across 78 templated
compositions in the community corpus (~15/composition) [C].
**Verdict: structural.** Pure emitter output — the user never writes it. It also fixes footgun F3
(`range` rebinds `.`) for free.

#### A-2. Field mapping with `default`
```gotemplate
location: {{ default "Switzerland North" $params.location }}
accountReplicationType: {{ default "LRS" $params.accountReplicationType }}
```
(same file). **Frequency:** 215/381 (56%), 1,530 occurrences [C]; 369 uses / 55 files in the community
corpus [C]. Both operand orders in the wild (`X | default "d"` 797, `default "d" X` ~600) [C].
Of 10,103 value-bearing expressions, **73% are a bare path/var and 9% are a path plus one trivial
pipe** [C].
**Verdict: structural.** `{from, to, default, quote}` covers ~82% of all expressions.

#### A-3. Type-aware quoting
| XR spec (string) | Unquoted emit | With `\| quote` |
|---|---|---|
| `"1.10"` | `1.1` — **data loss** | `"1.10"` |
| `"on"` / `"yes"` | `true` | `"on"` / `"yes"` |
| `5` (integer) | `5` correct | `"5"` — **wrong type** |

**Frequency:** `| quote` in 101/381 (27%), 855 occurrences [C]. Non-string annotation values are a
**fatal** render error, not coerced [V]. Upjet emits `type: number` for integral fields, 726 `number`
vs 204 `integer` in EC2 [V].
**Verdict: structural.** Rule: quote iff schema type is `string`; bare for numeric/boolean; quote
every annotation and label value unconditionally.

#### A-4. Conditional field omission
```gotemplate
  protection:
    state: {{ $protectionState }}
    {{- if ne $protectionValidUntil "" }}
    validUntil: {{ $protectionValidUntil | quote }}
    {{- end }}
```
<https://github.com/openkubes/openkubes/blob/15d339517b4b79ff1bee5584ac5e5d60c29ff178/platform/database/postgresql/crossplane/composition.yaml>

**Frequency:** "the dominant shape in status blocks" [I]; `hasKey` used 376 times across 25
compositions as the presence test [C]; `{{ with }}` optional guard 40/381 (10%), 161 occurrences [C].
The whitespace-chomp discipline (`{{-` on guard lines) is named the #1 hand-authoring bug source [I].
**Verdict: structural** — `omitEmpty: true` on the mapping. Emit `if` + explicit alias, never `with`
(issue #579: `with` silently drops whole resources on first reconcile).

#### A-5. Dict lookup table / t-shirt sizing
```gotemplate
{{- $sizeResourceMap := dict "xs" (dict "cpuReq" "25m" "cpuLim" "100m" "memReq" "32Mi" "memLim" "64Mi")
      "sm" (dict "cpuReq" "50m" ...) "md" (...) "lg" (...) }}
{{- $sizeKey := $xr.spec.parameters.size | default "sm" }}
{{- $sizeResources := get $sizeResourceMap $sizeKey | default (get $sizeResourceMap "sm") }}
```
<https://github.com/cujarrett/homelab/blob/5ab10ce6af91dafd8a77e47abfafdc84bead72a7/platform/api/composition.yaml>

**Frequency:** `dict` in 85/381 (22%), 1,091 occurrences [C]; the *lookup-table* subset 17% [C];
22/332 community files [C]; P&T `type: map` transform 39/426 transforms (9.2%) [C].
platformplane document it as a house rule (version → chart-version tables).
**Verdict: structural** — a `valueMap` node: `{key, table, fallback}`.

#### A-6. Preset/profile via variable reassignment
```gotemplate
{{- $instances := 1 }}{{- $storageSize := "5Gi" }}{{- $retention := "7d" }}
{{- if eq $spec.availability.mode "ha" }}{{- $instances = 3 }}{{- end }}
{{- if $isProduction }}{{- $storageSize = "20Gi" }}{{- $retention = "30d" }}{{- end }}
```
(openkubes, same file). **Frequency:** reassignment `{{ $x = }}` in 83/381 (22%), **947 occurrences** [C].
**Verdict: structural** — it is a two-dimensional `valueMap` (policy × mode → row), not control flow.
Modelling it as "raw template" would be a design error at this frequency.

#### A-7. Derived name with sanitisation / length clamp
```gotemplate
{{- if gt (len $dbName) 52 }}
{{- $dbName = printf "%s-%s" (trunc 43 $dbName | trimSuffix "-" | trimSuffix ".") (sha256sum $dbName | trunc 8) }}
{{- end }}
```
(openkubes). And:
```gotemplate
{{ $postfix := trunc 6 (sha256sum (printf "%s" $xr.metadata.uid)) }}
{{ $storageAccountExternalName := printf "%s%s" (trunc 18 (regexReplaceAll "[^a-z0-9]" ($xrName | lower) "")) $postfix }}
```
(platformplane azurestorage v2). Also `namespace | replace "." "-"` (Konflux),
`_n = "${appName}-${region}-${n}"` (Giant Swarm).

**Frequency:** `printf`/`print` 164/381 (43%), 858 occurrences [C]; 152 printf uses / 58 files in the
community corpus [C]; `sha1sum`/`sha256sum`/`uuidv4` 44/381 (12%) [C]; length-clamp-with-hash observed
independently in ≥3 repos [I].
**Verdict: structural-with-effort.** `{fmt, inputs[], sanitize{lower, replace, regexKeep, maxLength,
hashSuffix}}` covers every observed case, but the sanitiser vocabulary is a real design surface and
the *arbitrary* case (a provider-specific identity URI) falls to A-8.

#### A-8. Hardcoded external identity strings
```gotemplate
printf "principal://iam.googleapis.com/projects/754336396991/locations/global/workloadIdentityPools/kube-talos-phoebe/subject/system:serviceaccount:%s:%s"
```
<https://github.com/hbjydev/phoebe/blob/07f2d25a953ad3caee58c9dc2f95884ae0133d4b/kubernetes/apps/crossplane-system/crossplane/gcp/storage-bucket/composition.yaml>

**Frequency:** present in 2 of the 5 "most complex" compositions [I]; no count.
**Verdict: raw-escape-only** (per-field). Nothing to enumerate — it is a cloud identity format the
tool cannot know.

#### A-9. Map merge / passthrough (tags, labels)
```gotemplate
    tags:
      Name: {{ $.observed.composite.resource.spec.name }}
    {{ range $key, $value := .observed.composite.resource.spec.tags }}
      {{ $key }}: {{ $value }}
    {{ end }}
```
<https://github.com/oopsmyops/crossplane-aws-demo/blob/0bff5a850f1a462966c21de34cb1e3568d6c00de/4-compositions/networking-composition.yaml>
KCL form: `tags = get(oxr,"spec.tags",{}) | {"region": region} | labels` (Giant Swarm).

**Frequency:** two-variable `range $k, $v` in 87/381 (23%) [C], "overwhelmingly tags/labels/configmaps" [I];
tags block in 64/332 community files (19%) [C]; `toYaml` 51/381 (13%), `nindent` 46/381 (12%) [C].
`toYaml | nindent N` with a hand-counted N appears 58 times [C]; upstream's own example ships the
comment *"weird indentation to make it work"*.
**Verdict: structural.** `merge: [sources...]` → `{{ toYaml $tags | nindent N }}`. **The generator
knowing N is named the single biggest ergonomic win available** [I].

### B. Resource-level patterns

#### B-1. Composed-resource-name annotation = node identity
```gotemplate
metadata:
  annotations:
    {{ setResourceNameAnnotation "securitygroup" }}
```
<https://github.com/upbound/configuration-aws-securitygroup/blob/main/functions/xsecuritygroups/compose.yaml.gotmpl>

**Frequency:** raw annotation string in 268/381 (70%), 1,073 occurrences; `setResourceNameAnnotation`
helper in 85/381 (22%), 258 occurrences [C]. 1,800 occurrences / 601 files across the 1,409-file
corpus [C]. 41 hits / 30 of 34 Upbound KCL functions use the KCL analogue [C].
Missing it on a non-XR kind is a **fatal** pipeline error (`fn.go`).
**Verdict: structural.** It **is** the node id. Never expose it as an editable template field. It is
also the universal join key across every function (P&T `resources[].name`, sequencer
`rules[].sequence[]`, cel-filter `filters[].name`, status-transformer `resources[].name`) — **node
identity must be this string and must be stable across regeneration.**

#### B-2. Conditional whole-resource inclusion
```gotemplate
{{ if $spec.interoperability.enabled }}
---
apiVersion: cloudplatform.gcp.m.upbound.io/v1beta1
kind: ServiceAccount
...
{{ else }}
---
apiVersion: storage.gcp.m.upbound.io/v1beta1
kind: BucketIAMMember
...
{{ end }}
```
(hbjydev/phoebe). CEL-flavoured equivalent, `upboundcare/function-conditional-patch-and-transform`:
```yaml
resources:
  - name: XNetworkAWS
    condition: observed.composite.resource.spec.parameters.cloud == "aws"
    base: {apiVersion: aws.platform.upbound.io/v1alpha1, kind: XNetwork}
```
<https://github.com/upbound/platform-ref-multi-k8s/blob/main/apis/composition.yaml>

**Frequency:** `if` in 218/381 (57%), 2,371 occurrences [C]; **88/381 (23%) have an `if` wrapping ≥1
whole resource doc — 315 such blocks** [C]; 73/332 community files have `if` [C];
`function-conditional-patch-and-transform` 14 files GitHub-wide [C]; `function-cel-filter` 19 files [C].
Condition shapes counted across the 2,371 `if`s: truthiness/presence 584, `hasKey` 332,
`eq $v "literal"` 152, `gt (len $v) 0` 51, `and A B` 49, `eq .observed.resources nil` 40 [C].
**Verdict: structural** for the five counted shapes (a path + operator + value builder).
Compound boolean expressions → A-8-style escape.

#### B-3. `forEach` over an XRD array
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
{{ end }}
```
<https://github.com/anistajouri/crossplane-tutorial/blob/9af11a0b7c0fcc6570929eed0130733c12e39bef/compositions/sql-v10/google.yaml>

**Frequency:** `range` in 152/381 (40%), 580 occurrences [C]; **94/381 (25%) have a `range` wrapping
≥1 whole resource doc — 165 blocks** [C]; 26/332 community files [C]; 132 array-typed properties
across 117 Upbound XRDs, 27/34 KCL functions contain a comprehension (123 `for`) [C].

**The strongest single argument in the whole corpus:** `ims-platform-dev/portal-kombat`'s
security-group composition is **2,264 lines of which 2,099 (92%) are 40 hand-expanded "rule slots"**,
with a header comment admitting *"YAML Array Limitation: Uses pre-defined rule slots (0-19) with
Optional policy to skip unused slots."*
<https://github.com/ims-platform-dev/portal-kombat/blob/main/infra-definitions/compositions/network/security-group-standard.yaml>
One loop node collapses it to ~25 lines.
**Verdict: structural** for the container, per-item resource-name and per-item external-name.

#### B-4. Nested loop with computed name **and computed kind**
```gotemplate
{{- range $i, $rule := $spec.parameters.rules }}
  {{- $cidrBlocks := $rule.cidrBlocks | default (list "") }}
  {{- range $j, $cidrBlock := $cidrBlocks }}
---
apiVersion: ec2.aws.upbound.io/v1beta1
kind: SecurityGroup{{ title $rule.type }}Rule
metadata:
  annotations:
    {{ setResourceNameAnnotation (printf "sgrule-%d-%s" $i ($cidrBlock | replace "." "-" | replace "/" "-")) }}
```
<https://github.com/upbound/configuration-aws-securitygroup/blob/main/functions/xsecuritygroups/compose.yaml.gotmpl>

**Frequency:** nested `range` in 38/381 (10%) [C]. Computed **kind**: 1 site [C]. Cartesian product
(`for repo in ... for team in ...`): 1 site [C]
(<https://github.com/upbound/platform-ref-upbound/blob/main/functions/xupboundreposet/main.k#L68>).
**Verdict:** nested container **structural**; computed name **structural-with-effort** (A-7);
**computed kind is raw-escape-only** — a node with a data-derived kind has no schema, so no form,
no validation, no palette entry.

#### B-5. One loop iteration emits N correlated resources
```python
ec2v1beta1.Subnet{ metadata = _metadata("subnet-" + _formatSubnet(s)) | {
    labels = { zone = s.availabilityZone, access = "private" if s.type=="private" else "public" } } ... }
  for s in oxr.spec.parameters.subnets

ec2v1beta1.RouteTableAssociation{ metadata = _metadata("rta-" + _formatSubnet(s))
    spec.forProvider.subnetIdSelector = { matchControllerRef = True
        matchLabels = { access = ..., zone = s.availabilityZone } } }
  for s in oxr.spec.parameters.subnets
```
<https://github.com/upbound/configuration-aws-network/blob/main/functions/network/main.k>

**Frequency:** [I] — not counted, but present in ≥3 independent repos (upbound network, pavedplane
vpc/subnet, upbound securitygroup).
**Verdict: structural**, and it forces C-3 (loop-crossing edges must become label stamp/select).

#### B-6. Native Kubernetes object composed directly (v2)
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: deployment
    {{ if eq (.observed.resources.deployment | getResourceCondition "Available").Status "True" }}
    gotemplating.fn.crossplane.io/ready: "True"
    {{ end }}
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: app
        image: {{ .observed.composite.resource.spec.image }}
```
<https://github.com/crossplane/docs/blob/main/content/v2.4/manifests/get-started/composition/composition-templated-yaml.yaml>

**Frequency:** **114/319 v2 compositions (36%) compose a native K8s apiVersion** [C]. Direct beats
the provider-kubernetes `Object` wrapper for every workload kind: Service 88 vs 11, ConfigMap 75 vs 9,
Secret 65 vs 33, Deployment 56 vs 12 [C]. In the go-templating corpus: `apps/v1` 58 occurrences,
`Deployment` 56, `Service` 77 [C].
Note: Upbound's own 71 repos do this **0 times** — they still wrap in `Object` (6) or `Release` (9) [C].
**Verdict: structural**, and named the single most important node type by `cs-v2-native`.

#### B-7. provider-kubernetes `Object` wrapper (yaml-in-yaml)
**Frequency:** 351 `Object` occurrences — the single most-composed kind in the go-templating corpus [C];
66/680 compositions still reference `kubernetes.crossplane.io` at all [C]; 26% of the go-templating
corpus composes `Object`s [C]. Still dominant for `ServiceAccount` (14 vs 8) and `Job` (8 vs 1) [C],
because a namespaced XR **cannot** compose a cluster-scoped `Namespace`.
**Verdict: structural** — `spec.forProvider.manifest` is a nested node; render the wrapped GVK as a
child node. Two things the emitter must know: the **double hop** (`status.atProvider.manifest.status.*`
not `status.atProvider.*`) and that `Object` readiness does not propagate (open issue #99 — the direct
cause of 11% of the corpus hardcoding `ready: "True"`).

#### B-8. Nested XR (an XR composing another XR)
```gotemplate
{{- if $cacheEnabled }}
---
apiVersion: platform.local.lab/v1alpha1
kind: Cache
metadata:
  name: {{ $name }}-cache
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: cache
spec:
  parameters:
    backend: {{ $cacheBackend }}
{{- end }}
```
<https://github.com/cujarrett/homelab/blob/main/platform/api/composition.yaml>

**Frequency:** **30/680 (4.4%)**, of which 11 target a v2 XRD, across 7 independent platforms [C].
But 104 `models.io.upbound.platform` imports in the Upbound corpus [C] — it is Upbound's *core
architectural move* (leaf Configurations own MRs, platform-refs compose XRs).
**UNRESOLVED (emphasis, not fact):** 4.4% of the general corpus vs "the architecture" in Upbound's.
Both are true; they measure different populations.
**Verdict: structural.** Readiness and RBAC are free (XRs publish a `Ready` condition; the RBAC
manager grants access to all XRs). Namespace inheritance is transitive.

#### B-9. Observe-only managed resource
```yaml
spec:
  managementPolicies: ["Observe"]
  forProvider: { manifest: { apiVersion: kubeconfig.stuttgart-things.com/v1alpha1, kind: RemoteCluster,
                             metadata: { name: <clusterName> } } }
  providerConfigRef: { name: in-cluster, kind: ClusterProviderConfig }
```
<https://github.com/stuttgart-things/crossplane> (community brief §5)
**Frequency:** 32/332 community files (10%) [C]; `managementPolicies` present in 37/332 (11%) [C].
**Verdict: structural** — a distinct node type ("read a foreign object's status without owning it").

### C. Wiring patterns

#### C-1. The provider reference triad
```json
"network":         {"type":"string","description":"Name of VPC network connected with service producers using VPC peering."},
"networkRef":      {"type":"object","description":"Reference to a Network in compute to populate network."},
"networkSelector": {"type":"object","description":"Selector for a Network in compute to populate network."}
```
`connections.servicenetworking.gcp.m.upbound.io`, `xpkg.upbound.io/upbound/provider-gcp-servicenetworking:v3.0.1`;
source <https://github.com/crossplane-contrib/provider-upjet-gcp/tree/v3.0.0/apis/namespaced/servicenetworking>

**Frequency:** **578/578 GCP refs (100%) have a matching Selector; 578/578 selectors have a matching
Ref. Same both ways on AWS: 464/464.** 1,042 refs across both providers [C]. 285/405 GCP MRs and
229/279 AWS MRs carry ≥1 ref [C]. This is the **highest-frequency structural pattern in the entire
provider surface — higher than any go-templating idiom** [C].
**Verdict: structural.** This *is* the graph edge.

#### C-2. Edge → `matchControllerRef: true` (sibling)
```python
ec2v1beta1.InternetGateway{ spec.forProvider.vpcIdSelector = { matchControllerRef = True } }
```
<https://github.com/upbound/configuration-aws-network/blob/main/functions/network/main.k>

**Frequency:** 99 hits / 20 Upbound files [C]; 24/381 go-templating compositions (6%), 116
occurrences [C]; 44/332 community files (13%) [C].
**Verdict: structural** — and it should be the **default edge semantics**.

#### C-3. Edge → `matchLabels` (loop-crossing / role disambiguation)
```gotemplate
{{- range $vpc := $xr.spec.vpcs }}
kind: Network
metadata:
  labels:
    platform.example.org/xr:  {{ $xrName | quote }}
    platform.example.org/vpc: {{ $vpc.name | quote }}
{{- end }}
{{- range $subnet := $xr.spec.subnets }}
kind: Subnetwork
spec:
  forProvider:
    networkSelector:
      matchLabels:
        platform.example.org/xr:  {{ $xrName | quote }}
        platform.example.org/vpc: {{ $subnet.vpcRef | quote }}
{{- end }}
```
<https://github.com/tomernos/pavedplane/blob/main/configuration-gcp/compositions/xnetwork.yaml>
Role form (two Roles, one Cluster picking one):
<https://github.com/upbound/configuration-aws-eks/blob/main/functions/eks/main.k>

**Frequency:** 53 `matchLabels` hits / 19 Upbound files [C]; 32/381 go-templating compositions (8%),
100 occurrences [C]; 60/332 community files use any `*Selector` (18%) [C].
**Verdict: structural.** `matchControllerRef` is **not sufficient once a loop produces >1 of a kind** —
the generator must emit the label-stamp/label-select pair automatically when an edge crosses a loop
boundary. This is a correctness rule, not an ergonomic one.

#### C-4. Edge → `<x>Ref: {name: …}` (explicit name)
**Frequency:** `<x>Ref: {name: …}` in **215/381 go-templating compositions (56%), 972 occurrences** [C].
**UNRESOLVED — emphasis conflict.** `cs-upbound` states cross-resource references are *"~99% `*Selector`,
not templated status paths"* [C, 160 selector occurrences vs a handful of status interpolations],
while `cs-gotemplating` counts `Ref{name}` at 56% vs selectors at 8%/6% [C]. The two are measuring
different things: the go-templating 972 includes `providerConfigRef`, `secretRef`,
`writeConnectionSecretToRef` and native-K8s `backend.service.name`, whereas Upbound's 160 counts only
*cross-managed-resource* wiring. **Design consequence: the DSL must model both and not assume one.**
**Verdict: structural**, but it forces C-5.

#### C-5. Explicit `metadata.name` coupling
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ .observed.composite.resource.metadata.name }}
spec:
  rules:
  - http: {paths: [{backend: {service: {name: {{ .observed.composite.resource.metadata.name }} }}}]}
```
<https://github.com/back-stack/kubecon-na-2025/blob/main/crossplane/05-compositions/web-app/go-templating.yaml>

**Why it matters:** Crossplane sets `generateName: <xr-name>-` when no name is given
(`RenderComposedResourceMetadata`, v2.4), producing `my-app-9bj8j`. Any composed object referenced
**by name** by another composed object therefore needs an explicit `metadata.name`.
**Frequency:** **57/114 v2+native compositions (50%) set a templated `metadata.name`** [C];
287/381 go-templating compositions template `metadata.name`, 2,106 occurrences [C].
**Verdict: structural, and a validation rule.** When node B references node A's name, force A to
`naming: explicit` and emit the *same* expression on both sides. This eliminates a real production
bug class.

#### C-6. Cross-Configuration label contract
```python
# producer (configuration-aws-network) stamps on every MR it creates:
labels = { "networks.aws.platform.upbound.io/network-id" = oxr.spec.parameters.id }

# consumer (configuration-aws-database) selects on it:
rdsv1beta1.SubnetGroup{ spec.forProvider.subnetIdSelector.matchLabels = {
    "networks.aws.platform.upbound.io/network-id" = params.networkRef.id } }
```
<https://github.com/upbound/configuration-aws-database/blob/main/functions/sqlinstance/main.k>
XRD side declares a plain id, deliberately *not* a Crossplane triad:
<https://github.com/upbound/configuration-aws-database/blob/main/apis/sqlinstances/definition.yaml#L71-L79>

**Frequency:** 103 hits across the Upbound corpus [C]; top keys
`networks.aws.platform.upbound.io/network-id` 54, `azure.platform.upbound.io/network-id` 38,
`networks.gcp.platform.upbound.io/network-id` 11 [C].
**Verdict: structural** — two concepts: `emitsLabel` (stamp on everything this blueprint produces)
and `externalRef` (an XRD input that becomes `matchLabels` on a named field). Composition-level
inbound port and outbound label contract.

#### C-7. XRD-level reference-triad passthrough
```gotemplate
{{- if $spec.parameters.kmsKeyArn }}
kmsKeyArn: {{ $spec.parameters.kmsKeyArn }}
{{- end }}
{{- if $spec.parameters.kmsKeyArnRef }}
kmsKeyArnRef: {{ $spec.parameters.kmsKeyArnRef }}
{{- end }}
{{- if $spec.parameters.kmsKeyArnSelector }}
kmsKeyArnSelector: {{ $spec.parameters.kmsKeyArnSelector }}
{{- end }}
```
<https://github.com/upbound/configuration-aws-dynamodb/blob/main/apis/table/composition.yaml>
(the matching ~60-line XRD schema fragment:
<https://github.com/upbound/configuration-aws-dynamodb/blob/main/apis/tableitemset/definition.yaml#L77-L150>)

**Frequency:** 1 Upbound repo, 3 fields [C]. Low count, high leverage — it generates ~60 lines of XRD
schema *and* the three-branch passthrough from one declaration.
**Verdict: structural**, low frequency.

#### C-8. Secret-key selector field (`{key, name}`)
```json
"passwordSecretRef": {"properties":{"key":{},"name":{}},"required":["key","name"],"type":"object"}
```
`users.sql.gcp.m.upbound.io`, provider-gcp-sql v3.0.1.
**Frequency:** **115 on GCP, 23 on AWS** — 5× denser on GCP [C]. Identified by shape +
`required: ["key","name"]`, **not** by the description grammar (105 of 115 carry business prose, not a
grammar sentence) [C].
**Verdict: structural**, but a **different node type / port colour** — it points at a `v1/Secret` in
the XR's namespace, not at a composed resource. In v2 namespaced it is `{key,name}` with **no
namespace**.

#### C-9. Observed-composed-resource data edge
Three interchangeable spellings, all present:
```gotemplate
{{ ( index $.observed.resources "sample-access-key-0" ).connectionDetails.username }}     # index  (41%)
{{- $accountName := dig "resources" "account" "resource" "metadata" "annotations" "crossplane.io/external-name" "" .observed }}  # dig+default (20%)
{{ $flexServer := getComposedResource . "flexServer" }}{{ get $flexServer.status "id" }}  # helper (7%)
```
**Frequency:** 121/381 (32%) read an observed composed resource by any spelling [C]; `.observed.resources`
89/381 (23%), 267 occurrences [C]; **422 occurrences / 121 files** across the 1,409-file corpus [C];
`dig` 75/381 (20%), 539 occurrences [C]; `getComposedResource` 25/381 (7%) [C];
the `.observed.resources nil` guard present in only 49/381 (13%) [C].
**Footgun F4** (open issue #78): indexing untyped nil is a **fatal** error for the whole step —
one unguarded observed read breaks every resource in the composition.
**Verdict: structural.** A data edge `nodeA.status.atProvider.id → nodeB.spec.forProvider.x`;
the emitter always picks `dig` + default and always adds the nil guard.

### D. Readiness

#### D-1. `function-auto-ready` as terminal step
**Frequency:** 257/381 (67%) [C]; 274/680 [C]; 262/356 (73.6%) [C]; 126/332 community (38%) [C];
55/84 Upbound pipeline compositions [C]. stuttgart-things codify it as a rule: *"Always end with
`function-auto-ready` step."*
**Verdict: structural** — not a node; an implicit tail step the generator appends and the user can
turn off.

**Hard version dependency.** `function-auto-ready` **v0.5.0** (the user's version) checks only for a
`Ready=True` condition. A `Deployment` reports `Available`, never `Ready`; `Service`/`ConfigMap`/
`Secret` have no conditions at all — so **on v0.5.0 every native K8s composed resource is permanently
not-ready, and so is the XR.** GVK health checks (Deployment, Service, Job, StatefulSet, Ingress,
PVC, HPA, ConfigMap, Secret, ServiceAccount, Namespace, Pod, ReplicaSet, DaemonSet, CronJob) landed
in **v0.6.0** (2025-12-05), verified present at v0.6.0/v0.6.8/v0.7.0 and absent at v0.5.x [C].
**This must be a blueprint input** (`functions.autoReady.version`) that changes the default readiness
mode of native nodes.

#### D-2. `ForceReady` — hardcoded `"True"`
```yaml
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: mesh-rules
    gotemplating.fn.crossplane.io/ready: "True"
```
(cujarrett/homelab). **Frequency:** annotation present in 47/381 (12%), 226 occurrences, of which
**218 hardcoded `"True"` across 43 compositions** [C]; and 93/109 occurrences (85%) across 20 files
in the v2 corpus [C].
**UNRESOLVED (minor):** 218/226 = 96% vs 93/109 = 85% for the "bare literal" share. Different corpora,
same conclusion.
Accepted values are exactly `"True"`, `"False"`, `"Unspecified"`, plus `"Unknown"` as an alias —
**anything else is a fatal pipeline error**, and it must be quoted (bare `True` becomes a YAML boolean).
**Verdict: structural** (an enum value). Worth a GUI warning: it is a *lie* the user is choosing.

#### D-3. `DeriveReadyFromCondition` / `DeriveReadyFromField`
```yaml
# Deployment: bridge Available -> Ready
{{ if eq (.observed.resources.deployment | getResourceCondition "Available").Status "True" }}
gotemplating.fn.crossplane.io/ready: "True"
{{ end }}

# Service: readiness from a field's presence
{{ if (get (getComposedResource . "service").spec "clusterIP") }}
gotemplating.fn.crossplane.io/ready: "True"
{{ end }}
```
(crossplane/docs v2.4 get-started manifests; back-stack/kubecon-na-2025 basic-app + web-app)
**Frequency:** 14/109 occurrences (13%) [C]; only **8 compositions** in the 381-corpus derive readiness
from `availableReplicas`/`readyReplicas` [C].
Helper semantics the emitter must respect: `getResourceCondition` returns an empty condition when
absent (safe, no nil guard); `getComposedResource` returns nil when absent (**must** be guarded).
Precedent that this belongs in a schema: function-kro exposes `readyWhen` as a first-class list field
in the same docs page.
**Verdict: structural.** `readyWhen: {source, kind: condition|fieldPresent, type, status, path}`.
Note this is **rarer than expected but exactly the right shape** — model it, default to auto.

#### D-4. `ComputedReadyValue`
```gotemplate
{{- $rdsReady := "False" }}
{{- with index (.observed.resources | default dict) "rds-instance" }}
  {{- range (.resource.status.conditions | default list) }}
    {{- if and (eq .type "Ready") (eq .status "True") }}{{- $rdsReady = "True" }}{{- end }}
  {{- end }}
{{- end }}
...
    gotemplating.fn.crossplane.io/ready: {{ $rdsReady | quote }}
```
<https://github.com/cujarrett/homelab/blob/main/platform/sql/composition.yaml>
**Frequency:** **2/109 (2%)** [C].
**Verdict: raw-escape-only** on the readiness property. 2/109 does not justify a boolean-expression
editor.

#### D-5. Aggregated status/phase derivation step
```gotemplate
{{- $phase := "Creating" -}}
{{- if and $queueReady $kedaReady $workloadReady -}}{{- $phase = "Ready" -}}
  {{- if eq (int $workloadReplicas) 0 -}}{{- $phase = "Napping" -}}{{- end -}}{{- end -}}
status:
  phase: {{ $phase }}
```
<https://github.com/deliveryhero/asya/blob/main/deploy/helm-charts/asya-crossplane/templates/composition-sqs.yaml>
**Frequency:** ~100 lines in 1 repo [C]; Giant Swarm compute `readystr(name)` for every emitted
resource across 13 compositions [C].
**Verdict: structural-with-effort** — a "phase" pseudo-node whose rows are
`{phase, when: allOf[nodeRefs ready] + extra condition}`. The generator owns the boilerplate; the
*ordering* of phase rules is a small editor.

#### D-6. `readinessChecks: [{type: None}]` (legacy P&T)
**Frequency:** `None` 56 uses; `MatchString` 16; `NonEmpty` 2; everything else 0 [C]. In the Upbound
corpus, readinessChecks appear in only 12/118 compositions, 20 of 21 checks being `type: None` [C].
**Verdict: structural** — the same `readiness: never` enum value, different emission backend.

#### D-7. `DeriveFromCelQuery` readiness for `Object`s
```gotemplate
{{- define "allotment.nestedObjectReadiness" }}
readiness:
  policy: DeriveFromCelQuery
  celQuery: has(object.status.conditions) && object.status.conditions.exists(c, c.type == "Ready" && c.status == "True")
{{- end }}
```
<https://github.com/livewyer-ops/gardener-allotment/blob/main/platform/templates/_helpers.tmpl>
**Frequency:** 5/332 community files (2%) [C].
**Verdict: structural** — a readiness-policy enum value on `Object` nodes only.

### E. Ordering

#### E-1. `dependsOnReady` guard (`ready(X) or exists(self)`)
```python
if ready(get(_ocds, "kubernetesCluster", "")) or exists("kubernetesClusterAuth"):
    _items += [ eksv1beta1.ClusterAuth { ... } ]
if ready(get(_ocds, "vpc-cni-addon", "")) or exists("nodeGroupPublic"):
    _items += [ eksv1beta1.NodeGroup { ... } ]
```
<https://github.com/upbound/configuration-aws-eks/blob/main/functions/eks/main.k>
with the author's own comment elsewhere: *"Also create if it already exists to prevent uninstalling"*
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>

**Frequency:** ~15 sites across the Upbound corpus; `_ocds` read in 21/34 KCL fns (130 hits);
`status?.conditions` read in 8/34 [C]. Called **"the highest-value single feature in this brief"**
by `cs-upbound`.
**Verdict: structural** (a second, dashed edge type). The `or exists(self)` half is load-bearing —
without it the resource is *deleted* when the dependency flaps. **A naive `if ready(X)` emission is
a bug.**

#### E-2. `function-sequencer`
```yaml
- step: enforce-creation-sequence
  functionRef: {name: crossplane-contrib-function-sequencer}
  input:
    apiVersion: sequencer.fn.crossplane.io/v1beta1
    kind: Input
    rules:
      - sequence: ["account", "endpointblob"]
      - sequence: ["account", "endpointfile"]
```
<https://github.com/platformplane/catalog-crossplane-azure/blob/main/package/azurestorage/v2/composition.yaml>
**Frequency:** 62 files GitHub-wide [C]; 9/381 [C]; 10/356 (2.8%) [C]; 5/118 Upbound [C].
Input surface: `rules[].sequence[]`, `condition` (CEL), `createOnly`/`deleteOnly`,
`enableDeletionSequencing`, `replayDeletion`, `usageVersion`, `cacheTTL`, `resetCompositeReadiness`.
**Verdict: structural — an edge type, not a node.** The transitive closure of ordering edges *is*
`rules[].sequence[]`. This is a cheap, correct alternative to E-1 that gets the `or exists` semantics
right by construction.

#### E-3. `Usage` objects for deletion ordering
```python
{ apiVersion: "protection.crossplane.io/v1beta1"
  kind: "Usage"
  spec: { replayDeletion = True
          by: { apiVersion = "aws.platform.upbound.io/v1alpha1", kind = "EKS",
                resourceSelector: { matchControllerRef = True } }
          of: { apiVersion = "aws.platform.upbound.io/v1alpha1", kind = "Network",
                resourceSelector: { matchControllerRef = True } } } }
```
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>
In go-templating the target is found by mirroring the resource-name annotation into a **label**:
<https://github.com/upbound/platform-ref-upbound-spaces/blob/main/apis/space-init/composition.yaml>

**Frequency:** 10 hits / 3 Upbound fns + 3 legacy [C]; 25 hand-written `Usage`/`ClusterUsage` across
17 files in the 1,409-file corpus [C]. Two API versions in flight
(`protection.crossplane.io/v1beta1` in v2 repos, `apiextensions.crossplane.io/v1alpha1` in older).
**Verdict: structural** — the same ordering edge, a different emission backend (~25 lines of YAML per
edge, generated).

### F. Outputs

#### F-1. XR status derivation
```gotemplate
---
apiVersion: platform.openkubes.ai/v1alpha1
kind: Database
status:
  evidence:
    operational:
      state: {{ $operationalState }}
      evidenceRef: {{ printf "Cluster/%s/%s" $namespace $dbName | quote }}
```
(openkubes). Whole-block dump form:
```gotemplate
status:
  dynamodbTable:
  {{ $tableStatus.atProvider | toYaml | nindent 4 }}
```
<https://github.com/upbound/configuration-aws-dynamodb/blob/main/apis/table/composition.yaml>

**Frequency:** 105/381 (28%) emit a composite `status:` document, 112 occurrences [C];
20/34 Upbound KCL fns write `_dxr`, and 85/117 XRDs declare a `status` schema [C];
`ToCompositeFieldPath` (the P&T equivalent) 247/1,899 patches (13%) [C].
**The `fn.go` rule the emitter must honour:** a document of the XR's own apiVersion+kind with **no**
resource-name annotation ⇒ status **merge** (`mergo.WithOverride`); *with* one ⇒ recursive XR
composition. This is a genuine DSL-visible mode switch.
**Verdict: structural** for field lift and whole-block dump.

#### F-2. Aggregate-then-filter status
```python
createdSubnets = [ c for c in [ { id = _getExternalName(r.name), type = r.type }
                                for r in [ {name = "subnet-" + _formatSubnet(s), type = s.type}
                                           for s in oxr.spec.parameters.subnets ] ]
                   if c.id != None ]
status = { subnetIds = [s.id for s in createdSubnets]
           publicSubnetIds  = [s.id for s in createdSubnets if s.type == "public"]
           privateSubnetIds = [s.id for s in createdSubnets if s.type == "private"] }
```
<https://github.com/upbound/configuration-aws-network/blob/main/functions/network/main.k>
**Frequency:** 1 site [C].
**Verdict: raw-escape-only.** The simple "collect one field from every loop iteration into a list"
subset is structural (see §4); the *filter-by-a-property-of-the-item* variant is not.

#### F-3. Aggregate connection Secret (the v2 replacement)
```gotemplate
---
apiVersion: v1
kind: Secret
metadata:
  name: {{ dig "spec" "writeConnectionSecretToRef" "name" "" $.observed.composite.resource }}
  annotations:
    {{ setResourceNameAnnotation "connection-secret" }}
{{ if eq $.observed.resources nil }}
data: {}
{{ else }}
data:
  user-0: {{ ( index $.observed.resources "accesskey-0" ).connectionDetails.username }}
  password-0: {{ ( index $.observed.resources "accesskey-0" ).connectionDetails.password }}
{{ end }}
```
<https://github.com/crossplane/docs/blob/main/content/v2.4/manifests/guides/connection-details-composition/composition-go-templating.yaml>

**Frequency:** 47/381 (12%) emit a `kind: Secret` for connection details, 73 occurrences [C];
151/332 community files touch connection secrets (45%) [C]; `writeConnectionSecretToRef` 121/680 [C];
`.connectionDetails` read in 97/680 [C]; `b64enc`/`b64dec` 65/381 (17%) [C].

Five mechanics the emitter must get exactly right: (1) `writeConnectionSecretToRef` on a namespaced
MR takes **`name` only**; (2) `.connectionDetails` values are **already base64** → `data:`, verbatim,
while literals need `| b64enc`; (3) the `eq $.observed.resources nil` first-reconcile guard is
**mandatory**; (4) cross-resource wiring uses the composition-resource-name, not the K8s name;
(5) `matchControllerRef: true` finds the sibling.
**Verdict: structural** — named "the single biggest ergonomic win available". Pair it with an
**explicit** readiness derivation: auto-ready ≥v0.6.0 marks a Secret ready on *existence*, so the XR
would report Ready while `data` is still `{}`.

#### F-4. `CompositeConnectionDetails` — dead on v2
```python
{ apiVersion: "meta.krm.kcl.dev/v1alpha1"
  kind: "CompositeConnectionDetails"
  data: { kubeconfig = _ocds["EKS"].ConnectionDetails.kubeconfig } }
```
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>
**Frequency:** 19/381 (5%) [C]; **29/680 files still emit it** [C]; 7 Upbound fns + 18 legacy
compositions [C].
**Why it is dead:** `spec.writeConnectionSecretToRef` is only added to the generated CRD schema for
`LegacyCluster` XRs, and `APIFilteredSecretPublisher.PublishConnection` early-returns when the ref is
nil. So on a v2 XR: nothing is published, **with no error**.
**UNRESOLVED (wording):** `cs-gotemplating` says the function *"refuses it for v2 XRs"*;
`cs-v2-native` and the grounding doc [V] both say it is parsed and **silently ignored**. Two sources
against one — treat it as a **silent no-op**, and have the generator *refuse to produce it* for a v2
XRD.

#### F-5. `ClaimConditions`
```gotemplate
apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
kind: ClaimConditions
conditions:
  - type: UnifiNetworkMatch
    status: {{ if $ok }}"True"{{ else }}"False"{{ end }}
    reason: {{ if $ok }}Matches{{ else if not $networkFound }}NetworkNotFound{{ else }}Mismatch{{ end }}
    target: CompositeAndClaim
```
<https://github.com/estenrye/flux-platform-src/blob/51426485942bb9048fa619528b8dbb5023b54764/applications/crossplane-resources/xnetworksegment/composition.yaml>
**Frequency:** 4/381 (1%), 9 occurrences [C]. `Healthy`/`Ready`/`Synced` are rejected by the function.
On a claim-less v2 XRD, `target: CompositeAndClaim` degrades to `Composite` [V].
**Verdict: structural** — cheap, and it is the only user-facing error channel in v2.

#### F-6. `fail` preconditions
```gotemplate
{{- if not (hasKey $store "endpointURL") }}
{{- fail (printf "no reviewed backup store is registered for cluster %q: refusing to compose a Database with no protection destination." $provider) }}
{{- end }}
```
(openkubes). **Frequency:** `required`/`fail` in 26/381 (7%), 40 occurrences [C]; 42/332 community
files use `required` (13%), 1 uses `fail` [C]. KCL's equivalent is `assert ... , "message"`.
**Verdict: structural** — a `preconditions:` list on the blueprint.

### G. Cross-cutting propagation

#### G-1. The `defaults` block
```python
_defaults = {
    managementPolicies = params.managementPolicies or ["*"]
    if providerConfigRefName:
        providerConfigRef = { kind = "ProviderConfig", name = providerConfigRefName }
    forProvider.region = params.region
}
# then on every resource:  spec = _defaults | { forProvider = { ... } }
```
<https://github.com/upbound/configuration-aws-database/blob/main/functions/sqlinstance/main.k#L15-L23>
P&T equivalent — the three-class patchSet design:
<https://github.com/awslabs/crossplane-on-eks/blob/main/compositions/upbound-aws-provider/serverless-microservice/rest-lambda-ddb.yaml>

**Frequency:** `providerConfigRef` 218/381 (57%) [C], 219/332 community (65%) [C], 70 hits / 23
Upbound files [C]; `managementPolicies` 80 hits / 23 Upbound files [C], 37/332 (11%) [C];
`deletionPolicy` 81 hits / 10 Upbound files [C], 96/332 (29%) [C]; `region` 81/332 (24%) [C];
tags 64/332 (19%) [C]; `patchSets:` 80/332 (24%), 339/1,899 patches are `type: PatchSet` (18%) [C].
Giant Swarm hand-copy the same ~90-line KCL preamble **31 times** across 27,206 YAML lines [C].
**Verdict: structural** — a composition-level `defaults:` block with per-node opt-out. Removes ~40%
of field mappings in a typical blueprint [I].

#### G-2. `providerConfigRef` — four distinct shapes
1. Literal: `{name: provider-helm, kind: ClusterProviderConfig}`
2. From an XR field: `spec.resourceConfig.providerConfigName → spec.providerConfigRef.name`
3. **Kind switched by an XR enum:**
```gotemplate
{{- $pcKind := "ProviderConfig" -}}
{{- if eq $scope "Cluster" -}}{{- $pcKind = "ClusterProviderConfig" -}}{{- end -}}
  providerConfigRef: {name: {{ $provider }}, kind: {{ $pcKind }}}
```
<https://github.com/stuttgart-things/crossplane/blob/main/configurations/apps/github-runner/compositions/github-runner.yaml>
4. Fallback chain resource → XR → `{name: ""}` (Giant Swarm `gpcr`)

**Frequency:** `kind: ClusterProviderConfig` in 44/381 (12%), 132 occurrences [C]; **84 of 106
compositions that emit `.m.` MRs set `kind: ClusterProviderConfig` explicitly** [C]. In v2 the ref
defaults to `{name: default, kind: ClusterProviderConfig}` if omitted; the field is `required:
[kind, name]` on `.m.` CRDs [C]. **Upbound uses namespaced `ProviderConfig` and ships
`ClusterProviderConfig` in 0 composition sources** [C].
**Verdict: structural.** `kind` must be a first-class enum populated from the provider group's own
CRDs — never hard-coded — because `ProviderConfig` means *Namespaced* in the `.m.` group and *Cluster*
in the legacy group [V].

#### G-3. v2 namespace derivation
**The rule:** `scope: Namespaced` → emit **no** `namespace` (Crossplane force-overwrites it);
`scope: Cluster` → `namespace` is required and is the only way to place the resource.
Proof: back-stack ship the same app twice, and `diff namespaced/ cluster-scoped/` is *entirely*
two added `namespace:` lines.
<https://github.com/back-stack/kubecon-na-2025/tree/main/crossplane/05-compositions/basic-app>

Enforcement, `composition_functions.go` v2.4: a namespaced XR composing a cluster-scoped kind fails
the **whole composition** with `cannot apply cluster scoped composed resource %q ...`.
**Frequency:** 175/381 (46%) template a `namespace:` [C]; 78/114 v2+native compositions write one
somewhere (harmless-but-redundant on namespaced XRs) [C].
**Verdict: structural and derived.** Hide the field entirely when `scope: Namespaced`. **Filter the
node palette by target scope** so a namespaced XR can never be given a `Namespace`, `ClusterRole`, or
`StorageClass` node.

#### G-4. Aggregated RBAC ClusterRole for native kinds
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cnpg:aggregate-to-crossplane
  labels:
    rbac.crossplane.io/aggregate-to-crossplane: "true"
rules:
- apiGroups: ["postgresql.cnpg.io"]
  resources: ["clusters"]
  verbs: ["*"]
```
(crossplane/docs v2.4 `composition/compositions.md`)
**Frequency:** not counted; named **"the #1 'why is nothing happening' failure for v2 native
composition"** [I].
**Verdict: structural and fully automatable** — the tool knows every composed GVK.

#### G-5. `ManagedResourceActivationPolicy`
```yaml
apiVersion: apiextensions.crossplane.io/v1alpha1
kind: ManagedResourceActivationPolicy
metadata: {name: configuration-aws-network}
spec:
  activate:
  - vpcs.ec2.aws.m.upbound.io
  - subnets.ec2.aws.m.upbound.io
```
<https://github.com/upbound/configuration-aws-network/blob/main/apis/networks/mrap.yaml>
**Frequency:** **2/71 Upbound repos** [C]; near-zero elsewhere — it is new [C].
**Verdict: structural (derived, no user input)**, but **opt-in output**, not default: v2 ships a
catch-all `*` MRAP, so emitting one only matters if the user replaced it.

#### G-6. Output metadata presets
**Frequency (occurrences across 332 community files):** `composition-resource-name` 237,
`argocd.argoproj.io/sync-wave` 86, `app.kubernetes.io/name` 132, `awsblueprints.io/environment` 85,
`crossplane.io/xrd` 70, `crossplane.io/external-name` 49 [C]. **88 files / 150 occurrences carry
ArgoCD annotations (27%)** [C]. VSHN apply a whole self-service-catalogue metadata block via a
post-processing pass (`add_argo_wave_crossplane.jsonnet`).
**Three things never to emit** [V]: `argocd.argoproj.io/tracking-id`; a default `kustomization.yaml`;
any generated-at annotation (perpetual sync loop under `selfHeal`).
**Verdict: structural** — label/annotation preset sets plus a post-processing hook.

### H. Inputs beyond the XR

#### H-1. EnvironmentConfig via `function-environment-configs` + context
```yaml
- step: environmentConfigs
  functionRef: {name: function-environment-configs}
  input:
    apiVersion: environmentconfigs.fn.crossplane.io/v1beta1
    kind: Input
    spec:
      environmentConfigs:
      - {type: Reference, ref: {name: allotment-versions}}
      - {type: Reference, ref: {name: allotment-versions-aws}}
      - {type: Reference, ref: {name: allotment-config}}
```
<https://github.com/livewyer-ops/gardener-allotment/blob/main/platform/compositions/aws/infra.yaml>
consumed as `{{ $envCtx := index .context "apiextensions.crossplane.io/environment" | default dict }}`.

**Frequency:** 163 files GitHub-wide [C]; 68/680 (10%) [C]; 68/381 compositions have the step (18%) [C];
51/356 (14.3%) [C]; 31/332 community (9%) [C]; `.context` read in 70/381 (18%) and the environment key
specifically in 50/381 (13%) [C].
**UNRESOLVED — corpus split.** **Upbound: 0 across all 71 repos** (zero `EnvironmentConfig`,
`FromEnvironmentFieldPath`, `spec.environment`) [C], and their brief says *"Do not spend DSL surface on
it."* Every other corpus puts it at 9–18%. Resolution taken here: **it is real, it is the highest-
frequency non-resource step, and Upbound is the outlier** — but it is a *blueprint section plus a
mapping source*, not a node type, so the surface cost is small. Merge order matters (later index wins).
Caveat: EnvironmentConfigs are cluster-scoped, so they cannot be tenant-scoped by namespace.
**Verdict: structural.**

#### H-2. External lookup — three competing mechanisms
**(A) `function-extra-resources`** — 79 files GitHub, 20/356 (5.6%) [C]:
```yaml
    spec:
      extraResources:
        - kind: XUnifiNetwork
          apiVersion: platform.rye.ninja/v1alpha1
          into: XUnifiNetwork
          type: Selector
          selector:
            maxMatch: 1
            matchLabels:
              - key: platform.rye.ninja/unifi-network-name
                type: FromCompositeFieldPath
                valueFromFieldPath: spec.unifiNetworkRef.name
                fromFieldPathPolicy: Optional
```
<https://github.com/estenrye/flux-platform-src/blob/main/applications/crossplane-resources/xnetworksegment/composition.yaml>

**(B) go-templating's inline `ExtraResources` meta doc** — 35 files GitHub, 19 in corpus,
14/381 (4%) [C]. The requirement itself is *templated*, so the lookup key can be computed from the XR.

**(C) Crossplane v2 `requirements.requiredResources`** — a **step-level** field.
**0 occurrences in any corpus** [C] because it is new; but `function-go-templating` v0.12.0 already
resolves `requiredResources[<name>].items` first, falling back to `extraResources[...]`.
Crossplane's own docs: *"Use bootstrap requirements when possible for better performance."*

Two-pass semantics are mandatory in all three: the function runs once returning requirements, then is
re-invoked with results populated — an unguarded read is a **fatal** first-pass error [V].
Behaviour differs between `crossplane render` and a live cluster (issues #536, #501).
**Verdict: structural** — a Lookup node with a dashed border, an `into`-named **list** output port
that defaults into a loop node, and a `maxMatch: 1` scalar form. Emit the **v2 form** by default.

#### H-3. `getCredentialData` (step-level credentials)
**Frequency:** 12/381 (3%), 13 occurrences, 2 repos [C].
**Verdict: structural**, trivial (a step-level `credentials: [{name, secretRef, source: Secret}]` list),
but very low priority.

### I. Pipeline patterns

#### I-1. Multi-step pipelines
**Step-count distribution (356 files):** 1 step 61 (17%), 2 steps 175 (49%), 3 steps 82 (23%),
4–12 steps 38 [C]. **≥3 steps = 120 files (34%)** [C]. Upbound (84 pipeline compositions): 1 step 23,
2 steps 51, 3 steps 9, 4 steps 1 [C] — "two steps is the norm and the second is always
`function-auto-ready`."
**Authoring granularity:** of 486 inline go-templating steps, **53.5% emit exactly one named
resource**, 13.8% two, 4.3% none (status/context only), 11.9% six or more [C].
**Verdict: structural** — see §5.

#### I-2. Multiple go-templating steps in one pipeline
**Frequency:** 56/381 (15%) [C]. Real 9-step (Delivery Hero) and 10-step (estenrye) pipelines exist,
the latter with **four dedicated validation steps that create no resources**.
**Verdict: structural** (the generator owns step layout) — see §5.

#### I-3. P&T coexisting with go-templating
```yaml
- step: render-templates          # go-templating creates "bucketACL"
- step: patch-and-transform-again # P&T resource with NO `base:` attaches to it by name
  input:
    resources:
      - name: bucketACL
        patches: [{type: FromCompositeFieldPath, fromFieldPath: spec.acl, toFieldPath: spec.forProvider.acl}]
```
<https://github.com/crossplane-contrib/function-patch-and-transform/blob/main/example/multistep/composition.yaml>
**Frequency:** 66/381 (17%) run both [C]; 40/332 community P&T step instances [C]; 23/156 Upbound
pipeline steps [C]. Footgun F7 (open issue #41): the two functions use **different** resource-name
annotations, so both spellings coexist in one file.
**Verdict: structural-with-effort** — post-hoc patching of another step's resources has no natural
node-graph representation (see §7).

#### I-4. `Context` write between steps
```yaml
apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
kind: Context
data:
  "asya/user-labels":
    {{- $userLabels | toYaml | nindent 4 }}
```
(Delivery Hero). **Frequency:** 3/381 (1%), 5 occurrences [C]; 12 occurrences / 8 files across the
1,409-file corpus [C]. Reads are far more common than writes (`.context` 18%).
**Verdict: raw-escape-only.** If the generator owns step layout it can recompute the value in the
consuming step instead. Justification for T3 is in §3.

#### I-5. `source: FileSystem` + ConfigMap + `DeploymentRuntimeConfig`
```yaml
- step: create-namespace
  functionRef: {name: function-go-templating}
  input:
    source: FileSystem
    fileSystem: {dirPath: /templates/xnamespace/ns.yaml}
```
<https://github.com/konflux-ci/crossplane-control-plane/blob/main/config/xnamespace/composition.yaml>
backed by a `DeploymentRuntimeConfig` mounting a ConfigMap at `/templates/xnamespace`.

**Frequency:** `source: Inline` 472 steps, `FileSystem` 40, `Environment` 2 [C]; 14 community files [C].
**UNRESOLVED — viability.** The grounding doc says *"`source: Inline` is the only viable template
source … incompatible with a plain ArgoCD directory sync"* [V/D]. `cs-community-platforms` calls the
ConfigMap+DeploymentRuntimeConfig form *"the cleanest generated artifact shape in the whole corpus,
and the one a node-graph GUI maps onto most naturally (one file per node)."* Both are right about
different things: it works, at the cost of two extra artifacts and a runtime-config dependency.
**Verdict: structural** as an *output mode*, flagged UNRESOLVED.

#### I-6. Generic non-go-templating step (KCL / Python / in-house)
```yaml
  pipeline:
  - functionRef: {name: upbound-configuration-caasxcluster}
    step: xcluster
  - functionRef: {name: crossplane-contrib-function-auto-ready}
    step: crossplane-contrib-function-auto-ready
```
<https://github.com/upbound/configuration-caas/blob/main/apis/composition.yaml> (17 lines;
`functions/xcluster/main.k` is 624 lines emitting 23 named resources)

**Frequency:** `krm.kcl.dev/v1alpha1` **666 files** GitHub-wide — KCL is the #3 function after P&T
(1,188) and go-templating (914) [C]. 51/156 Upbound pipeline steps are embedded project functions
(34 KCL / 7 Python repos / 1 TS / 1 gotmpl) [C]. **66/332 community compositions (20%) have abandoned
declarative composition YAML entirely** — including the two most mature shops, VSHN AppCat (52) and
modelplane (14) — the Composition being a 12–130 line shim handing a ConfigMap of settings to an
in-house Go function [C]. `function-python` 41 files, `function-cue` 10, `function-shell` 9 [C].
**Verdict: structural-with-effort** — an opaque "function step" node with a code/YAML blob and
declared position constraints. Do not attempt to parse the body.

#### I-7. `functionRef.name` resolution
**The bug:** `crossplane composition generate` emits `crossplane-contrib-function-auto-ready` while
installed `Function` objects are named `function-auto-ready` — applying it dangles the ref [V].
Both spellings are live in the wild: `function-go-templating` (89 steps) *and*
`crossplane-contrib-function-go-templating` (42 steps) in the same 332-file corpus [C].
stuttgart-things call it out in their conventions file: *"Function Names (must match between
composition and functions.yaml)"*. VSHN pin a **revision suffix** into both
(`function-appcat-master-v4-194-0`). Konflux use digest-pinned mirrored images
(`quay.io/konflux-ci/.../function-go-templating@sha256:e2ea39…`).
In v2, `spec.package` **must be fully qualified** (default registry removed).
**Verdict: structural** — one source of truth emitting both `functions.yaml` and every
`functionRef.name`, with arbitrary registries and digest pinning.

### J. Genuine escapes

#### J-1. Nested templates (`define`/`include`/`template`)
```gotemplate
{{- define "labels" -}}...{{- end -}}
{{- include "labels" $vals | nindent 4}}   ## weird indentation to make it work
```
(upstream `example/functions/include`, comment verbatim)
**Frequency:** `define` 6/381 (2%), 9 occurrences; `include` 6/381, 29 occurrences; `template` 2/381,
7 occurrences [C].
**Verdict: raw-escape-only.**

#### J-2. Recursive / meta template engine
```gotemplate
{{- define "setNestedValue" -}}
  {{- $parts := regexSplit "\\." $path -1 -}}
  {{- if eq (len $parts) 1 -}}{{- $_ := set $dict (first $parts) (append $valueList $value) -}}
  {{- else -}}{{- template "setNestedValue" (list (index $dict $firstKey) (join "." (rest $parts)) ...) -}}{{- end -}}
{{- end -}}
{{- range $resource := $step.resources }}
apiVersion: {{ $resource.spec.apiVersion }}
kind: {{ $resource.spec.kind }}
```
<https://github.com/livewyer-ops/crossplane-configuration-aws-elemental/blob/f7b436fa2de079f1a8d1be00095abf66073236ab/apis/workflow/composition.yaml>
**Frequency:** `set`/`mergeOverwrite`/`regexSplit`-driven engines in **18/381 (5%)** [C].
**Verdict: raw-escape-only, whole-step.** The composed apiVersion/kind are *data*, not schema.
Also a warning: **people build meta-compositions when the authoring tool is too rigid.**

#### J-3. Derived-collection prelude (list partitioning)
```gotemplate
{{- $appRoles := list }}{{- $delegatedScopes := list }}
{{- range $iface := $provides }}
{{- if eq $iface.auth "user" }}{{- $delegatedScopes = append $delegatedScopes $iface }}
{{- else if eq $iface.auth "workload" }}{{- $appRoles = append $appRoles $iface }}{{- end }}
{{- end }}
{{- $entraNeeded := or (gt (len $appRoles) 0) (gt (len $delegatedScopes) 0) }}
```
<https://github.com/cujarrett/homelab/blob/5ab10ce6af91dafd8a77e47abfafdc84bead72a7/platform/api/composition.yaml>
Also: Giant Swarm's 300-line `routingTables` CIDR/blackhole comprehension; openkubes' ~120 lines of
RFC3339 timestamp arithmetic deriving `$protectionState`.
**Frequency:** [I] — present in 3 of the 5 "most complex" compositions across two briefs.
**Verdict: raw-escape-only — and it needs a *specific* escape flavour: `rawPrelude`, a raw template
block that runs before the nodes and binds named variables the structural mappings can then reference.**
Named independently by two briefs as the strongest argument for that flavour.

#### J-4. Claim/composite selector shim
```python
clusterNameSelector = { matchLabels = { "crossplane.io/claim-name" = params.id }
                          if oxr.spec?.claimRef?.name else
                        { "crossplane.io/composite" = params.id } }
```
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>
**Frequency:** 1 site [C]. **Verdict: raw-escape-only** (a ternary inside a field value).

#### J-5. Reference bound to a specific array element
```
deidentifytemplates.datalossprevention.gcp.m.upbound.io
  spec.forProvider.deidentifyConfig.recordTransformations.fieldTransformations[]
    .infoTypeTransformations.transformations[].primitiveTransformation
    .cryptoDeterministicConfig.cryptoKey.unwrapped.keySecretRef
```
**Frequency:** **103/578 GCP refs (17.8%), 29/464 AWS refs (6.2%)** — nearly 3× denser on GCP [C].
**Verdict: raw-escape-only**, *unless* the array is itself a `forEach` source (then the edge is
per-item and structural). A node-graph edge cannot address `spec.forProvider.networks[3].networkRef`
without an index literal. Called by `cs-gcp-portability` "the strongest argument for a per-field
`rawTemplate` escape in the reference layer".

#### J-6. Extractor invisibility (not an escape — a hard limit)
The description says *which field* gets populated, never *what value* lands there. That is decided by
an `Extractor` in the provider's Go config, invisible in the CRD: `common.PathARNExtractor` (99+14 on
AWS), `common.PathSelfLinkExtractor` (24 on GCP), `ExtractParamPath(...)`.
**Frequency:** 80 of 178 `config.Reference{}` blocks on GCP, 162 of 427 on AWS declare a non-default
Extractor [C].
**Verdict:** structural for *drawing* the edge; **impossible for previewing it.** Label the edge with
the target Kind and leave the value to the provider. **Never hand-patch `status.atProvider.id` where
a ref triad exists** — the extractor may be `arn`, not `id`.

---

## 3. THE DSL COVERAGE MATRIX

Blueprint YAML fragments are what the **user writes**. Emitted output is §4.
Frequencies are abbreviated; full denominators are in §2.

### T1 — must be first-class in v1

| # | Pattern | Frequency | Blueprint DSL representation | Tier |
|---|---|---|---|---|
| 1 | XRD parameters, single-source defaults/enum/required (§A-2, §G-1) | 428 `default:` across 117 XRDs (3.7/XRD) [C]; defaults written **twice** (XRD + template) is a documented pain in ≥2 repos | `parameters:`<br>`  size: {type: string, enum: [xs,sm,md], default: sm}` → emits the XRD schema **and** the `\| default "sm"` from one line | **T1** |
| 2 | Variable prelude (§A-1) | 47% / 83% [C] | *(no DSL surface — pure emitter)* | **T1** |
| 3 | Field mapping + default (§A-2) | 56%, 1530 occ [C] | `- {to: spec.forProvider.region, from: env.region, default: eu-north-1}` | **T1** |
| 4 | Type-aware quoting (§A-3) | 27% quote; fatal on annotations [V] | *(derived from the CRD schema type; `quote: force` override)* | **T1** |
| 5 | Conditional field omission (§A-4) | dominant in status blocks; `hasKey` 376 occ [C] | `- {to: spec.forProvider.kmsKeyId, from: params.kmsKeyId, omitEmpty: true}` | **T1** |
| 6 | Value map / t-shirt sizing (§A-5, §A-6) | 17–22%, 1091 `dict` occ; 947 reassignments [C] | `valueMaps: {size: {key: {from: params.size}, fallback: sm, table: {xs: {...}, sm: {...}}}}` | **T1** |
| 7 | Derived name + sanitiser (§A-7) | printf 43%; hash-clamp in ≥3 repos [C/I] | `derived: {appName: {fmt: "%s-%s", inputs: [...], sanitize: {lower: true, maxLength: 63, hashSuffix: sha256/8}}}` | **T1** |
| 8 | Map merge / tags passthrough (§A-9) | 23% `range $k,$v`; 19% tags [C] | `tags: {merge: [{from: params.tags}, {app: "{derived.appName}"}]}` — emitter owns `nindent N` | **T1** |
| 9 | Node identity = resource-name annotation (§B-1) | 70% + 22% helper; fatal if absent [C] | `nodes: [{name: deployment, ...}]` — the `name` **is** the annotation | **T1** |
| 10 | Node `when:` — conditional resource (§B-2) | 23% wrap whole docs, 315 blocks [C] | `when: {path: params.bucket.enabled, op: isTrue}` | **T1** |
| 11 | Node `forEach:` — loop over an XRD array (§B-3, §B-5) | 25% wrap whole docs, 165 blocks; 2,099 wasted lines in one file [C] | `forEach: {over: params.queues, as: q, name: "queue-{q.name}"}` | **T1** |
| 12 | Native K8s node type (§B-6) | **36% of v2 compositions** [C] | `- {name: deployment, gvk: {apiVersion: apps/v1, kind: Deployment}}` | **T1** |
| 13 | Reference edge → `matchControllerRef` (§C-1, §C-2) | **578/578 & 464/464 triads**; 99 hits/20 files [C] | `refs: [{field: dbSubnetGroupName, to: dbSubnetGroup}]` → `matchControllerRef: true` | **T1** |
| 14 | Reference edge → `matchLabels` across a loop (§C-3) | 53 hits/19 files; 8% [C] | same `refs:` entry — the emitter **detects the loop boundary** and stamps/selects labels automatically | **T1** |
| 15 | Reference edge → `<x>Ref: {name}` (§C-4) | 56%, 972 occ [C] | `refs: [{field: xyz, to: nodeA, by: name}]` | **T1** |
| 16 | Explicit-name coupling (§C-5) | 50% of v2+native set a templated name [C] | `naming: generated \| explicit` — auto-forced to `explicit` when another node references it; **refuse to leave it generated** | **T1** |
| 17 | Secret-key selector field (§C-8) | 115 GCP / 23 AWS [C] | `- {to: spec.forProvider.passwordSecretRef, secretRef: {name: "{derived.appName}-db", key: password}}` | **T1** |
| 18 | Observed-resource data edge (§C-9) | 32%; 422 occ/121 files; fatal if unguarded [C] | `- {to: spec.x, fromNode: {node: db, path: status.atProvider.arn, default: ""}}` → emits `dig` + nil guard | **T1** |
| 19 | `defaults:` block (§G-1, §G-2) | pcRef 57–65%, mgmtPol 11%, delPol 29%, region 24%, tags 19% [C] | `defaults: {providerConfigRef: {kind: ClusterProviderConfig, name: "{params.providerConfigName}"}, managementPolicies: ["*"], tags: {...}}` with per-node `defaults: false` | **T1** |
| 20 | v2 scope handling: `.m.` group, namespace derivation, palette filter (§G-3) | 29% touch `.m.` groups; hard failure otherwise [C/V] | `scope: Namespaced` — namespace field **hidden**, palette filtered, `.m.` variant selected | **T1** |
| 21 | `readiness:` enum (§D-2, §D-3, §D-6) | annotation 12%; 85–96% of those literal True; `None` 56 uses [C] | `readiness: always \| never \| auto \| {condition: Available} \| {fieldPresent: spec.clusterIP}` | **T1** |
| 22 | Terminal auto-ready + version awareness (§D-1) | 67–74% [C]; v0.5.0 cannot ready a Deployment | `functions: {autoReady: {version: v0.5.0}}` → changes the **default** readiness of native nodes | **T1** |
| 23 | `functions.yaml` + `functionRef.name` single source (§I-7) | dangling-ref bug [V]; both spellings live [C] | `functions: {goTemplating: {name: ..., package: "xpkg.crossplane.io/.../function-go-templating:v0.12.0"}}` | **T1** |
| 24 | Aggregate connection Secret (§F-3) | 12% emit a Secret; official v2 pattern; F-4 is dead [C] | `connectionSecret: {name: "{derived.appName}-conn", keys: [{key: password, fromNode: db, detail: password}]}` | **T1** |
| 25 | XR status derivation (§F-1) | 28% emit a status doc; 85/117 XRDs have a status schema [C] | `status: [{to: status.dbArn, fromNode: {node: db, path: status.atProvider.arn}}]` | **T1** |
| 26 | `dependsOn:` ordering edge → sequencer (§E-1, §E-2) | E-1 ~15 sites, "highest-value single feature"; sequencer 62 files [C/I] | `dependsOn: [db]` on a node → one `function-sequencer` step from the transitive closure | **T1** |
| 27 | EnvironmentConfig source (§H-1) | 9–18% everywhere except Upbound (0) [C] | `environment: [{type: Reference, ref: {name: platform-defaults}}]` + `from: env.<key>` on any mapping | **T1** |
| 28 | Aggregated RBAC ClusterRole (§G-4) | uncounted; "#1 why-nothing-happens failure" [I] | `emit: {rbac: true}` — derived from every composed native GVK | **T1** |
| 29 | `missingkey=error` + `options` at input top level (§A-3 / F1) | only 3/381 opt in [C]; `<no value>` is schema-valid and passes every gate [V] | `emit: {options: [missingkey=error]}` **default on** | **T1** |
| — | **Escape mechanism** (per-field `rawTemplate`, per-node `rawPrelude`, whole-step `rawStep`) | 5% need a true escape [C] | `rawTemplate: '...'` / `rawPrelude: '...'` / `- {step: x, raw: {...}}` | **T1 mechanism** (enables every T3) |

### T2 — DSL-modelled, lower priority

| # | Pattern | Frequency | Blueprint DSL representation | Tier |
|---|---|---|---|---|
| 30 | Nested-XR node with **schema-aware** palette (§B-8) | 4.4% general, but Upbound's core architecture [C] | `gvk: {blueprintRef: xnetwork}` — populates `spec` from the other blueprint's XRD, validates scope. *The plain-GVK emitter is already T1; only the schema binding is T2.* | **T2** |
| 31 | Cross-Configuration label contract (§C-6) | 103 hits [C] | `emitsLabel: {"networks.acme.io/network-id": "{params.id}"}` + `externalRef: {field: subnetId, byLabel: {...}}` | **T2** |
| 32 | XRD reference-triad passthrough (§C-7) | 1 repo × 3 fields; generates ~60 XRD lines [C] | `exposeRef: {node: table, field: kmsKeyArn}` | **T2** |
| 33 | `Usage` object generation (§E-3) | 10 hits/3 fns + 25 hand-written/17 files [C] | `dependsOn: [{node: network, deletionOrder: true}]` | **T2** |
| 34 | Aggregated phase/status step (§D-5) | ~100 lines in 1 repo; 13 comps in another [C] | `phases: [{name: Ready, when: {allReady: [queue, deployment]}}, {name: Napping, when: {...}}]` | **T2** |
| 35 | External lookup node (§H-2) | 4–5.6%; v2 form has 0 published uses [C] | `lookups: [{name: appConfig, gvk: {...}, matchName: "{params.configName}", optional: true}]` → `requirements.requiredResources` | **T2** |
| 36 | `preconditions:` / `fail` (§F-6) | 7–13% [C] | `preconditions: [{when: {...}, message: "..."}]` | **T2** |
| 37 | `ClaimConditions` (§F-5) | 1% [C] | `conditions: [{type: NetworkResolved, when: {...}, trueReason: ..., falseReason: ...}]` | **T2** |
| 38 | Observe-only MR node (§B-9) | 10% [C] | `managementPolicies: ["Observe"]` on a node | **T2** |
| 39 | `DeriveFromCelQuery` readiness for `Object`s (§D-7) | 2% [C] | `readiness: {celQuery: "..."}` on `Object` nodes | **T2** |
| 40 | provider-kubernetes `Object` wrapper with child node (§B-7) | 351 occ, most-composed kind; 26% [C] | `gvk: {kind: Object}` + `manifest: {gvk: ..., fields: [...]}`; emitter knows the `status.atProvider.manifest` double hop | **T2** |
| 41 | Variant expansion (one blueprint → N Compositions) | 72–97% pairwise duplication measured in **five** repos [C] | `variants: {matrix: {cloud: [aws, gcp]}, labels: {awsblueprints.io/provider: "{cloud}"}}` | **T2** |
| 42 | P&T emission backend | 24% of the community corpus is still legacy `resources:` [C]; P&T 1,188 files GitHub [C] | `emit: {mode: patchAndTransform}` — same blueprint, different backend | **T2** |
| 43 | `source: FileSystem` output mode (§I-5) | 40 steps / 14 files [C]; **UNRESOLVED viability** | `emit: {templateSource: FileSystem}` → template files + ConfigMap + DeploymentRuntimeConfig | **T2** |
| 44 | Generic function-step node (§I-6) | KCL 666 files; **20% of the community corpus abandoned YAML** [C] | `steps: [{name: flavors, functionRef: {...}, input: {...}, after: environment}]` | **T2** |
| 45 | `ManagedResourceActivationPolicy` (§G-5) | 2/71 repos [C] | `emit: {mrap: true}` | **T2** |
| 46 | Output metadata presets + post-processing hook (§G-6) | ArgoCD annotations on 27% of community files [C] | `metadata: {presets: [appK8sIo, argocdSyncWave], annotations: {...}}` | **T2** |
| 47 | XRD `x-kubernetes-validations` passthrough | 5/117 XRDs [C] | `validations: [{rule: "...", message: "..."}]` — a **verbatim string list**, no GUI construction | **T2** |
| 48 | `additionalPrinterColumns` | 10/117 XRDs [C] | `printerColumns: [{name: URL, type: string, jsonPath: .status.url}]` | **T2** |
| 49 | Step-level `credentials` (§H-3) | 3% [C] | `steps[].credentials: [{name, secretRef}]` | **T2** |
| 50 | Hashed name to force re-creation (§P-20) | 2 sites [C] | `recreateOnChange: [params.principalArn]` → `sha256` name | **T2** |
| 51 | Inline `ready(X) or exists(self)` guard emission mode (§E-1) | ~15 sites [C] | `emit: {ordering: guards}` (alternative to `sequencer`) | **T2** |

### T3 — rawTemplate escape only

Every T3 is a place the GUI degrades to a text editor. Each is justified.

| # | Pattern | Frequency | Escape flavour | Why it cannot be structural |
|---|---|---|---|---|
| 52 | Arbitrary identity/format strings (§A-8) | 2 of 5 complex comps [I] | per-field `rawTemplate` | Cloud identity URI formats are unbounded and provider-private. There is no schema to enumerate them from. Cost: **low** — one field is a text box; the surrounding node stays structural. |
| 53 | Computed **kind** from data (§B-4) | 1 site [C] | per-node `rawTemplate` (whole doc) | A node with a data-derived kind has **no schema** at design time → no form, no field validation, no palette entry, no reference triad, no scope check. Cost: **high per instance, negligible in aggregate.** |
| 54 | Reference bound to a specific array element (§J-5) | **103/578 GCP (17.8%)**, 29/464 AWS (6.2%) [C] | per-field `rawTemplate` | The graph edge has no way to name `networks[3]`. *Mitigated:* when the array is a `forEach` source the edge **is** structural. The residue is refs into arrays the user does not loop over. Cost: **the single largest T3 by frequency, and 3× worse on GCP than AWS.** |
| 55 | Compound multi-resource readiness (§D-4) | 2/109 (2%) [C] | `rawTemplate` on the readiness property | Would require a boolean expression editor over the whole graph. 2/109 does not pay for it. Cost: **low.** |
| 56 | Aggregate-then-filter status (§F-2) | 1 site [C] | `rawPrelude` + a normal status mapping | "Collect one field from each loop item" is structural (§4 emits it). "Collect, then partition by a property of the item" is a comprehension. Cost: **low**, and the *simple* case is covered. |
| 57 | Derived collections / list partitioning (§J-3) | 3 of 5 complex comps [I] | **`rawPrelude`** (binds named variables) | This is data-shaping, not resource templating. A node graph that could express it would be a programming language. Cost: **medium and structural-in-the-bad-sense** — variables cross the raw/structural boundary, so the generator cannot type-check them and a rename in the prelude breaks downstream mappings silently. **This is the escape flavour that most needs a lint rule.** |
| 58 | Business logic (timestamp arithmetic, freshness policy) | ~120 lines in 1 repo [C] | `rawPrelude` | Same as 57. Cost: **low frequency, high line count where it appears.** |
| 59 | Nested templates `define`/`include` (§J-1) | 2%, 6 comps [C] | `rawStep` or a template-library include | Upstream's own example concedes the indentation problem. Cost: **low** — and the generator's `nindent` ownership removes the main reason people reach for it. |
| 60 | Recursive/meta template engines (§J-2) | **5%, 18 comps** [C] | **whole-step `rawStep`** | The composed apiVersion/kind are *data*. Nothing structural exists. Cost: **total for that step** — one opaque box on the canvas, no validation, no palette, no refactoring. Also a **warning sign**: people build these when the authoring tool is too rigid. |
| 61 | Claim/composite selector ternary (§J-4) | 1 site [C] | per-field `rawTemplate` | A ternary inside a field value. Cost: **negligible.** |
| 62 | `Context` write between steps (§I-4) | 1%, 12 occ/8 files [C] | `rawStep` | The generator owns step layout, so it can recompute the value in the consuming step rather than passing it. If a user genuinely needs cross-step context (e.g. feeding a third-party function), it is a raw step. Cost: **low**, but note this is a *design bet* — if the one-big-step emission mode is abandoned, this moves to T2. |
| 63 | Post-hoc mutation of another step's resources (KCL `PatchDesired`, P&T with no `base`, `function-cel-filter`) | P&T-no-base: upstream example + 17% run both fns [C]; cel-filter 19 files [C] | `rawStep` | A node graph has **no representation for "edit a node someone else made."** Cost: **medium** — see §7. |
| 64 | `toYaml`/`nindent` splicing of arbitrary sub-documents outside tags/labels | 17 + 18 community files (5%) [C] | per-field `rawTemplate` | The spliced sub-document has no schema binding, so the GUI cannot show or validate its fields. *Mitigated:* the tags/labels case (§A-9) is structural and is most of the volume. Cost: **low.** |
| 65 | Helm-wrapping the whole Composition | 1,123 backtick escapes across 2,087 lines in 1 repo [C] | **not supported** | compositionfactory cannot be the inner layer of someone else's templating system. Cost: **users who need Helm values inside compositions must use `variants:` (T2 #41) or leave.** Stated plainly rather than half-supported. |
| 66 | Importing / round-tripping existing go-templates | — | `rawTemplate` adoption only (Tier 1 `adopt`) | Proven infeasible [V]: TEXT nodes are not YAML, document shape is data-dependent, indentation is semantic, XRD comments are destroyed. Cost: **the honest 90% of the value at ~5% of the risk** — adopt captures the template verbatim as an opaque node. |

**T3 count: 15 patterns.** Two of them (54 and 60) are the only ones that cost a *whole node or step*.
The other thirteen degrade a single field or a prelude while the surrounding node stays structural.

---

## 4. Proposed blueprint DSL sketch

One blueprint exercising **every T1 pattern**, and the artifacts it emits. Numbers in `# T1-n`
comments map to §3.

### 4.1 The blueprint

```yaml
apiVersion: compositionfactory.io/v1alpha1
kind: Blueprint
metadata:
  name: xmicroservice

spec:
  target:
    crossplane: "v2.4"
    scope: Namespaced                                    # T1-20
    functions:                                           # T1-23
      goTemplating: {name: function-go-templating, package: "xpkg.crossplane.io/crossplane-contrib/function-go-templating:v0.12.0"}
      autoReady:    {name: function-auto-ready,    package: "xpkg.crossplane.io/crossplane-contrib/function-auto-ready:v0.5.0", version: v0.5.0}   # T1-22
      sequencer:    {name: function-sequencer,     package: "xpkg.crossplane.io/crossplane-contrib/function-sequencer:v0.3.0"}
      environmentConfigs: {name: function-environment-configs, package: "xpkg.crossplane.io/crossplane-contrib/function-environment-configs:v0.5.0"}
    emit:
      options: ["missingkey=error"]                      # T1-29
      rbac: true                                         # T1-28
      templateSource: Inline
      stepLayout: singleRender                           # see §5

  xrd:                                                   # T1-1
    group: platform.acme.io
    kind: XMicroservice
    plural: xmicroservices
    version: v1alpha1
    parameters:
      image:              {type: string, required: true}
      replicas:           {type: integer, default: 2}
      size:               {type: string, enum: [xs, sm, md], default: sm}
      host:               {type: string, description: "hostname; joined with env.domain"}
      providerConfigName: {type: string, default: default}
      tags:               {type: object, additionalProperties: {type: string}}
      database:
        type: object
        properties:
          enabled: {type: boolean, default: false}
          class:   {type: string,  default: db.t4g.micro}
      queues:
        type: array
        items:
          type: object
          properties:
            name: {type: string, required: true}
            fifo: {type: boolean, default: false}
    status:
      url:       {type: string}
      dbArn:     {type: string}
      queueUrls: {type: array, items: {type: string}}

  environment:                                           # T1-27
    - {type: Reference, ref: {name: platform-defaults}}   # provides: domain, region, dbSubnetIds

  defaults:                                              # T1-19
    providerConfigRef: {kind: ClusterProviderConfig, name: "{params.providerConfigName}"}
    managementPolicies: ["*"]
    location: {from: env.region}                          # emitted as `region` on AWS — see §6
    tags:                                                 # T1-8
      merge:
        - {from: params.tags}
        - {app: "{derived.appName}", managedBy: crossplane}

  valueMaps:                                             # T1-6
    size:
      key: {from: params.size}
      fallback: sm
      table:
        xs: {cpuReq: "25m",  memReq: "64Mi"}
        sm: {cpuReq: "100m", memReq: "128Mi"}
        md: {cpuReq: "500m", memReq: "512Mi"}

  derived:                                               # T1-7
    appName:
      fmt: "%s"
      inputs: [{from: xr.metadata.name}]
      sanitize: {lower: true, replace: {".": "-", "/": "-"}, maxLength: 63, trimSuffix: "-"}

  nodes:
    # ---- native workload -------------------------------------------------- T1-12
    - name: deployment
      gvk: {apiVersion: apps/v1, kind: Deployment}
      naming: explicit                                   # T1-16 (Service/Ingress reference it)
      nameFrom: derived.appName
      defaults: false                                    # no providerConfigRef on native kinds
      labels: {"platform.acme.io/service": "{derived.appName}"}
      dependsOn: [db]                                    # T1-26
      readiness: {condition: Available}                  # T1-21 (auto-ready v0.5.0 cannot do this)
      fields:
        - {to: spec.replicas,                                          from: params.replicas}
        - {to: spec.selector.matchLabels,                              literal: {"platform.acme.io/service": "{derived.appName}"}}
        - {to: spec.template.metadata.labels,                          literal: {"platform.acme.io/service": "{derived.appName}"}}
        - {to: spec.template.spec.containers[0].name,                  literal: app}
        - {to: spec.template.spec.containers[0].image,                 from: params.image}        # T1-3/4
        - {to: spec.template.spec.containers[0].resources.requests.cpu,    from: valueMaps.size.cpuReq}
        - {to: spec.template.spec.containers[0].resources.requests.memory, from: valueMaps.size.memReq}

    - name: service
      gvk: {apiVersion: v1, kind: Service}
      naming: explicit
      nameFrom: derived.appName
      defaults: false
      readiness: {fieldPresent: spec.clusterIP}          # T1-21
      fields:
        - {to: spec.selector, literal: {"platform.acme.io/service": "{derived.appName}"}}
        - {to: spec.ports[0], literal: {port: 80, targetPort: 8080}}

    - name: ingress
      gvk: {apiVersion: networking.k8s.io/v1, kind: Ingress}
      naming: explicit
      nameFrom: derived.appName
      defaults: false
      when: {path: params.host, op: isSet}               # T1-10
      readiness: always                                  # no LB controller assumed; see §2 D-2
      fields:
        - {to: spec.rules[0].host, fmt: "%s.%s", inputs: [{from: params.host}, {from: env.domain}]}
        - {to: spec.rules[0].http.paths[0], literal: {path: /, pathType: Prefix}}
        - {to: spec.rules[0].http.paths[0].backend.service.name, fromNodeName: service}   # T1-16 forces `service` explicit
        - {to: spec.rules[0].http.paths[0].backend.service.port.number, literal: 80}

    # ---- managed resources ------------------------------------------------
    - name: dbSubnetGroup
      gvk: {apiVersion: rds.aws.m.upbound.io/v1beta1, kind: SubnetGroup}
      when: {path: params.database.enabled, op: isTrue}
      fields:
        - {to: spec.forProvider.subnetIds, from: env.dbSubnetIds}

    - name: db
      gvk: {apiVersion: rds.aws.m.upbound.io/v1beta1, kind: Instance}
      when: {path: params.database.enabled, op: isTrue}
      refs:
        - {field: dbSubnetGroupName, to: dbSubnetGroup}  # T1-13 → matchControllerRef
      fields:
        - {to: spec.forProvider.engine,        literal: postgres}
        - {to: spec.forProvider.username,      literal: app}
        - {to: spec.forProvider.instanceClass, from: params.database.class}
        - {to: spec.forProvider.passwordSecretRef,                     # T1-17
           secretRef: {name: "{derived.appName}-db-password", key: password}}
      writeConnectionSecretToRef: {name: "{derived.appName}-db"}

    # ---- loop: two correlated resources per item -------------------------- T1-11
    - name: queue
      gvk: {apiVersion: sqs.aws.m.upbound.io/v1beta1, kind: Queue}
      forEach: {over: params.queues, as: q, name: "queue-{q.name}"}
      externalName: {fmt: "%s-%s", inputs: [{from: derived.appName}, {from: q.name}]}
      fields:
        - {to: spec.forProvider.fifoQueue, from: q.fifo}

    - name: queuePolicy
      gvk: {apiVersion: sqs.aws.m.upbound.io/v1beta1, kind: QueuePolicy}
      forEach: {over: params.queues, as: q, name: "queue-policy-{q.name}"}
      refs:
        - {field: queueUrl, to: queue, sameIteration: true}   # T1-14 → label stamp + matchLabels
      fields:
        - to: spec.forProvider.policy
          rawTemplate: |                                       # T3-52, in situ
            {{ printf "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":\"*\",\"Action\":\"sqs:SendMessage\"}]}" | quote }}

  connectionSecret:                                      # T1-24
    name: "{derived.appName}-conn"
    when: {path: params.database.enabled, op: isTrue}
    keys:
      - {key: host,     fromNode: db, detail: endpoint}
      - {key: username, fromNode: db, detail: username}
      - {key: password, fromNode: db, detail: password}
      - {key: url,      fmt: "https://%s.%s", inputs: [{from: params.host}, {from: env.domain}]}   # literal → b64enc

  status:                                                # T1-25
    - {to: status.url,   fmt: "https://%s.%s", inputs: [{from: params.host}, {from: env.domain}], omitEmpty: true}
    - {to: status.dbArn, fromNode: {node: db, path: status.atProvider.arn}, omitEmpty: true}       # T1-18
    - {to: status.queueUrls, collectFrom: {loop: queue, path: status.atProvider.url}}
```

### 4.2 The emitted Composition

```yaml
# generated by compositionfactory — edit the blueprint, not this file
apiVersion: apiextensions.crossplane.io/v1        # Composition is STILL v1 in Crossplane v2
kind: Composition
metadata:
  name: xmicroservice
  labels:
    crossplane.io/xrd: xmicroservices.platform.acme.io
spec:
  compositeTypeRef:
    apiVersion: platform.acme.io/v1alpha1
    kind: XMicroservice
  mode: Pipeline
  pipeline:
  - step: environment
    functionRef: {name: function-environment-configs}
    input:
      apiVersion: environmentconfigs.fn.crossplane.io/v1beta1
      kind: Input
      spec:
        environmentConfigs:
        - type: Reference
          ref: {name: platform-defaults}
  - step: render
    functionRef: {name: function-go-templating}
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      options: ["missingkey=error"]                 # top level, NOT under `inline` — the README is wrong
      inline:
        template: |
          {{- $xr     := .observed.composite.resource }}
          {{- $params := $xr.spec }}
          {{- $obs    := .observed.resources | default dict }}
          {{- $env    := index .context "apiextensions.crossplane.io/environment" | default dict }}
          {{- $pcName := dig "providerConfigName" "default" $params }}
          {{- $region := dig "region" "" $env }}
          {{- $domain := dig "domain" "" $env }}
          {{- $host   := dig "host" "" $params }}
          {{- $sizeTable := dict "xs" (dict "cpuReq" "25m" "memReq" "64Mi") "sm" (dict "cpuReq" "100m" "memReq" "128Mi") "md" (dict "cpuReq" "500m" "memReq" "512Mi") }}
          {{- $size   := get $sizeTable (dig "size" "sm" $params) | default (get $sizeTable "sm") }}
          {{- $appName := $xr.metadata.name | lower | replace "." "-" | replace "/" "-" | trunc 63 | trimSuffix "-" }}
          {{- $tags   := merge (dict "app" $appName "managedBy" "crossplane") (dig "tags" dict $params) }}
          ---
          apiVersion: apps/v1
          kind: Deployment
          metadata:
            name: {{ $appName | quote }}
            annotations:
              {{ setResourceNameAnnotation "deployment" }}
              {{- if eq (getResourceCondition "Available" (dig "deployment" dict $obs)).Status "True" }}
              gotemplating.fn.crossplane.io/ready: "True"
              {{- end }}
            labels:
              platform.acme.io/service: {{ $appName | quote }}
          spec:
            replicas: {{ dig "replicas" 2 $params }}
            selector:
              matchLabels:
                platform.acme.io/service: {{ $appName | quote }}
            template:
              metadata:
                labels:
                  platform.acme.io/service: {{ $appName | quote }}
              spec:
                containers:
                - name: app
                  image: {{ $params.image | quote }}
                  resources:
                    requests:
                      cpu: {{ $size.cpuReq | quote }}
                      memory: {{ $size.memReq | quote }}
          ---
          apiVersion: v1
          kind: Service
          metadata:
            name: {{ $appName | quote }}
            annotations:
              {{ setResourceNameAnnotation "service" }}
              {{- if dig "service" "resource" "spec" "clusterIP" "" $obs }}
              gotemplating.fn.crossplane.io/ready: "True"
              {{- end }}
          spec:
            selector:
              platform.acme.io/service: {{ $appName | quote }}
            ports:
            - port: 80
              targetPort: 8080
          {{- if $host }}
          ---
          apiVersion: networking.k8s.io/v1
          kind: Ingress
          metadata:
            name: {{ $appName | quote }}
            annotations:
              {{ setResourceNameAnnotation "ingress" }}
              gotemplating.fn.crossplane.io/ready: "True"
          spec:
            rules:
            - host: {{ printf "%s.%s" $host $domain | quote }}
              http:
                paths:
                - path: /
                  pathType: Prefix
                  backend:
                    service:
                      name: {{ $appName | quote }}
                      port:
                        number: 80
          {{- end }}
          {{- if dig "database" "enabled" false $params }}
          ---
          apiVersion: rds.aws.m.upbound.io/v1beta1
          kind: SubnetGroup
          metadata:
            annotations:
              {{ setResourceNameAnnotation "dbSubnetGroup" }}
          spec:
            managementPolicies: ["*"]
            providerConfigRef:
              kind: ClusterProviderConfig
              name: {{ $pcName | quote }}
            forProvider:
              region: {{ $region | quote }}
              subnetIds:
                {{- dig "dbSubnetIds" list $env | toYaml | nindent 8 }}
              tags:
                {{- $tags | toYaml | nindent 8 }}
          ---
          apiVersion: rds.aws.m.upbound.io/v1beta1
          kind: Instance
          metadata:
            annotations:
              {{ setResourceNameAnnotation "db" }}
          spec:
            managementPolicies: ["*"]
            providerConfigRef:
              kind: ClusterProviderConfig
              name: {{ $pcName | quote }}
            writeConnectionSecretToRef:
              name: {{ printf "%s-db" $appName | quote }}      # name only — no namespace in v2
            forProvider:
              region: {{ $region | quote }}
              engine: postgres
              username: app
              instanceClass: {{ dig "database" "class" "db.t4g.micro" $params | quote }}
              passwordSecretRef:
                name: {{ printf "%s-db-password" $appName | quote }}
                key: password
              dbSubnetGroupNameSelector:
                matchControllerRef: true
              tags:
                {{- $tags | toYaml | nindent 8 }}
          {{- end }}
          {{- range $i, $q := dig "queues" list $params }}
          ---
          apiVersion: sqs.aws.m.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              {{ setResourceNameAnnotation (printf "queue-%s" $q.name) }}
              crossplane.io/external-name: {{ printf "%s-%s" $appName $q.name | quote }}
            labels:
              platform.acme.io/queue: {{ $q.name | quote }}    # stamped because an edge crosses the loop
          spec:
            managementPolicies: ["*"]
            providerConfigRef:
              kind: ClusterProviderConfig
              name: {{ $pcName | quote }}
            forProvider:
              region: {{ $region | quote }}
              fifoQueue: {{ dig "fifo" false $q }}
              tags:
                {{- $tags | toYaml | nindent 8 }}
          ---
          apiVersion: sqs.aws.m.upbound.io/v1beta1
          kind: QueuePolicy
          metadata:
            annotations:
              {{ setResourceNameAnnotation (printf "queue-policy-%s" $q.name) }}
          spec:
            managementPolicies: ["*"]
            providerConfigRef:
              kind: ClusterProviderConfig
              name: {{ $pcName | quote }}
            forProvider:
              region: {{ $region | quote }}
              policy: {{ printf "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":\"*\",\"Action\":\"sqs:SendMessage\"}]}" | quote }}
              queueUrlSelector:
                matchControllerRef: true
                matchLabels:
                  platform.acme.io/queue: {{ $q.name | quote }}
          {{- end }}
          {{- if dig "database" "enabled" false $params }}
          ---
          apiVersion: v1
          kind: Secret
          metadata:
            name: {{ printf "%s-conn" $appName | quote }}
            annotations:
              {{ setResourceNameAnnotation "connection" }}
          {{- $dbc := dig "db" "connectionDetails" dict $obs }}
          {{- if $dbc }}
          data:
            host: {{ index $dbc "endpoint" }}                  # already base64 — verbatim
            username: {{ index $dbc "username" }}
            password: {{ index $dbc "password" }}
            url: {{ printf "https://%s.%s" $host $domain | b64enc }}   # literal — must be encoded
          {{- else }}
          data: {}
          {{- end }}
          {{- end }}
          ---
          apiVersion: platform.acme.io/v1alpha1                # same GVK + NO resource-name annotation
          kind: XMicroservice                                  #   => status MERGE (fn.go), not a nested XR
          status:
            {{- if $host }}
            url: {{ printf "https://%s.%s" $host $domain | quote }}
            {{- end }}
            {{- with dig "db" "resource" "status" "atProvider" "arn" "" $obs }}
            dbArn: {{ . | quote }}
            {{- end }}
            queueUrls:
            {{- range $i, $q := dig "queues" list $params }}
            {{- with dig (printf "queue-%s" $q.name) "resource" "status" "atProvider" "url" "" $obs }}
            - {{ . | quote }}
            {{- end }}
            {{- end }}
  - step: sequence
    functionRef: {name: function-sequencer}
    input:
      apiVersion: sequencer.fn.crossplane.io/v1beta1
      kind: Input
      rules:
      - sequence: ["db", "deployment"]                # from `dependsOn: [db]` only
  - step: auto-ready
    functionRef: {name: function-auto-ready}
```

### 4.3 Side artifacts emitted from the same blueprint

- **`definition.yaml`** — the XRD: `apiextensions.crossplane.io/v2`, `scope: Namespaced` **explicitly**
  (omitting it makes the server default `Namespaced` and `crossplane xrd convert` default
  `LegacyCluster` [V]), `defaultCompositionUpdatePolicy: Automatic` emitted explicitly to avoid a
  permanent GitOps diff [V], no `claimNames`, no `connectionSecretKeys` (both CEL-rejected), and every
  `default:`/`enum:`/`required:` from `parameters:` — written **once**.
- **`functions.yaml`** — four `pkg.crossplane.io/v1 Function` objects with fully-qualified packages;
  `functionRef.name` in the Composition is generated from the same table (T1-23).
- **`rbac.yaml`** — one aggregated ClusterRole covering `apps/v1 Deployment`, `v1 Service`,
  `networking.k8s.io/v1 Ingress`, `v1 Secret`, labelled `rbac.crossplane.io/aggregate-to-crossplane: "true"`.
- **`examples/xmicroservice.yaml`** — a sample XR (61/71 Upbound repos ship `examples/`).
- **Not emitted:** `kustomization.yaml`, `argocd.argoproj.io/tracking-id`, any generated-at annotation.
  Provenance goes in YAML comments only [V].

### 4.4 Six things in that output that are arguments, not facts

1. **One `render` step, not one step per node.** 53.5% of real go-templating steps emit exactly one
   resource [C], so the idiomatic shape is the opposite of what is emitted here. The counter-argument
   is that a single step makes every `.observed.resources` read available everywhere without ordering
   constraints, and makes the emitted file diffable as one unit. Both modes should exist
   (`emit.stepLayout`); the default is arguable. **See §5.**
2. **`sequencer` rather than inline `ready(X) or exists(self)` guards.** Upbound write the guards by
   hand ~15 times [C]; sequencer is used in 62 files GitHub-wide [C]. Sequencer gets the
   "don't delete on flap" semantics right by construction; guards keep everything in one step. T2 #51
   keeps the alternative.
3. **`readiness: always` on the Ingress.** This is a lie the user is choosing. It is also what
   *every* real Ingress node in the corpus does, because `status.loadBalancer.ingress` never populates
   without an LB controller. The GUI should warn.
4. **`options: [missingkey=error]` on by default.** Only 3/381 real compositions opt in [C]. The
   justification is that `<no value>` is a *legal string* that passes validate → render → validate with
   exit 0 [V]. This will surface latent bugs in adopted templates; that is the point.
5. **`region` hard-coded as the location field.** Correct on AWS, **invalid on GCP** — see §6. The
   blueprint says `defaults.location`; the emitter resolves the actual field name per CRD.
6. **The status document merges rather than replaces.** `mergo.WithOverride` means several pipeline
   steps can each contribute status keys. If `stepLayout: perNode` is chosen, this becomes load-bearing.

---

## 5. Pipeline modelling

### 5.1 The canvas is a resource DAG; the pipeline is a mostly-derived lane

The graph the user edits is **nodes = composed resources, edges = references and ordering**. The
pipeline is a second, horizontal lane below it. Most of that lane is **derived and undraggable**:

| Step | Origin | User-visible? |
|---|---|---|
| `environment` | `environment:` block (T1-27) | as a blueprint section, not a node |
| `render` | all resource nodes | the canvas itself |
| `sequence` | transitive closure of `dependsOn` edges (T1-26) | as edges, not a step |
| `auto-ready` | always appended (T1-22) | a toggle |
| a lookup step | `lookups:` (T2 #35) | **yes** — a dashed node with a list output port |
| a validation step | `preconditions:` (T2 #36) | as a blueprint section |
| a generic function step | `steps:` (T2 #44) | **yes** — an opaque box in the lane |

### 5.2 Ordering rules the generator must enforce

Taken verbatim from the multi-step analysis of the 1,409-file corpus:

1. Context producers (`function-environment-configs`, extra/required resources, custom `Context`)
   **before** any step that reads context.
2. Validation/gate steps **after** context producers, **before** resource renders.
3. Resource renders in dependency order when a later template reads an earlier resource's `.observed`
   state.
4. Status/condition derivation **second-to-last** (it reads `.observed.resources` for everything).
5. `function-auto-ready` **last, always.**

Real evidence for all five: the estenrye 10-step pipeline
(`environmentConfigs` → four `validate-*` → three `create-*` → `status-update` → `auto-ready`)
<https://github.com/estenrye/flux-platform-src/blob/main/applications/crossplane-resources/delegated-hosted-zone-aws/composition.yaml>
and the Delivery Hero 9-step pipeline where step 8 must be last-before-auto-ready because it needs
every prior step's resource to exist in `observed`.

### 5.3 Three emission layouts

| Layout | Steps | Matches | Trade-off |
|---|---|---|---|
| **`singleRender`** (default) | env + render + sequence + auto-ready = 4 | the 2-step (49%) / 3-step (23%) mode [C] | Every observed read is available everywhere; one diffable unit. Diverges from the 53.5% one-resource-per-step norm. |
| **`perNode`** | env + N renders + sequence + auto-ready | the dominant real authoring style [C] | Node ↔ step is 1:1, which is what a canvas *looks* like. Costs N repeated preludes and makes step ordering a real constraint the tool must solve. |
| **`embeddedFunction`** | 2 steps, 16 lines, logic in `functions/<step>/compose.yaml.gotmpl` | Upbound's current house style: **46 of 84 pipeline compositions are exactly this shape** [C] | What `up project build` / `crossplane project` consume. Requires a Project manifest; the function package name is mechanically `<org>-<repo><step>` and must be reproduced exactly. |

Real embedded example:
<https://github.com/upbound/configuration-aws-securitygroup/blob/main/functions/xsecuritygroups/compose.yaml.gotmpl>
(and the 17-line `composition.yaml` next to a 624-line `main.k` emitting 23 resources:
<https://github.com/upbound/configuration-caas/blob/main/apis/composition.yaml>)

### 5.4 Non-go-templating steps on the canvas

A generic **function-step node**: a box in the pipeline lane with `{name, functionRef, input (opaque
YAML), after: [step]}` and no resource output ports. It is *not* parsed. Justification:

- KCL is the **#3 function** by GitHub file count (666) and Upbound's house language (34 of 51
  embedded functions) [C]. KCL can do four things go-templating cannot: post-hoc mutation
  (`target: PatchDesired`), `assert` with a real error message, typed reusable schemas, and
  OCI-distributed shared logic (`source: oci://...`).
- **20% of the community corpus has abandoned declarative composition YAML entirely** for an in-house
  Go function fed a ConfigMap of settings [C]. A generator that cannot emit that shim cannot express
  the endgame the two most mature shops in the corpus reached.
- P&T is the **most common function overall** (1,188 files GitHub-wide vs go-templating's 914) [C],
  and 17% of the go-templating corpus runs both in one pipeline [C].

### 5.5 Inter-step data channels, and the generator's policy

| Channel | Occurrences / files [C] | Generator policy |
|---|---|---|
| `.observed.resources.<name>` | **422 / 121** | The workhorse. Always `dig` + default + nil guard. |
| `index .context "<key>"` | 130 / 75 | Read-only for `environment` and `requiredResources`. |
| `apiextensions.crossplane.io/environment` | 80 / 50 | First-class (T1-27). |
| XR `status.atFunction.*` scratchpad | function-cidr, function-shell | Never generate. Note the fieldpath escaping rule (`apiextensions\.crossplane\.io/extra-resources`). |
| `.desired.resources.<name>` | **1 / 1** | **Never generate.** |
| custom `Context` write | 12 / 8 | T3 (§I-4). |

**The composed-resource name is the universal join key across every function** — go-templating's
annotation (1,800 occurrences / 601 files [C]), KCL's `krm.kcl.dev/composition-resource-name`,
P&T `resources[].name`, sequencer `rules[].sequence[]`, cel-filter `filters[].name`,
status-transformer `resources[].name`. **Node identity in the canvas must be exactly this string, and
it must be stable across regeneration.** Renaming a node is a breaking change to the live cluster.

**Known limitation:** sequencer rules reference resource names literally, so **a node inside a
`forEach` cannot be sequenced** — its names are data-dependent. Ordering edges into or out of a loop
node must fall back to E-1 guards or be refused at graph-edit time.

---

## 6. Portability verdict

**Does the reference-inference layer survive GCP? Yes — and GCP is cleaner than AWS.** Every
divergence found traces to a *provider-config decision* (a Go file in the provider repo), never to
the generator.

### 6.1 What is universal — hard-code it

These are generated by **upjet**, not by the provider, so they hold for every upjet provider (aws,
gcp, azure, and the community set):

| Invariant | GCP evidence | AWS evidence |
|---|---|---|
| Every kind ships cluster + `.m.` namespaced | 405/405 pairs | 405/405 equivalent; EC2 102+102 [V] |
| Ref ⇄ Selector are always siblings | 578/578 both ways | 464/464 both ways |
| `NamespacedReference` shape `{name, namespace, policy{resolution,resolve}}`, `required:[name]` | 541 obj + 37 array | 409 + 55 |
| `NamespacedSelector` shape `{matchControllerRef, matchLabels, namespace, policy}` | 578 | 464 |
| Description grammar (corrected regexes below) | 1156/1156 | 928/928 |
| `"a list of"` ⇔ slice | 37/37 | 55/55 |
| Namespaced spec envelope: no `deletionPolicy`, `providerConfigRef` requires `kind`, `writeConnectionSecretToRef` name-only, `managementPolicies` default `["*"]` | 405/405 | 279/279 |
| Status envelope | 1 distinct shape | byte-identical to GCP |
| CEL on MRs = required-parameter templates only | 454/454 | 206/206 |
| No `oneOf`/`anyOf`/`allOf`/`not`/real enums/formats anywhere in `spec` | 0 each | 0 each |
| Cluster and namespaced descriptions byte-identical | 1156/1156 | same generator |

**Corrected regexes** (validated 2,084/2,084 across both providers — the AWS-derived pair fails 2/928
because `friendlyTypeDescription` omits `" in <group>"` for same-package refs and appends free text):

```
^(?P<plural>References?) to (?:a )?(?P<Kind>\w+)(?: in (?P<group>\w+))? to populate (?P<field>\w+)\.(?:\n[\s\S]*)?$
^Selector for (?:(?P<list>a list of )|a )?(?P<Kind>\w+)(?: in (?P<group>\w+))? to populate (?P<field>\w+)\.(?:\n[\s\S]*)?$
```

Origin: two `fmt.Sprintf` calls in <https://github.com/crossplane/upjet/blob/main/pkg/types/reference.go>.
That is a single point of generation — **pin a golden test that parses one CRD per provider family and
asserts 100%**, converting the residual risk from "an unspecified convention we depend on" into a
build-time assertion.

**Method correction, and it matters:** detection must be **shape-first**, confirmed by name suffix
**and** a parsing description. Name-only admits 22 GCP + 4 AWS false positives (`nodeSelector`,
`configMapRef`, `localTrafficSelector`, `secretKeyRef`); shape-only admits `iam.Role.inlinePolicy`
(AWS) and `compute.Router.md5AuthenticationKeys` (GCP). **The 3-way conjunction is exact on both
corpora: 1,042 true positives, 0 false positives, 0 false negatives.**

### 6.2 What must be per-provider data

| # | Thing | Why | Failure mode if hard-coded |
|---|---|---|---|
| 1 | **Value-field resolution** | The `{stem, stem+"s"}` rule is **34 hand-written `RefFieldName:` overrides in `provider-upjet-aws/config/`**; GCP ships **2 lines total**. Use the `to populate <field>` capture instead — right 578/578 on GCP and 464/464 on AWS | Silently generalises an AWS-only artefact. **Supersedes the grounding doc's §2.2 rule**, which was measured on EC2 only |
| 2 | **The location/scoping field** | `region` is OpenAPI-`required` on **246/279 AWS MRs and 0/405 GCP MRs**. GCP requires `location` (57), `region` (38), `zone` (11); `project` is present on 319/405 and **never required** (it falls back to `ClusterProviderConfig.spec.projectID`) | A generator that emits `region:` unconditionally **produces schema-invalid GCP compositions** and omits `location`/`zone` where they are mandatory |
| 3 | **ProviderConfig object schema + `credentials.source` enum** | Fully disjoint. GCP requires `projectID`; AWS does not. GCP sources: `None, Secret, AccessToken, ImpersonateServiceAccount, InjectedIdentity, Environment, Filesystem, Upbound`. AWS: `None, Secret, IRSA, WebIdentity, PodIdentity, Upbound`. Only `reconciliationPolicy` is shared | Wrong credential form in the palette |
| 4 | **Short-group → API-group mapping** | The description's `group` is a *short* group (`compute`, `cloudplatform`); full group is `<short>.<provider>.[m.]upbound.io`. **The description never carries the scope** | Unresolvable edges |
| 5 | **Package-set discovery** | **34.1% of GCP refs cross API groups** (`compute` is the target of 222 of 578) vs 30.0% on AWS. 54 distinct target groups, 128 distinct target Kinds | An editor that loads only the packages named in the blueprint **fails to resolve a third of its edges**. Load the family, or resolve lazily |

### 6.3 What changes for the GUI on GCP

- **The T3 escape gets bigger.** Refs nested under an array: **17.8% on GCP vs 6.2% on AWS** — nearly
  3×. Deepest observed is 8 levels of nesting through two arrays.
- **Secret-key selector ports get denser.** 115 on GCP vs 23 on AWS — 5×. That port type must be
  first-class (T1-17), not an afterthought.
- **`ClusterProviderConfig`/`ProviderConfig` naming collision is identical** on both — the same
  off-by-one exception in the scope/group counts.
- **`project` needs no field at all** on GCP, which means the "where does this live" section of the
  form is provider-shaped, not a fixed `region` box.

**Verdict:** the reference-inference layer is portable. Ship it as an explicit, testable, data-driven
layer with a per-provider table for the five items above, and a golden regression test on the
description grammar. **Do not prototype against a second upjet provider and call it validated** — GCP
and AWS share the same generator. The real portability question is a **non-upjet** provider
(provider-kubernetes, provider-helm, provider-terraform), where the `*Ref`/`*Selector` convention is
not guaranteed to exist at all. That remains untested.

---

## 7. What would break the DSL

The real-world patterns that do **not** fit a node graph, with the honest cost of each.

### 7.1 Meta-compositions (a template that renders arbitrary GVKs from XR data)

`livewyer-ops/crossplane-configuration-aws-elemental`'s `Workflow` is a recursive Go-template
interpreter: `apiVersion` and `kind` come out of `spec.steps[].resources[].spec`, and dependencies are
wired by dotted path through `setNestedValue`/`digNestedPath`.
<https://github.com/livewyer-ops/crossplane-configuration-aws-elemental/blob/f7b436fa2de079f1a8d1be00095abf66073236ab/apis/workflow/composition.yaml>

**Frequency:** 18/381 (5%) contain `set`/`mergeOverwrite`/`regexSplit` engines [C].
**Cost: total, for that step.** One opaque box on the canvas. No palette, no field forms, no
reference triads, no scope validation, no diffable refactoring. Nothing structural exists to model.
**And it is a signal, not just a gap:** people build meta-compositions when the authoring tool is too
rigid. If compositionfactory's structural path is good enough, this population shrinks.

### 7.2 Derived collections computed from XR arrays

`cujarrett/homelab` partitions `$provides` into `$appRoles` / `$delegatedScopes` and derives
`$entraNeeded` from the lengths; Giant Swarm compute a 300-line CIDR/blackhole route-set
comprehension; Upbound's network function aggregates external names across loop-produced subnets then
filters by type.

**Cost: medium, and structurally corrosive.** The `rawPrelude` escape works — but the variables it
binds cross the raw/structural boundary. The generator cannot type-check them, cannot rename them
safely, and cannot tell the user which downstream mappings break when the prelude changes. **This is
the one escape that needs a lint rule** (declare the prelude's output variables and their types in the
blueprint, so at least the boundary is checked).

### 7.3 References into a specific array element

`spec.forProvider.networks[3].networkRef` cannot be drawn as an edge — the graph has no way to name
the index. **103/578 GCP refs (17.8%)** and 29/464 AWS refs (6.2%) sit under an array [C].

**Cost: the largest T3 by frequency, and 3× worse on GCP.** Mitigated when the array is a `forEach`
source (then the edge is per-item and fully structural). The residue is refs into arrays the user
treats as a single opaque value — a per-field `rawTemplate` containing a literal index that nothing
validates.

### 7.4 Data-derived kinds

`kind: SecurityGroup{{ title $rule.type }}Rule`
<https://github.com/upbound/configuration-aws-securitygroup/blob/main/functions/xsecuritygroups/compose.yaml.gotmpl>

**Frequency: 1 site [C].**
**Cost: high per instance.** A node whose kind is unknown at design time has no schema → no form, no
field validation, no palette entry, no reference triad, no namespace/scope check, no RBAC derivation,
no MRAP entry. Everything about it is raw. The tool should offer the alternative (two sibling nodes
each with `when:`) and only fall back to raw if the kind set is genuinely open.

### 7.5 Post-hoc mutation of another step's resources

Three real mechanisms: KCL `target: PatchDesired` / `PatchResources`
(<https://github.com/crossplane-contrib/function-kcl/blob/main/examples/patch_desired/patching_multiple/composition.yaml>);
P&T resource entries with **no `base:`** attaching by name to a go-templating-produced resource
(<https://github.com/crossplane-contrib/function-patch-and-transform/blob/main/example/multistep/composition.yaml>);
`function-cel-filter` post-filtering desired resources produced by earlier steps.

**Cost: medium.** **A node graph has no representation for "edit a node someone else made."** The
node exists in one place and is mutated in another; drawing that is a second, invisible edge class.
The DSL's answer is `when:` on the node itself (better UX than cel-filter, per the same brief) and
`rawStep` for the rest. The gap is real for teams adopting compositionfactory into an existing
multi-function pipeline.

### 7.6 The Composition being someone else's template output

Delivery Hero's `asyncactor-sqs` is a **Helm template that emits a Go template** —
**1,123 backtick escapes across 2,087 lines** [C], with 7 Helm `if` blocks conditionally including or
excluding whole pipeline steps.
<https://github.com/deliveryhero/asya/blob/main/deploy/helm-charts/asya-crossplane/templates/composition-sqs.yaml>

**Cost: exclusion.** compositionfactory cannot be the inner layer of another templating system without
inheriting its escaping. The answer is `variants:` (T2 #41) — one blueprint plus a values matrix
producing N Compositions — which is what VSHN (6 Jsonnet re-renders), AWS Labs (49 label-selected
compositions) and livewyer (aws/ + gcp/ trees, 77–97% identical) already do by other means. **Teams
that need runtime Helm values inside a composition are not served, and that should be said out loud.**

### 7.7 Round-tripping existing compositions

Proven infeasible by actually parsing the user's production template with `text/template/parse` [V]:
parsing fails outright without replicating the entire FuncMap; TEXT nodes are non-YAML fragments
(`"spec:\n  forProvider:\n    region:"` is a dangling mapping key); document shape is data-dependent
(three `with` nodes ⇒ 2³ possible key sets); indentation is semantic and lives in TEXT; and XRD
comments are destroyed by parse → model → re-emit.

**Cost: accepted, and cheap.** Ship `adopt` (Tier 1): map everything *structured* (name,
`compositeTypeRef`, pipeline steps, `functionRef` names, the whole XRD `spec`) and capture each
go-templating template verbatim as an opaque `rawTemplate` node. Losslessly byte-reproducible.
**Declare round-tripping a non-goal in v1 and say so loudly** — every comparable system (Terraform,
cdk8s, Backstage, Kratix, Crossplane Project) is one-directional, and that is not an accident.

### 7.8 Ordering into or out of a loop

`function-sequencer` rules name resources literally. A node inside a `forEach` has data-dependent
names, so it **cannot appear in a sequence rule**.
**Cost: low but sharp.** The graph editor must refuse an ordering edge crossing a loop boundary, or
silently fall back to E-1 guards (which *can* be templated per-item). Refusing is better; silently
changing the emission strategy is how tools lose trust.

### 7.9 The two things that would invalidate the emitter target entirely

- **Open issue #513, "Templating in the Style of a RunFunctionResponse"** — argues the
  YAML-document-with-magic-annotations output format is itself the problem. If it lands, the emitter
  target changes shape. Worth watching, not worth pre-empting.
- **`upjet/pkg/types/reference.go` changing.** One file, two `fmt.Sprintf` calls, one
  `friendlyTypeDescription`. A change there breaks the reference-inference layer for **every upjet
  provider at once** — which is also why a single golden test detects it (§6.1).

---

## Appendix — full list of UNRESOLVED items

| # | Topic | Brief A | Brief B | Disposition |
|---|---|---|---|---|
| U1 | Dominant reference spelling | `cs-upbound`: refs are "~99% `*Selector`" (160 selector occ) | `cs-gotemplating`: `<x>Ref: {name}` 56% / 972 occ vs selectors 8%/6% | Different denominators — the 972 includes `providerConfigRef`/`secretRef`/native-K8s name refs. **Model both** |
| U2 | `EnvironmentConfig` relevance | `cs-upbound`: **0 across 71 repos**, "do not spend DSL surface on it" | `cs-v2-native` 10%, `cs-other-functions` 14.3%, `cs-community` 9% | Upbound is the outlier. **T1**, but as a blueprint section + mapping source, not a node type |
| U3 | `CompositeConnectionDetails` on v2 | `cs-gotemplating`: the function "refuses it for v2 XRs" | `cs-v2-native` + grounding [V]: parsed, then **silently ignored** by the publisher | 2-vs-1 → **silent no-op**. Generator refuses to emit it for a v2 XRD |
| U4 | `source: FileSystem` viability | grounding [V/D]: "`Inline` is the only viable source" | `cs-community`: the ConfigMap + `DeploymentRuntimeConfig` form is "the cleanest generated artifact shape in the corpus" | Both true. **T2 output mode**, flagged |
| U5 | P&T `match` transform usage | `cs-other-functions`: **0 occurrences in 1,409 files** | `cs-upbound`: `type: match` 10 times | Different corpora. Keep `match` out of T1; T3 if it appears |
| U6 | Nested-XR importance | `cs-v2-native`: 4.4%, "a v2 feature, not v1" | `cs-upbound`: 104 imports, the core architectural move | Split: plain-GVK emitter **T1** (free), schema-aware palette **T2** |
| U7 | Share of ready annotations that are bare `"True"` | `cs-gotemplating`: 218/226 = 96% | `cs-v2-native`: 93/109 = 85% | Different corpora, same conclusion |
| U8 | Ref value-field resolution rule | grounding [V]: `{stem, stem+"s"}` = 172/172 on EC2 | `cs-gcp-portability`: `stem+"s"` fires 34× AWS / 0× GCP; use the description capture | **Superseded** — the description capture is right 1,042/1,042 across both providers |
| U9 | EC2 schema max depth | `crd-schema-shape` [V]: 7 | `canvas-ux` [V]: 9 | Counting convention. Immaterial: design for depth 5, raw-YAML escape below |
