# Community & enterprise platform repos — what production Crossplane actually looks like

**Survey area:** real organisations' public Crossplane platform repos (not vendor demos).
**Method:** GitHub code search (authenticated) → 26 candidate repos cloned → 21 kept that
contain ≥2 Compositions → **332 Composition files** greped/parsed at scale with Python.
Corpus manifest: `scratchpad/cs-comps.txt`. All counts below are over those 332 files unless stated.

---

## What this means for the DSL — 5 bullets

1. **The DSL's centre of gravity is a 1–2 step pipeline of "read XR field → default → derive name → emit one resource".**
   119/332 compositions have exactly 1 pipeline step, 85 have 2, only 22 have >3. Across the 78 templated
   compositions there are **1,189 go-template variable assignments, 369 `| default`, 152 `printf`** —
   i.e. ~15 "field-with-default" bindings per composition. Model *this* first-class and you cover the bulk of the corpus.

2. **Loops over XRD arrays must be a first-class node, because the absence of one is the single largest
   source of hand-written garbage in the corpus.** `ims-platform-dev/portal-kombat`'s security-group composition
   is **2,264 lines of which 2,099 (92%) are 40 hand-expanded "rule slots"** — 20 ingress + 20 egress
   `SecurityGroupRule` blocks that are byte-identical apart from the array index, with a header comment
   admitting *"YAML Array Limitation: Uses pre-defined rule slots (0-19) with Optional policy to skip unused slots."*
   The go-templating repos that *do* loop (`pavedplane`, `openkubes`, Delivery Hero) show the target shape:
   `range` + per-item resource name + per-item external-name + label-selector wiring.

3. **Per-environment / per-region variance is NOT expressed inside compositions — it is expressed by
   emitting N compositions.** Only **2/332** files branch on an environment/tier field. Instead:
   VSHN re-renders the same composition 6× from Jsonnet (one golden dir per environment, differing only in
   ConfigMap values); AWS Labs stamps `awsblueprints.io/environment: dev` on **49 compositions** and selects
   with `compositionSelector.matchLabels`; livewyer ships `compositions/aws/` and `compositions/gcp/` trees that
   are 77–97% identical. **A blueprint + a values file → N composition files is the shape the real world already uses.**
   Config that *does* vary at runtime goes into `EnvironmentConfig` (31/332 files, 3 layered configs in livewyer).

4. **Cross-cutting propagation is a fixed, small, universal set — make it a checkbox, not templating.**
   `providerConfigRef` 219/332 (65%), `deletionPolicy` 96 (29%), `region` 81 (24%), tags 64 (19%),
   `managementPolicies` 37 (11%). Giant Swarm and AWS Labs both factor it into a named patchSet or KCL preamble
   applied to *every* composed resource; AWS Labs standardises a required XRD block `spec.resourceConfig`
   `{providerConfigName, region, deletionPolicy, name, tags}`. Readiness annotation
   (`composition-resource-name` 237 occurrences, `gotemplating…/ready`, `krm.kcl.dev/ready`) is equally mechanical.

5. **Two escape hatches are needed, not one: (a) per-field raw template, (b) "this whole step is a custom
   function / an external template file".** 66/332 compositions (20%) — including the two most mature shops,
   **VSHN AppCat (52) and modelplane (14)** — have *abandoned declarative composition YAML entirely*: the
   Composition is a 12–130 line shim that hands a ConfigMap of settings to an in-house Go function.
   14 more use `source: FileSystem` + a ConfigMap-mounted template dir (Konflux/Red Hat, livewyer) with
   Helm-style `{{ define }}` partials. A generator that can only emit inline templates cannot express the
   endgame these teams reached.

---

## 1. The corpus: 21 real repos, scale, and functions

| Repo | Comps | XRDs | Functions in use | Packaging |
|---|---|---|---|---|
| [vshn/component-appcat](https://github.com/vshn/component-appcat) | 62 | 53 | in-house `function-appcat` only (version-pinned name) | Jsonnet/Commodore generator → 6 env golden dirs |
| [awslabs/crossplane-on-eks](https://github.com/awslabs/crossplane-on-eks) | 58 | 33 | patch-and-transform (mostly native `resources:`) | Kustomize (33 kustomization.yaml) + 3 xpkg |
| [stuttgart-things/crossplane](https://github.com/stuttgart-things/crossplane) | 36 | 32 | go-templating, auto-ready, kcl, environment-configs | 31 Crossplane Configuration packages |
| [platformplane/catalog-crossplane](https://github.com/platformplane/catalog-crossplane) | 28 | 28 | go-templating + auto-ready | 1 xpkg, v1+v2 side by side |
| [wiggitywhitney/cluster-whisperer](https://github.com/wiggitywhitney/cluster-whisperer) | 20 | 20 | go-templating, auto-ready | — |
| [modelplaneai/modelplane](https://github.com/modelplaneai/modelplane) | 14 | 14 | 14 bespoke in-house functions (1 per XRD) | — |
| [giantswarm/crossplane-gs-apis](https://github.com/giantswarm/crossplane-gs-apis) | 13 | 13 | kcl ×31 steps, auto-ready ×24, p&t ×7, in-house cidr/network-discovery | Go `crossbuilder` compiler → YAML; kustomize |
| [shlapolosa/health-service-idp](https://github.com/shlapolosa/health-service-idp) | 13 | 14 | go-templating, auto-ready | 36 ArgoCD Applications |
| [0xayf/homelab-idp](https://github.com/0xayf/homelab-idp) | 13 | 13 | go-templating | 15 Helm charts |
| [livewyer-ops/gardener-allotment](https://github.com/livewyer-ops/gardener-allotment) | 12 | 1 | environment-configs, go-templating (FileSystem), p&t, extra-resources | xpkg; templates in a ConfigMap |
| [livewyer-ops/crossplane-configuration-aws-elemental](https://github.com/livewyer-ops/crossplane-configuration-aws-elemental) | 12 | 12 | go-templating | xpkg |
| [openkubes/openkubes](https://github.com/openkubes/openkubes) | 10 | 10 | go-templating | 19 ArgoCD Applications |
| [ims-platform-dev/portal-kombat](https://github.com/ims-platform-dev/portal-kombat) | 8 | 8 | patch-and-transform | ArgoCD ×9 + 4 Helm charts |
| [tomernos/pavedplane](https://github.com/tomernos/pavedplane) | 7 | 7 | go-templating | — |
| [TeraSky-OSS/declarative-conversion-operator](https://github.com/TeraSky-OSS/declarative-conversion-operator) | 6 | 24 | go-templating | — |
| [aws-samples/appmod-blueprints](https://github.com/aws-samples/appmod-blueprints) | 3 | 4 | go-templating, environment-configs | Helm |
| [deliveryhero/asya](https://github.com/deliveryhero/asya) | 3 | 1 | go-templating ×7 steps, auto-ready, in-house `function-asya-flavors` | **Helm chart wrapping the compositions** |
| [PHACDataHub/infra-core](https://github.com/PHACDataHub/infra-core) | 4 | 4 | p&t | Flux (17 HelmRelease) + kustomize |
| [luebken/platform-example-logistics](https://github.com/luebken/platform-example-logistics) | 4 | 4 | go-templating | — |
| [Gustavobelfort/forja](https://github.com/Gustavobelfort/forja) | 4 | 3 | go-templating, environment-configs | — |
| [konflux-ci/crossplane-control-plane](https://github.com/konflux-ci/crossplane-control-plane) | 2 | 2 | go-templating (**FileSystem**), auto-ready | kustomize; digest-pinned mirrored function images |

Composition file size: **median 110 lines, mean 222, p90 472, max 4,100.**

### Function-step census (step instances across the 332 files)

```
89 function-go-templating            72 function-auto-ready
52 crossplane-contrib-function-auto-ready   42 crossplane-contrib-function-go-templating
40 function-patch-and-transform      36 function-kcl
34 function-appcat-master-v4-194-0   26 crossplane-contrib-function-patch-and-transform
23 crossplane-contrib-function-kcl   19 function-environment-configs
18 function-appcat-debug-v4-194-0     6 function-extra-resources
 4 function-sequencer                 3 function-asya-flavors
 + 14 one-off modelplane-modelplanecompose-* functions
```

> **Generator requirement:** the *same* function is referenced by two different names in the wild —
> `function-go-templating` and `crossplane-contrib-function-go-templating` (the package-derived name).
> stuttgart-things call this out explicitly in their conventions file: *"Function Names (must match between
> composition and functions.yaml)"*. The generator must emit `functions.yaml` and the `functionRef.name`
> from one source of truth, and support arbitrary registries + digest pinning
> (Konflux: `quay.io/konflux-ci/crossplane-components/function-go-templating@sha256:e2ea39…`).

---

## 2. Quantified pattern frequency (n=332)

| Pattern | Files | % | Graph-expressible? |
|---|---:|---:|---|
| `providerConfigRef` present | 219 | 65% | **Yes** — resource-node property |
| XR `spec.providerConfigRef` propagated down | 103 | 31% | **Yes** — "propagate" checkbox |
| `deletionPolicy` | 96 | 29% | **Yes** |
| `region` | 81 | 24% | **Yes** |
| tags block | 64 | 19% | **Yes** — merge semantics needed |
| `managementPolicies` | 37 | 11% | **Yes** |
| `writeConnectionSecretToRef` / connection secrets | 151 | 45% | **Yes** |
| `connectionDetails` list | 59 | 18% | **Yes** — repeated struct |
| `crossplane.io/external-name` | 77 | 23% | **Yes** — usually a `printf` of XR name |
| `*Selector:` (ref triad) | 60 | 18% | **Yes** — edge in the graph |
| `matchControllerRef: true` | 44 | 13% | **Yes** |
| `patchSets:` | 80 | 24% | **Yes** — "shared property set" |
| `ToCompositeFieldPath` (status back-patch) | 74 | 22% | **Yes** — reverse edge |
| `transforms:` | 73 | 22% | Partly — string/map/convert yes, math never used |
| `combine:` multi-field patch | 40 | 12% | **Yes** — printf node with N inputs |
| go-template `if` | 73 | 22% | **Yes** — conditional resource/field |
| go-template `else` | 44 | 13% | **Yes** |
| go-template `range` | 26 | 8% | **Yes** — the loop node |
| `with` | 6 | 2% | Yes (optional-guard sugar) |
| `setResourceNameAnnotation` | 46 | 14% | **Yes** |
| `composition-resource-name` annotation (any form) | 84 files / 237 occurrences | 25% | **Yes** |
| `.observed.composite` read | 83 | 25% | **Yes** |
| `.observed.resources[...]` lookup | 35 files / 44 lookups | 11% | **Yes** — "observe composed X" node |
| `dig` | 36 | 11% | **Yes** — path+default |
| `index` | 51 | 15% | **Yes** |
| `printf` | 58 | 17% | **Yes** — derived-string node |
| `| default` | 55 files / 369 uses | 17% | **Yes** |
| `dict`-based value mapping (version/plan tables) | 22 | 7% | **Yes** — lookup-table node |
| `b64enc`/`b64dec` | 31 | 9% | **Yes** — secret assembly |
| `randAlpha*` | 12 | 4% | Yes (password gen) |
| `toYaml` / `nindent` | 17 / 18 | 5% | **Raw** — arbitrary sub-doc splicing |
| `{{ define }}` named partials | few but load-bearing | — | **Raw / library** |
| `fail` / `required` validation guard | 42 (`required`), 1 (`fail`) | 13% | **Yes** — required-field flag |
| `managementPolicies: ["Observe"]` observe-only MR | 32 | 10% | **Yes** — node type |
| `DeriveFromCelQuery` readiness (provider-kubernetes) | 5 | 2% | **Yes** — readiness policy enum |
| `apiextensions.crossplane.io/v2` XRD in same repo | 19 | 6% | — |
| `.m.crossplane.io` / `.m.upbound.io` namespaced groups | 87 | 26% | — |
| `argocd.argoproj.io/*` annotations on compositions | 88 files / 150 occurrences | 27% | **Yes** — output metadata |
| environment/tier branch inside the composition | **2** | **0.6%** | — (see §4) |

---

## 3. The most complex compositions, explained

### 3a. Giant Swarm — `apis/xnetworks/transitgateway.yaml`, **4,100 lines**, 8 KCL steps
<https://github.com/giantswarm/crossplane-gs-apis/blob/main/apis/xnetworks/transitgateway.yaml>

Every one of the 8 steps re-embeds the same ~90-line KCL preamble. **Verbatim copy count in this repo:
31× `get = lambda x: {:}, y: str, d: any -> any`, 31× `ready = lambda x: str -> bool`, 7× `gpcr = lambda`,
across 27,206 YAML lines in `apis/`.** The preamble reimplements, by hand, exactly the primitives a DSL should own:

```python
get = lambda x: {:}, y: str, d: any -> any {
    """Get an item from a dictionary using a dot separated path.
       If the item is not found, return a default value."""
    p = regex.split(y, "\."); c = p[0]; y = ".".join(p[1:])
    x[c] if len(p) == 1 and c in x else d if c not in x else get(x[c], y, d)
}
gpcr = lambda x: {:} -> {:} {
    """Get the ProviderConfigRef from the given object.
       If this is not set it will attempt to return the ProviderConfigRef from
       the Observed Composite Resource, and if that isn't set, will return an
       object with an empty name."""
    get(x, "providerConfigRef", get(oxr, "spec.providerConfigRef", {name: ""}))
}
ocdsstatus = lambda x: str, y: str, d: any -> any {
    _x = None
    if get(ocds, "${x}.Resource.status.atProvider", False):
        _x = get(ocds, "${x}.Resource.status.atProvider.${y}", d)
    else:
        _x = get(ocds, "${x}.Resource.status.${y}", d)
    _x if _x else d
}
```
→ *dig-with-default*, *providerConfigRef fallback chain*, *observed-status read with `atProvider` vs bare fallback*.

The cross-cutting propagation, applied to **every** emitted resource:
```python
dp        = get(oxr, "spec.deletionPolicy", "Delete")
labels    = get(oxr, "metadata.labels", {})
mgmtPolicy= get(oxr, "spec.managementPolicies", [])
tags      = get(oxr, "spec.tags", {}) | { "region": region } | labels
```

And a **parameterised composed-resource factory called in a loop** — the loop node, with conditional emission,
conditional naming, tag merge and a per-resource derived ready flag:
```python
mpl = lambda n: str, r: str, a: str, b: bool, isp: bool, e: [], p: {:} -> {:} {
    _bh = "-bh" if b else ""
    _n = "${appName}-${region}-${n}${_bh}" if n != appName else "${appName}-${region}${_bh}"
    _resourceName = "mpl-${_n}" if not isp else "p-mpl-${_n}"
    { "apiVersion": "xnetworks.crossplane.giantswarm.io/v1alpha1", "kind": "ManagedPrefixList",
      "metadata": { "annotations": { "krm.kcl.dev/composition-resource-name": _resourceName,
                                     "krm.kcl.dev/ready": readystr(_resourceName) },
                    "labels": labels | { "vpcName" = n } },
      "spec": { "deletionPolicy": dp, "managementPolicies": mgmtPolicy,
                "providerConfigRef": p, "entries": e, "region": r,
                "tags": tags | { "Name": "${_n}${_isp}" } }
    } if len(e) > 0 else {}          # ← conditional emission by returning {}
}
```
**Graph verdict:** the *resource factory* and the propagation set are perfectly structural. The 300-line
`routingTables` comprehension that computes CIDR/blackhole route sets is **raw escape** — it is data-shaping,
not resource templating, and belongs in a `rawExpression` bound to one field.

Giant Swarm are already generating: `README.md` says the repo *"contains both code and crossplane compositions
and definitions generated via `crossbuilder`, an opensource crossplane compiler."*
Their Go builder API is direct prior art for compositionfactory's IR:
```go
c.WithName("rds-cache-cluster").WithMode(xapiextv1.CompositionModePipeline).
  WithLabels(map[string]string{"provider":"aws","component":"database","type":"base"})
c.NewPipelineStep("patch-and-transform").
  WithFunctionRef(xapiextv1.FunctionReference{Name: "function-patch-and-transform"}).
  WithInput(build.ObjectKindReference{Object: &xpt.Resources{ PatchSets: []xpt.PatchSet{
      metadataPatchSet(), commonPatchSet() }, Resources: resources }})
kclPatchTemplate, err = build.LoadTemplate("templates/patching.k")   // ← templates loaded from disk
```
<https://github.com/giantswarm/crossplane-gs-apis/blob/main/crossplane.giantswarm.io/xcomposite/compositions/rds-cache-cluster/main.go>

Their two reusable patchSets are *exactly* the cross-cutting set the generator should offer as a preset
(`crossplane.giantswarm.io/xcomposite/compositions/rds-cache-cluster/patchsets.go`):
```go
// "metadata"     metadata.labels→metadata.labels (MergeObjects); spec.region→metadata.labels.region;
//                spec.providerConfigRef→spec.providerConfigRef; spec.deletionPolicy→spec.deletionPolicy
// "commontags"   spec.tags→spec.forProvider.tags (MergeObjects);
//                metadata.labels→spec.forProvider.tags (MergeObjects); spec.region→spec.forProvider.tags.region
```

### 3b. Delivery Hero `asya` — Helm-templated go-templating, **758 lines**, 9 steps
<https://github.com/deliveryhero/asya/blob/main/deploy/helm-charts/asya-crossplane/templates/composition-sqs.yaml>

Composition YAML is a **Helm template that emits a Go template**. Every single `{{ }}` intended for the
composition function is escaped as `` {{`{{ … }}`}} ``:

```
{{`{{- $xr := .observed.composite.resource -}}`}}
{{`{{- $namespace := index $xr.metadata.labels "crossplane.io/claim-namespace" -}}`}}
{{`{{- $region := "`}}{{ .Values.awsRegion | default "us-east-1" }}{{`" -}}`}}
{{`{{- $providerConfigRef := "`}}{{ .Values.awsProviderConfig.name }}{{`" -}}`}}
```
**Escape count: 401 (sqs) + 391 (pubsub) + 331 (rabbitmq) = 1,123 backtick escapes across 2,087 lines.**
7 Helm `{{- if .Values… }}` blocks conditionally include/exclude whole pipeline steps.

Three transport variants of **one** XRD (`XAsyncActor`): **sqs vs pubsub = 80% identical lines,
sqs vs rabbitmq = 75%**.

Also demonstrates three patterns worth first-class support:
* **Cross-step context**: step 1 writes `kind: Context` `data: "asya/user-labels"`; later steps read
  `{{ if and .context (index .context "asya/user-labels") }}`.
* **Pipeline-aware XR read** (needed once a step mutates the XR):
  `$xrSpec := $xr.spec` then `if .desired.composite.resource.spec → $xrSpec = .desired…spec`.
* **~100-line readiness/status derivation step** — see §5.

### 3c. `ims-platform-dev/portal-kombat` — patch-and-transform without loops, **2,264 lines**
<https://github.com/ims-platform-dev/portal-kombat/blob/main/infra-definitions/compositions/network/security-group-standard.yaml>

Header comment, verbatim:
```
# Naming Pattern: {environment}-{purpose}-sg
# Rule Limits: Maximum 20 ingress + 20 egress rules per security group
# YAML Array Limitation: Uses pre-defined rule slots (0-19) with Optional policy to skip unused slots
```
45 composed resources; **289 `FromCompositeFieldPath` patches**. Slots `ingress-rule-0` … `ingress-rule-19`
are identical modulo a comment line and the index:
```yaml
- name: ingress-rule-7
  base: { apiVersion: ec2.aws.upbound.io/v1beta1, kind: SecurityGroupRule,
          spec: { forProvider: { type: ingress, securityGroupIdSelector: { matchControllerRef: true } } } }
  patches:
    - type: FromCompositeFieldPath
      fromFieldPath: spec.parameters.ingressRules[7].protocol
      toFieldPath: spec.forProvider.protocol
      policy: { fromFieldPath: Optional }
    # …6 more, all identical but for the field name
```
**Graph verdict:** one loop node over `spec.parameters.ingressRules` collapses 2,099 lines to ~25.

### 3d. `openkubes/openkubes` — PostgreSQL/CNPG, **879 lines, 254 variable assignments**
<https://github.com/openkubes/openkubes/blob/main/platform/database/postgresql/crossplane/composition.yaml>

Highest density of "things the DSL should own":
```gotemplate
{{- define "canonicalRFC3339" -}}   {{/* a named partial, inline */}}
{{- $claimName := index $xr.metadata.labels "crossplane.io/claim-name" | default $xr.metadata.name }}
{{- $dbName := $claimName }}
{{- if gt (len $dbName) 52 }}
{{- $dbName = printf "%s-%s" (trunc 43 $dbName | trimSuffix "-" | trimSuffix ".") (sha256sum $dbName | trunc 8) }}
{{- end }}
{{- $isProduction := eq $policy "production" }}
{{- $instances := 1 }}{{- $storageSize := "5Gi" }}{{- $retention := "7d" }}
{{- if eq $spec.availability.mode "ha" }}{{- $instances = 3 }}{{- end }}
{{- if $isProduction }}
{{- $storageSize = "20Gi" }}{{- $retention = "30d" }}{{- $backupValidity = "+24h" }}
{{- end }}
```
* **name-length safeguard with deterministic hash suffix** (k8s/cloud name limits) — a generator should emit this.
* **profile/tier override block** — the *only* common in-composition "environment" mechanism found (a policy enum
  driving a set of defaults), and it is a table, not control flow.
* **hardcoded lookup registry with a guard**, with the security rationale written into the template:
```gotemplate
{{- $backupStores := dict "ok-robotics" (dict "bucket" "ok-db-backups"
      "endpointURL" "https://minio.minio.svc:9000" "endpointCA" "minio-backup-store-ca") }}
{{- if not (hasKey $store "endpointURL") }}
{{- fail (printf "no reviewed backup store is registered for cluster %q: refusing to compose a Database
        with no protection destination. Add it to $backupStores in this Composition." $provider) }}
{{- end }}
```
* **observed-composed-resource lookup boilerplate, repeated 5×** (cluster, app-secret, evidence-backup,
  scheduled-backup, object-store) — the strongest argument for an "observe composed resource X" node:
```gotemplate
{{- $clusterObject := dict }}
{{- if hasKey $.observed.resources "database-cluster" }}
{{- $clusterObject = (index $.observed.resources "database-cluster").resource }}
{{- end }}
{{- $clusterManifest := dig "status" "atProvider" "manifest" (dict) $clusterObject }}
{{- $clusterStatus   := dig "status" (dict) $clusterManifest }}
```

### 3e. `awslabs/crossplane-on-eks` — `rest-lambda-ddb.yaml`, 907 lines, 13 composed resources
<https://github.com/awslabs/crossplane-on-eks/blob/main/compositions/upbound-aws-provider/serverless-microservice/rest-lambda-ddb.yaml>

Canonical **three-class patchSet** design — one set per composed-resource class:
```yaml
patchSets:
  - name: common-fields              # for nested XRs: pass the whole envelope
    patches: [{type: FromCompositeFieldPath, fromFieldPath: spec.resourceConfig, toFieldPath: spec.resourceConfig}]
  - name: common-fields-upbound      # for regional MRs
    patches:
      - {fromFieldPath: spec.resourceConfig.deletionPolicy,     toFieldPath: spec.deletionPolicy}
      - {fromFieldPath: spec.resourceConfig.providerConfigName, toFieldPath: spec.providerConfigRef.name}
      - {fromFieldPath: spec.resourceConfig.region,             toFieldPath: spec.forProvider.region}
  - name: common-fields-upbound-global   # for global MRs (no region)
```
and **nested-XR composition selection by label**, which is how AWS Labs express environment/variant choice:
```yaml
- name: restapi
  base:
    apiVersion: awsblueprints.io/v1alpha1
    kind: XApiGateway
    spec:
      compositionSelector:
        matchLabels:
          awsblueprints.io/provider: aws
          awsblueprints.io/environment: dev
          awsblueprints.io/type: rest
```

---

## 4. Per-environment / per-region / per-tenant: how it is *actually* done

Four mechanisms observed; **none of them is an `if env == "prod"` inside a composition** (2/332 files).

**(A) Re-render the composition per environment — VSHN AppCat.**
`tests/golden/{dev,dev-talos,control-plane,vshn-cloud,vshn-managed,exodev}/appcat/appcat/21_composition_*.yaml`
are the same compositions rendered 6× from Jsonnet. Diffing dev vs vshn-managed for PostgreSQL, the
*entire* delta is config values and the function's version-pinned name:
```diff
-    metadata.appcat.vshn.io/zone: lpg              +    metadata.appcat.vshn.io/zone: rma1
-    metadata.appcat.vshn.io/revision: debug-v4.194.0 +  metadata.appcat.vshn.io/revision: master-v4.194.0
-        name: function-appcat-debug-v4-194-0       +        name: function-appcat-master-v4-194-0
-          isOpenshift: 'false'                     +          isOpenshift: 'true'
-          bucketRegion: rma                        +          bucketRegion: lpg
-          salesOrder: ''                           +          salesOrder: ST10120
```
<https://github.com/vshn/component-appcat/blob/develop/tests/golden/vshn-managed/appcat/appcat/21_composition_vshn_postgres.yaml>
Note the **revision-pinned functionRef name** (`function-appcat-master-v4-194-0`) — multiple composition
revisions coexisting, each bound to its own function build. A generator emitting a version suffix into both
`functionRef.name` and the `Function` package is directly useful.

**(B) One composition per variant, selected by labels — AWS Labs.**
`awsblueprints.io/environment: dev` on **49** compositions, `awsblueprints.io/provider: aws` on 47, plus
per-variant discriminators: `iam.awsblueprints.io/policy-type: read|write` (13), `iam.awsblueprints.io/service`
(11 values), `dynamodb.awsblueprints.io/capacity: provisioned|on-demand`, `…/pkType: composite|partition`,
`…/secondaryIndexCount: "1"`. One `IAMPolicy` XRD has **10 compositions** (mean pairwise identity **72%**,
max 93%); one DynamoDB XRD has **5** (50–86% pairwise). The only real difference between the 10 IAM ones
is a JSON policy blob and a name prefix:
```yaml
- type: CombineFromComposite
  toFieldPath: spec.forProvider.policy
  combine:
    variables: [{fromFieldPath: spec.resourceArn}, {fromFieldPath: spec.resourceArn}]
    strategy: string
    string:
      fmt: |
        { "Version": "2012-10-17", "Statement": [
          { "Effect": "Allow", "Action": ["s3:GetObject"], "Resource": ["%s/*"] },
          { "Effect": "Allow", "Action": ["s3:ListBucket"], "Resource": ["%s"] } ] }
```
<https://github.com/awslabs/crossplane-on-eks/blob/main/compositions/upbound-aws-provider/iam-policy/s3-read.yaml>

**(C) One composition per cloud — livewyer `gardener-allotment`.** `platform/compositions/aws/{6 files}` and
`platform/compositions/gcp/{6 files}` for the same 6 XRDs. Line-identity aws↔gcp:
`workload 97%, seed 88%, allotment 86%, virtual 81%, garden 77%` — only `infra` genuinely differs (4%).

**(D) `EnvironmentConfig` for values that vary at runtime** — 31/332 files. livewyer layer three of them
per composition (shared / per-cloud / per-install):
```yaml
- step: load-versions
  functionRef: { name: function-environment-configs }
  input:
    spec:
      environmentConfigs:
        - {type: Reference, ref: {name: allotment-versions}}
        - {type: Reference, ref: {name: allotment-versions-aws}}
        - {type: Reference, ref: {name: allotment-config}}
```
<https://github.com/livewyer-ops/gardener-allotment/blob/main/platform/compositions/aws/infra.yaml>
with `configs/versions-shared.yaml` commented *"Update versions here — compositions read them via
function-environment-configs."* stuttgart-things make it a rule: **"Use EnvironmentConfig for all defaults |
Don't use plain ConfigMaps for composition defaults."**

**Per-tenant** appears only as claim-namespace derivation, never as a composition axis:
`{{ index $xr.metadata.labels "crossplane.io/claim-namespace" }}` (Delivery Hero, openkubes) and
`s3bucket-multi-tenant.yaml` (AWS Labs).

---

## 5. What they hand-write that a generator must reproduce

### Naming conventions
stuttgart-things codify the derivation rules in `CLAUDE.md` — a ready-made spec for the generator:
> **Kind**: PascalCase of name (`gitlab-runner` → `GitlabRunner`) · **xrdSingular**: lowercase without hyphens
> (`gitlabrunner`) · **xrdPlural**: singular + `s` · **claimName**: Kind + `Claim`
<https://github.com/stuttgart-things/crossplane/blob/main/CLAUDE.md>

Observed name-derivation idioms:
* `{{ setResourceNameAnnotation $releaseName }}` where `$releaseName := printf "ghr-%s-%s" $repository ($provider | replace "/" "-")` (sanitising `/`)
* `crossplane.io/external-name: {{ printf "%s-%s" $xrName $subnet.name | quote }}` inside a loop
* `_n = "${appName}-${region}-${n}${_bh}" if n != appName else "${appName}-${region}${_bh}"` (Giant Swarm)
* `namespace | replace "." "-"` (Konflux)
* length-clamped hash suffix (openkubes, §3d)
* `# Naming Pattern: {environment}-{purpose}-sg` (portal-kombat)
* provider-config names derived from a field: `_helmPcr = "{}-helm".format(_clusterName)`, `_k8sPcr = "{}-kubernetes".format(_clusterName)` (stuttgart-things)

### Labels & annotations actually written (occurrence counts across the 332 files)
```
237 gotemplating.fn.crossplane.io/composition-resource-name   86 argocd.argoproj.io/sync-wave
132 app.kubernetes.io/name                                    64 argocd.argoproj.io/sync-options
 85 awsblueprints.io/environment      80 awsblueprints.io/provider
 76 catalog.cluster.local/kind        76 catalog.cluster.local/name
 70 crossplane.io/xrd                 61 app.kubernetes.io/instance
 62 metadata.appcat.vshn.io/{description,displayname,end-user-docs-url,product-description,zone,offered,serviceID}
 49 crossplane.io/external-name       26 app.kubernetes.io/managed-by
```
Three families the generator should emit as presets: (1) the `app.kubernetes.io/*` recommended set;
(2) `crossplane.io/xrd: <xrd-name>` back-link on the Composition; (3) **ArgoCD sync metadata** —
`argocd.argoproj.io/sync-options: SkipDryRunOnMissingResource=true` plus a `sync-wave` (VSHN emit
`sync-wave: '-60'` on compositions and `-100` on providers, applied by a `postprocess/add_argo_wave_crossplane.jsonnet`
fix-up pass over the whole output dir — i.e. *a generator post-processing hook*).
VSHN additionally stamp a whole self-service-catalogue metadata block on every composition
(`displayname`, `description`, `plans` as inline JSON, `zone`, `serviceID`, `offered: 'true'`, `revision`).

### Tag propagation
Two idioms only. Patch form (AWS Labs / Giant Swarm):
```yaml
- type: FromCompositeFieldPath
  fromFieldPath: spec.resourceConfig.tags
  toFieldPath: spec.forProvider.tags
  policy: { mergeOptions: { keepMapValues: true } }     # older form
  # or policy: { toFieldPath: MergeObjects }            # newer form (portal-kombat, giantswarm)
```
Template form (Giant Swarm KCL): `tags = get(oxr,"spec.tags",{}) | {"region": region} | labels`
then per-resource `tags | {"Name": _n}`. **XR tags ∪ XR labels ∪ computed ∪ per-resource `Name`.**

### ProviderConfig selection — four distinct shapes, all needed
1. Literal: `providerConfigRef: {name: provider-helm, kind: ClusterProviderConfig}` (platformplane)
2. From an XR field: `spec.resourceConfig.providerConfigName → spec.providerConfigRef.name` (AWS Labs)
3. **Kind switched by an XR enum** (stuttgart-things — matches your `targetCluster` need exactly):
```gotemplate
{{- $scope := $spec.targetCluster.scope | default "Namespaced" -}}
{{- $pcKind := "ProviderConfig" -}}
{{- if eq $scope "Cluster" -}}{{- $pcKind = "ClusterProviderConfig" -}}{{- end -}}
...
  providerConfigRef:
    name: {{ $provider }}
    kind: {{ $pcKind }}
```
<https://github.com/stuttgart-things/crossplane/blob/main/configurations/apps/github-runner/compositions/github-runner.yaml>
4. **Fallback chain**: resource-level → XR-level → `{name: ""}` (Giant Swarm `gpcr`).

### Deletion / management policies
`deletionPolicy` in 96 files, always sourced from the XR (`get(oxr,"spec.deletionPolicy","Delete")`, or a
`FromCompositeFieldPath` patch) — never hardcoded per-resource except as a `Delete` default in the `base`.
`managementPolicies` in 37 files; **`["Observe"]` observe-only resources in 32** — a distinct node type
(read a foreign object's status without owning it), e.g. stuttgart-things wrapping a cluster-scoped CR:
```yaml
spec:
  managementPolicies: ["Observe"]
  forProvider: { manifest: { apiVersion: kubeconfig.stuttgart-things.com/v1alpha1, kind: RemoteCluster,
                             metadata: { name: <clusterName> } } }
  providerConfigRef: { name: in-cluster, kind: ClusterProviderConfig }
```

### Readiness derivation — three flavours, all mechanical
1. **`function-auto-ready` as the terminal step** — 126/332 files. stuttgart-things make it a rule:
   *"Always end with `function-auto-ready` step."*
2. **Per-resource ready annotation computed by hand** — Giant Swarm compute `readystr(name)` for every emitted
   resource from `conditions[] type==Ready && status==True` OR non-empty `status.atProvider`.
3. **A dedicated status/phase step.** Delivery Hero's is ~100 lines and is *pure boilerplate a generator should own*:
```gotemplate
{{`{{- if index .observed.resources "sqs-queue" -}}`}}
  {{`{{- $q := (index .observed.resources "sqs-queue").resource -}}`}}
  {{`{{- if and (hasKey $q "status") (hasKey $q.status "atProvider") -}}`}}
    {{`{{- $queueUrl = $q.status.atProvider.id | default "" -}}`}}
  {{`{{- end -}}`}}
  {{`{{- range $q.status.conditions -}}`}}
    {{`{{- if and (eq .type "Ready") (eq .status "True") -}}{{- $queueReady = true -}}{{- end -}}`}}
  {{`{{- end -}}`}}
{{`{{- end -}}`}}
...
{{`{{- $phase := "Creating" -}}`}}
{{`{{- if and $queueReady $kedaReady $workloadReady -}}`}}{{`{{- $phase = "Ready" -}}`}}
  {{`{{- if eq (int $workloadReplicas) 0 -}}{{- $phase = "Napping" -}}{{- end -}}`}}{{`{{- end -}}`}}
apiVersion: asya.sh/v1alpha1
kind: XAsyncActor
status:
  phase: {{`{{ $phase }}`}}
  infrastructure: { queue: {ready: …}, keda: {ready: …}, workload: {ready: …, replicas: …, readyReplicas: …} }
```
Note the **provider-kubernetes double hop**: MR fields live at `.status.atProvider.<f>`, but wrapped
manifests live at `.status.atProvider.manifest.status.<f>`. The generator must know which.
4. **CEL readiness for `Object`s**, factored into a named partial (livewyer `_helpers.tmpl`):
```gotemplate
{{- define "allotment.nestedObjectReadiness" }}
readiness:
  policy: DeriveFromCelQuery
  celQuery: has(object.status.conditions) && object.status.conditions.exists(c, c.type == "Ready" && c.status == "True")
{{- end }}
{{- define "allotment.existsReadiness" }}
readiness: { policy: DeriveFromCelQuery, celQuery: has(object.metadata.uid) }
{{- end }}
```
<https://github.com/livewyer-ops/gardener-allotment/blob/main/platform/templates/_helpers.tmpl>

### Cross-resource wiring (the "reference triad")
`*Selector:` in 60 files, `matchControllerRef: true` in 44. Two shapes:
* **Single instance** → `policyArnSelector: {matchControllerRef: true}` / `securityGroupIdSelector: {matchControllerRef: true}`
* **N instances from a loop** → stamp labels on the producer, select them on the consumer (pavedplane):
```gotemplate
{{- range $vpc := $xr.spec.vpcs }}
kind: Network
metadata:
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: {{ printf "vpc-%s" $vpc.name | quote }}
    crossplane.io/external-name: {{ printf "%s-%s" $xrName $vpc.name | quote }}
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
**This is the graph edge.** `matchControllerRef` is not sufficient once a loop produces >1 of a kind — the
generator must emit the label-stamp/label-select pair automatically when an edge crosses a loop boundary.
Known pain here: `# TODO there is an issue with destinationArnSelector not working for Kinesis Firehose
https://github.com/upbound/upjet/issues/95` (crossplane-on-eks, ×2).

### Value-mapping tables
```gotemplate
{{ $versionMapping := dict "18" "0.12.1" "17" "0.5.5" }}
{{ $specVersion := $params.version | default "18" }}
{{ $version := index $versionMapping $specVersion }}
```
<https://github.com/platformplane/catalog-crossplane/blob/main/package/postgresql/v2/composition.yaml> —
22/332 files. platformplane document it as a house rule: *"there is usually a version mapping table defined
at the beginning of the inline template mapping the major product versions to the corresponding Helm chart version."*

### Connection-secret assembly
```gotemplate
{{ if eq $.observed.resources nil }}
data: {}
{{ else }}
{{- $helmSecret := (index $.observed.resources "helm-release").connectionDetails | default dict }}
{{- if and $helmSecret.password $helmSecret.port }}
data:
  host:     {{ printf "%s.%s" $xrName $namespace | b64enc }}
  username: {{ "postgres" | b64enc }}
  password: {{ $helmSecret.password }}
  database: {{ dig "resources" "helm-release" "resource" "spec" "forProvider" "values" "auth" "database" "postgres" .observed | b64enc }}
{{- else }}
data: {}
{{- end }}
{{ end }}
```
Note the **first-reconcile nil guard** — `.observed.resources` is nil on pass 1. 31 files use `b64enc`/`b64dec`;
15 emit `CompositeConnectionDetails`. A generator emitting a connection secret must emit this guard or the
first render errors.

---

## 6. File organisation for GitOps

| Mechanism | Repos | Evidence |
|---|---|---|
| Crossplane **Configuration package** (`crossplane.yaml` per module) | stuttgart-things (**31**), crossplane-on-eks (3), giantswarm, livewyer ×2, portal-kombat | one dir per API: `apis/definition.yaml` + `compositions/<name>.yaml` + `examples/` + `crossplane.yaml` |
| **Kustomize** overlays | crossplane-on-eks (33 kustomization.yaml), PHAC (9), konflux (5), giantswarm (5) | flat `compositions/<provider>/<service>/` |
| **Helm chart wrapping compositions** | Delivery Hero, 0xayf/homelab-idp (15 charts), portal-kombat (4) | see §3b for the escaping cost |
| **ArgoCD Applications** | shlapolosa (36), openkubes (19), portal-kombat (9) | plus `sync-wave`/`sync-options` annotations on 88 composition files |
| **Flux** | PHAC (17 HelmRelease), stuttgart-things | |
| Templates as a **mounted ConfigMap** + `source: FileSystem` | konflux-ci, livewyer (14 files) | `DeploymentRuntimeConfig` volumeMounts `/templates/<xrd>` |

Directories named `dev`/`staging`/`prod` exist in exactly **1 of 21** repos. Environment variance lives in
labels, EnvironmentConfigs, or a re-render — not in the directory tree.

The `source: FileSystem` layout is worth calling out as a generator output mode
(<https://github.com/konflux-ci/crossplane-control-plane/blob/main/config/xnamespace/composition.yaml>):
```yaml
- step: create-namespace
  functionRef: { name: function-go-templating }
  input:
    apiVersion: gotemplating.fn.crossplane.io/v1beta1
    kind: GoTemplate
    source: FileSystem
    fileSystem: { dirPath: /templates/xnamespace/ns.yaml }
```
backed by
```yaml
kind: DeploymentRuntimeConfig
spec: { deploymentTemplate: { spec: { template: { spec: {
  containers: [{ name: package-runtime, volumeMounts: [{mountPath: /templates/xnamespace, name: xnamespace-templates}]}],
  volumes: [{ name: xnamespace-templates, configMap: { name: xnamespace-templates }}] }}}}}
```
This gives a **6-step composition of 76 lines** with all logic in reviewable per-resource files — the cleanest
"generated artifact" shape in the whole corpus, and the one a node-graph GUI maps onto most naturally
(one file per node).

---

## 7. Evidence of pain

**Duplication metrics (the strongest signal, ranked):**

| Duplication | Measure | Source |
|---|---|---|
| 40 hand-expanded array slots | **2,099 / 2,264 lines (92%)**, 20 ingress slots identical modulo index | portal-kombat security-group |
| Copy-pasted KCL helper preamble | **31 verbatim copies** of `get`, 31 of `ready`, 7 of `gpcr`, across 13 compositions | giantswarm |
| Transport variants of one XRD | **80% / 75%** line identity (sqs↔pubsub / sqs↔rabbitmq) | Delivery Hero |
| Catalog items | **89%** max pairwise (mariadb↔redis↔postgresql), **46%** mean across 14 items | platformplane |
| IAM policy variants of one XRD | **72% mean, 93% max** across 10 compositions | AWS Labs |
| DynamoDB variants of one XRD | 50–86% pairwise across 5 compositions | AWS Labs |
| Cloud variants of one XRD | workload **97%**, seed 88%, allotment 86%, virtual 81% (aws↔gcp) | livewyer |
| v1↔v2 API duplication of same item | 43–68% (both shipped side by side, 13 items) | platformplane |
| Env re-renders of same composition | 6 golden dirs, delta = config values only | VSHN |
| Duplicated file header | copyright block literally pasted twice | `iam-policy/s3-read.yaml` |

**Defaults are written twice** — once in the XRD (`default: in-cluster`) and again in the template
(`| default "in-cluster"`) — throughout stuttgart-things and platformplane. One source of truth is a
concrete generator win. stuttgart-things try to fight it with a rule: *"Prefer defaults in XRD over template logic."*

**Explicit tool complaints:**
* stuttgart-things `cluster-profile/CLAUDE.md` Do/Don't table: *"KCL for nested XR emission (spread spec, `if`
  guards)"* vs *"patch-and-transform for nesting (**verbose, no branching**)"*; *"Explicit `if` guards for option
  branching"* vs *"Emit MRs conditionally via patch transforms"*.
* Their scaffolding workflow documents that the existing Dagger generator is not good enough:
  *"The Dagger blueprint produces files with known issues. Apply these fixes:"* — then **7 numbered manual
  repairs**, including *"Fix composition: Replace the scaffolded stub (wrong apiVersion `v1beta1`,
  commented-out pipeline referencing `function-patch-and-transform`) with a real Pipeline composition"* and
  *"Fix functions.yaml: Replace wrong apiVersions and function names"*. This is precisely the gap
  compositionfactory should close.
* shlapolosa/health-service-idp header comments record a v1→v2 scoping failure in production terms:
  *"the ClusterProviderConfig kind under `.m.crossplane.io` does not exist on this cluster … The XRD is
  LegacyCluster (cluster-scoped), so a cluster-scoped XR cannot supply a namespace to a namespaced `.m.`
  Object (that caused the 'empty namespace may not be set' error, **0/36 resources deployed**)."*
  Same file records the loop lift: *"the topic-provisioning Job now ranges over `spec.topics[]` and renders an
  EXACT per-topic create_topic call … instead of applying one cluster-wide TOPIC_PARTITIONS/TOPIC_RETENTION to
  every topic."* and the migration: *"rewritten from legacy `spec.resources` patch-and-transform to the
  platform-idiomatic Crossplane v2 function-go-templating pipeline."*
* `# TODO there is an issue with destinationArnSelector not working for Kinesis Firehose
  https://github.com/upbound/upjet/issues/95` — reference triad reliability (crossplane-on-eks, 2 files).
* platformplane README warns that a generator changing chart-version defaults *"will replace the affected Helm
  releases with the new version and therefore cause downtime"* — i.e. **generated output stability matters**;
  a regenerate must be diffable and must not silently move pinned versions.

**Legacy debt:** **79/332 (24%)** compositions are still native non-pipeline `resources:` mode
(almost all of crossplane-on-eks + portal-kombat). A generator that can emit both P&T-in-pipeline and
go-templating, and can *convert* between them, has an immediate audience.

---

## 8. Recommended DSL coverage, ranked by observed frequency

**First-class (appears in ≥20% of the corpus, or is the direct cause of the worst duplication):**
1. `fieldMapping{from: spec.x, to: spec.forProvider.y, default: …, required: bool}` — 1,189 assignments / 369 defaults.
2. `derived{fmt: "…%s-%s", inputs: [...], sanitise: replace/lower/trunc+hash}` — 152 printf uses; must cover the ≤63/≤52-char clamp with hash suffix.
3. **`forEach` over an XRD array** producing N resources, with per-item resource-name, per-item external-name, and automatic label-stamp/label-select edge rewriting. (26 files do it; 1 file *fails* to do it at a cost of 2,099 lines.)
4. `conditional` — emit-resource-if and emit-field-if (73 `if` + 44 `else` files); include "skip when the array is empty" and "skip when an optional object is absent" (`with`).
5. **Cross-cutting propagation preset** — providerConfigRef (incl. `kind:` switching Namespaced/Cluster, and the resource→XR→default fallback chain), deletionPolicy, managementPolicies, region, tags-merge, labels-merge. Emit as a patchSet in P&T mode and as a template preamble in go-templating mode.
6. **Observe-composed-resource node** → binds `.observed.resources["<name>"].resource`, knows the MR (`status.atProvider.*`) vs `Object` (`status.atProvider.manifest.status.*`) hop, and emits the `hasKey`/nil guards.
7. **Readiness derivation node** — (a) terminal `function-auto-ready`, (b) per-resource ready annotation from conditions, (c) an XR `status` step aggregating booleans into a phase enum, (d) `DeriveFromCelQuery` for `Object`s.
8. `lookupTable{key: spec.version, map: {...}, default: …}` — 22 files (version→chart-version, plan→size).
9. **Reference triad** as a graph edge with two rendering strategies: `matchControllerRef` for singletons, label stamp/select across loops.
10. Connection-secret assembly with the `.observed.resources == nil` first-pass guard + `b64enc`.
11. Output metadata presets: `composition-resource-name`, `crossplane.io/xrd`, `app.kubernetes.io/*`, ArgoCD `sync-wave`/`sync-options`, and a free-form catalogue-metadata block (VSHN-style).

**Blueprint-level (not inside one composition):**
12. **Variant expansion**: one blueprint + a values matrix → N Compositions differing by labels
    (`environment`, `provider`, `policy-type`, `capacity`, …) plus the matching `compositionSelector` snippet.
    This is mechanism (A)+(B)+(C) in §4 and would collapse the 72–97% duplication measured in five repos.
13. **Output-mode switch**: inline template · `source: FileSystem` + ConfigMap + `DeploymentRuntimeConfig` ·
    thin shim delegating to a custom function with a ConfigMap of settings.
14. **Post-processing hook** over the emitted directory (VSHN's `add_argo_wave_crossplane.jsonnet` precedent).
15. XRD/template **default de-duplication** — write the default once, emit it into both the XRD schema and the
    template `| default`.

**Raw escape hatch (rare, or genuinely program-shaped):**
16. Multi-hundred-line data-shaping comprehensions (Giant Swarm routing tables) — bind to one field.
17. `toYaml`/`nindent` splicing of arbitrary sub-documents (17 + 18 files) — a raw block.
18. Named partials / `{{ define }}` libraries shared across compositions (livewyer `_helpers.tmpl`,
    openkubes inline `canonicalRFC3339`) — support a "template library" the generator inlines or mounts.
19. `fail`-based policy guards with human-readable messages.
20. Cross-step `Context` and `ExtraResources` requirements blocks (13 and 1 files) — low frequency, but
    unrepresentable otherwise.

