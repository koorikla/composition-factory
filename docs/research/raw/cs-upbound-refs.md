# Case study: Upbound reference platforms & official Configurations

**Method.** Enumerated `github.com/upbound` via the public API (210 repos, 3 pages), filtered to
`configuration-*` + `platform-ref-*` → **73 repos** (2 archived). Shallow-cloned all **71 live repos**
(2026-08-27). Parsed every YAML with PyYAML and grepped the embedded function sources.

**Corpus size:** 118 files containing 120 `Composition` docs; 116 files containing 117
`CompositeResourceDefinition` docs; 34 KCL embedded functions; 80 Python function files
(7 repos); 1 TypeScript function; 1 embedded go-template.

---

## What this means for the DSL — 5 bullets

1. **Upbound has abandoned YAML-authored composition logic entirely.** Their flagship
   `composition.yaml` files are now **16–19 lines**: `compositeTypeRef` + two pipeline steps
   (one embedded function + `function-auto-ready`). 46 of 84 pipeline compositions have exactly
   that shape. *All* the logic lives in `functions/<step>/main.k|.py|.ts|.gotmpl`. A generator that
   emits a big inline `template:` block is emitting a shape Upbound themselves moved away from —
   **compositionfactory should be able to emit either an inline `GoTemplate` step or an embedded
   `functions/<name>/compose.yaml.gotmpl` + a 16-line composition.yaml** (the latter is what
   `up project build` / `crossplane project` consume). Real example of the embedded form:
   `configuration-aws-securitygroup/functions/xsecuritygroups/compose.yaml.gotmpl`.

2. **Cross-resource references are ~99% `*Selector`, not templated status paths.**
   160 `*Selector` occurrences (23/34 KCL fns) vs a handful of computed `.status.atProvider.*`
   interpolations. `matchControllerRef: true` (99 hits) for siblings; `matchLabels` (53 hits) for
   "the sibling with role=X" and for cross-*Configuration* wiring. The DSL's "reference triad"
   must model `field` / `fieldRef.name` / `fieldSelector{matchControllerRef,matchLabels}` as a
   single first-class node, and **the graph edge between two nodes should default to
   `matchControllerRef: true`, optionally narrowed by labels the source node stamps.**

3. **Conditional resources are the single most common non-trivial pattern, and the condition is
   almost always "a *previously observed* sibling is Ready, OR I already exist".**
   `if ready(_ocds["kubernetesCluster"]) or exists("kubernetesClusterAuth")` appears throughout.
   The `or exists(...)` half is load-bearing — without it the resource gets *deleted* when the
   dependency flaps. The DSL needs a `dependsOnReady: <nodeName>` edge that generates the full
   `{{- if or (dig "observed" "resources" "X" "resource" "status" ...) (hasKey .observed.resources "Y") }}`
   guard, not just a naive `if`. 8/34 fns read `status.conditions`; 21/34 read `_ocds` at all.

4. **Loops are real, nested, and the resource *name* is a computed function of the loop item.**
   `range $i, $rule := .rules` × `range $j, $cidr := $rule.cidrBlocks` producing N×M MRs, named
   `printf "sgrule-%d-%s" $i ($cidr | replace "." "-" | replace "/" "-")`. One case even computes
   the **kind** from data (`kind: SecurityGroup{{ title $rule.type }}Rule`). 132 arrays across
   117 XRDs. A graph GUI can express "loop node over `spec.parameters.X[]`" structurally, but the
   **name expression** and the **dynamic kind** need an expression field / rawTemplate escape.

5. **EnvironmentConfig usage across all 71 Upbound repos: ZERO.** Not one `EnvironmentConfig`,
   `FromEnvironmentFieldPath`, or `spec.environment`. Do not spend DSL surface on it. Spend it
   instead on the things that *are* everywhere: `providerConfigRef{kind,name}` (70 hits),
   `managementPolicies` passthrough (80 hits), `deletionPolicy` (81 hits), the
   composition-resource-name annotation (41 hits / 30 files), a readiness-override annotation
   (21 hits / 14 files), and `CompositeConnectionDetails` (7 files).

---

## 1. Which composition functions — counts across the corpus

### Composition authoring style (120 Composition docs)

| Style | Count | % |
|---|---|---|
| `mode: Pipeline` | 84 | 70% |
| Legacy `spec.resources` (no pipeline, pre-function) | 36 | 30% |
| `apiVersion: apiextensions.crossplane.io/v1` | 120 | 100% (Composition is still v1 in XP v2) |

### Pipeline step function usage (156 total steps)

| Function | Steps | Compositions |
|---|---|---|
| `crossplane-contrib-function-auto-ready` | 55 | 55 |
| `crossplane-contrib-function-patch-and-transform` | 23 | 23 |
| **embedded project functions** (`upbound-<repo><step>`) | **51** | **51** |
| `crossplane-contrib-function-kcl` (inline KCL in YAML) | 8 | 8 |
| `crossplane-contrib-function-sequencer` | 5 | 5 |
| `upboundcare-function-conditional-patch-and-transform` | 5 | 5 |
| `crossplane-contrib-function-extra-resources` | 4 | 4 |
| **`crossplane-contrib-function-go-templating`** | **4** | **4** |
| `crossplane-contrib-function-cel-filter` | 1 | 1 |

**Embedded function language split** (the 51 project-function steps):
KCL `main.k` — 34 files / **26 repos**; Python `main.py` — 7 repos; TypeScript — 1 repo
(`configuration-aws-network-ts`, pushed today); go-template `.gotmpl` — **1 repo**
(`configuration-aws-securitygroup`).

**Go-templating is a rounding error in Upbound's own corpus: 4 inline + 1 embedded = 5 of 118
compositions (4%).** KCL is their house language. This means the *authoring idioms* you must
cover come from KCL/Python semantics, translated to go-templating — which is what the rest of
this brief does.

### v2 migration status

| | Count |
|---|---|
| XRDs on `apiextensions.crossplane.io/v2` | **41 / 117 (35%)** |
| ...of which `scope: Namespaced` | 39 |
| ...of which `scope: Cluster` | 2 |
| XRDs still on `v1` | 76 |
| XRDs with `claimNames` (v1 only) | 52 |

**The flagships have all migrated.** `platform-ref-aws`, `platform-ref-gcp`, `platform-ref-azure`,
and every `configuration-{aws,gcp,azure}-{network,eks,gke,aks,database,...}` are v2 Namespaced with
no claims. The v1 stragglers are the old repos (`platform-ref-cloud-native`, `configuration-eks`,
`platform-ref-lambda`, `platform-ref-s3-website`, `configuration-vault`) last touched in 2024.

**Namespaced (`.m.`) provider groups confirmed.** KCL model imports across the corpus:
`awsm` 37, `azurem` 22, `gcpm` 18, plus `kubernetesm`/`helmm` — vs cluster-scoped `aws` 16,
`gcp` 3, `azure` 2. So **77 namespaced vs 21 cluster-scoped**. The v2 repos import `.m.`.

**New artifact you must emit: `ManagedResourceActivationPolicy`.** In Crossplane v2, Upjet
providers ship every MR CRD deactivated. 2 repos ship an MRAP next to the composition:

```yaml
# configuration-aws-network/apis/networks/mrap.yaml
apiVersion: apiextensions.crossplane.io/v1alpha1
kind: ManagedResourceActivationPolicy
metadata:
  name: configuration-aws-network
spec:
  activate:
  - internetgateways.ec2.aws.m.upbound.io
  - mainroutetableassociations.ec2.aws.m.upbound.io
  - routes.ec2.aws.m.upbound.io
  - routetableassociations.ec2.aws.m.upbound.io
  - routetables.ec2.aws.m.upbound.io
  - securitygrouprules.ec2.aws.m.upbound.io
  - securitygroups.ec2.aws.m.upbound.io
  - subnets.ec2.aws.m.upbound.io
  - vpcs.ec2.aws.m.upbound.io
```
<https://github.com/upbound/configuration-aws-network/blob/main/apis/networks/mrap.yaml>

> **DSL note:** compositionfactory already knows every MR type in a blueprint. Emitting the MRAP is
> a free, mechanical, high-value output. Only 2/71 Upbound repos do it today — being ahead here is
> cheap. **Graph GUI: fully structural (derived, no user input).**

---

## 2. The biggest compositions

### Biggest by raw YAML: `platform-ref-aws-cnoe/apis/composition.yaml`
**1461 lines · 33 composed resources · 0 pipeline steps (legacy `spec.resources`)**
<https://github.com/upbound/platform-ref-aws-cnoe/blob/main/apis/composition.yaml>

Resource list (all 33, in file order): `XNetwork, XEKS, XOss, XArgo, XKarpenter,
usageXEksByXKarpernter, usageXEksByXArgo, usageXEksByXOss, XIRSAExternalDNS, externalDNSChart,
XIRSAAWSLoadBalancerController, AWSLoadBalancerControllerChart, CertManagerChart, clusterIssuer,
IngressNginxChart, ExternalSecretChart, CrossplaneChart, ObserveRoute53Zone, argocdIngress,
backstage, keycloak, providerconfigKeycloak, providerconfigSecretKeycloak, keycloakGroupArgoCD,
keycloakGroupBackstage, keycloakOpenIdClientScope, keycloakOpenIdGroupMembershipProtocolMapper,
keycloakArgoCDClientSecret, keycloakArgoCDOpenIdClient, keycloakArgoCDOpenIdClientDefaultScopes,
keycloakBackstageClientSecret, keycloakBackstageOpenIdClient, keycloakBackstageOpenIdClientDefaultScopes`

Note that 5 of the first 5 are **nested XRs**, 3 are `Usage` deletion-ordering objects, and the rest
are Helm `Release` / provider-keycloak MRs.

### Biggest by composed-resource count in the modern style: `configuration-caas`
`apis/composition.yaml` is **17 lines**; `functions/xcluster/main.k` is **624 lines and
emits 23 named resources**. Its pipeline:

```yaml
spec:
  compositeTypeRef: {apiVersion: caas.upbound.io/v1alpha1, kind: XCluster}
  mode: Pipeline
  pipeline:
  - functionRef: {name: upbound-configuration-caasxcluster}
    step: xcluster
  - functionRef: {name: crossplane-contrib-function-auto-ready}
    step: crossplane-contrib-function-auto-ready
```

### Biggest *pipeline* YAML: `configuration-gcp-gke-castai/apis/fullaccess/composition.yaml`
**582 lines · 3 steps · mixes three functions.** This is the only place in the corpus that mixes
go-templating + P&T + sequencer, and it's the best evidence for "pipeline steps are a real DSL
concept, not just one template":

```yaml
  pipeline:
    - step: render-resources                      # line 14
      functionRef: {name: crossplane-contrib-function-go-templating}
      input:
        apiVersion: gotemplating.fn.crossplane.io/v1beta1
        kind: GoTemplate
        source: Inline
        inline:
          template: |
            {{ $idTrimmed := sha1sum .observed.composite.resource.spec.parameters.id }}
            {{ $sa := substr 0 8 $idTrimmed }}
            ---
            apiVersion: {{ .observed.composite.resource.apiVersion }}
            kind: {{ .observed.composite.resource.kind }}
            status:
              gcp:
                sa: {{ $sa }}
    - step: patch-and-transform                   # line 32, ~530 lines of P&T
      functionRef: {name: crossplane-contrib-function-patch-and-transform}
      input:
        apiVersion: pt.fn.crossplane.io/v1beta1
        kind: Resources
        patchSets: [...]
        resources: [...]
    - step: sequence-render-resources             # line 573
      functionRef: {name: crossplane-contrib-function-sequencer}
      input:
        apiVersion: sequencer.fn.crossplane.io/v1beta1
        kind: Input
        rules:
          - sequence:
              - castai-role-sakey
              - cluster-readonly-to-fullaccess
```
<https://github.com/upbound/configuration-gcp-gke-castai/blob/main/apis/fullaccess/composition.yaml>

**Pattern: "go-templating computes a derived value onto XR status; P&T then patches from that
status."** This is a workaround for P&T's lack of expressions — the DSL makes it unnecessary, but
it shows that *derived scalar values on XR status* (here an 8-char sha1 prefix used as a GCP SA id)
are a real requirement.

### Pipeline step-count distribution (84 pipeline compositions)
`1 step: 23 · 2 steps: 51 · 3 steps: 9 · 4 steps: 1`. **Two steps is the norm and the second is
always `function-auto-ready`.**

---

## 3. Authoring patterns (with counts and real excerpts)

### P-1. Composed-resource-name annotation on *every* resource — 41 hits / 30 of 34 KCL fns

KCL's `krm.kcl.dev/composition-resource-name` is the exact analogue of go-templating's
`setResourceNameAnnotation`. Every single modern composition defines a one-line helper for it:

```python
_metadata = lambda name: str -> any {
    { annotations = { "krm.kcl.dev/composition-resource-name" = name }}
}
```
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>

In the go-template form:
```gotemplate
metadata:
  annotations:
    {{ setResourceNameAnnotation "securitygroup" }}
```
<https://github.com/upbound/configuration-aws-securitygroup/blob/main/functions/xsecuritygroups/compose.yaml.gotmpl>

**Graph GUI: fully structural** — it *is* the node id. Never expose it as a template field.

### P-2. The `_defaults` spread — a per-composition "common spec" block — 23/34 fns

Every composition defines a shared spec fragment merged into every composed MR:

```python
_defaults = {
    managementPolicies = params.managementPolicies or ["*"]
    if providerConfigRefName:
        providerConfigRef = {
            kind = "ProviderConfig"           # v2: kind is required
            name = providerConfigRefName
        }
    forProvider.region = params.region        # aws-database also folds region in
}
# ... then on every resource:
spec = _defaults | { forProvider = { ... } }
```
<https://github.com/upbound/configuration-aws-database/blob/main/functions/sqlinstance/main.k#L15-L23>

Counts: `providerConfigRef` 70 hits / 23 files · `kind = "ProviderConfig"` 26 hits / 17 files ·
`managementPolicies` 80 hits / 23 files · `deletionPolicy` 81 hits / 10 files.
**`ClusterProviderConfig` appears in 0 composition sources** (only in 3 `examples/` files) — Upbound
uses namespaced `ProviderConfig`. Your environment uses `ClusterProviderConfig`; the DSL needs
`providerConfigRef.kind` as a first-class enum, not a hardcode.

XRD side: 73/116 XRDs declare `providerConfigName` (usually `default: default`), 51 declare
`deletionPolicy` (enum Delete/Orphan, default Delete), 25 declare `managementPolicies`.

> **DSL implication: a composition-level `defaults:` block** whose keys are merged into every
> resource node, with per-node opt-out. This alone removes ~40% of the field mappings in a typical
> blueprint. **Graph GUI: structural — a "composition settings" panel, not a node.**

### P-3. Cross-resource reference via `matchControllerRef` — 99 hits / 20 files

The dominant sibling-reference idiom. No name computation, no status read:

```python
ec2v1beta1.InternetGateway{
    spec = _defaults | {
        forProvider = {
            vpcIdSelector = { matchControllerRef = True }
            region = oxr.spec.parameters.region
        }
    }
}
```
<https://github.com/upbound/configuration-aws-network/blob/main/functions/network/main.k>

Top selector fields: `resourceSelector` 58, `clusterNameSelector` 20, `resourceGroupNameSelector` 9,
`vpcIdSelector` 5, `subnetIdSelector` 5, `roleArnSelector` 3, ...

**Graph GUI: fully structural.** Draw an edge A→B, pick the reference field on B; emit
`<field>Selector.matchControllerRef: true`. **This should be the default edge semantics.**

### P-4. Reference narrowed by label — the "role" pattern — 53 `matchLabels` hits / 19 files

When there are two of the same kind, the producer stamps a label and the consumer matches it:

```python
# producer
iamv1beta1.Role { metadata = _metadata("controlplaneRole") | { labels: { role = "controlplane" } } ... }
iamv1beta1.Role { metadata = _metadata("nodegroupRole")    | { labels: { role = "nodegroup"    } } ... }
# consumer
eksv1beta1.Cluster {
    spec.forProvider.roleArnSelector = {
        matchControllerRef = True
        matchLabels = { role = "controlplane" }
    }
}
```
<https://github.com/upbound/configuration-aws-eks/blob/main/functions/eks/main.k>

**Graph GUI: structural.** The edge carries a "disambiguate by label" flag; the tool derives the
label key/value pair and stamps it on the target automatically.

### P-5. Label-keyed reference *across Configurations* — 103 hits

This is Upbound's composability contract, and it is the most important pattern in the corpus for a
platform team. A producing Configuration stamps a well-known label on every MR it creates:

```python
# configuration-aws-network stamps this on VPC, Subnet, RouteTable, SecurityGroup, ...
labels = { "networks.aws.platform.upbound.io/network-id" = oxr.spec.parameters.id }
```

A *different* Configuration declares a plain `networkRef.id` string in its XRD and selects on it:

```python
# configuration-aws-database
rdsv1beta1.SubnetGroup{
    spec.forProvider.subnetIdSelector.matchLabels = {
        "networks.aws.platform.upbound.io/network-id" = params.networkRef.id
    }
}
rdsv1beta1.Instance{
    spec.forProvider.vpcSecurityGroupIdSelector.matchLabels = {
        "networks.aws.platform.upbound.io/network-id" = params.networkRef.id
    }
}
```
<https://github.com/upbound/configuration-aws-database/blob/main/functions/sqlinstance/main.k>

XRD declaration of the ref (deliberately *not* a Crossplane reference triad — just an id):
```yaml
networkRef:
  type: object
  description: "A reference to the Network object that this database should be connected to."
  properties:
    id: {type: string, description: ID of the Network object this ref points to.}
  required: [id]
```
<https://github.com/upbound/configuration-aws-database/blob/main/apis/sqlinstances/definition.yaml#L71-L79>

Label-key frequency: `networks.aws.platform.upbound.io/network-id` 54,
`azure.platform.upbound.io/network-id` 38, `networks.gcp.platform.upbound.io/network-id` 11,
`xgke.gcp.platform.upbound.io/cluster-id` 5, `xeks.aws.platform.upbound.io/cluster-id` 4,
`azure.platform.upbound.io/subnet-service-type` 6, `eks.aws.platform.upbound.io/discovery` 3.

> **DSL implication:** two first-class concepts — **`emitsLabel`** (stamp `<key>: <path>` on every
> resource this composition produces) and **`externalRef`** (an XRD input that becomes
> `matchLabels: {<key>: <value>}` on a named field). **Graph GUI: structural** — an "inbound port"
> and an "outbound label contract" on the composition itself.

### P-6. The reference *triad* exposed at the XRD level — passthrough

`configuration-aws-dynamodb` copies the provider CRD's full triad into the XRD (`tableName`,
`tableNameRef{name,policy{resolution,resolve}}`, `tableNameSelector{matchLabels,matchControllerRef,policy}`)
— ~60 lines of boilerplate schema — and the template passes whichever is set straight through:

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
(XRD triad schema: <https://github.com/upbound/configuration-aws-dynamodb/blob/main/apis/tableitemset/definition.yaml#L77-L150>)

> **DSL implication: this is exactly the "reference triad" the user already planned, and it is
> worth automating in BOTH directions** — generate the ~60-line XRD schema fragment *and* the
> three-branch passthrough template from one declaration like
> `exposeRef: {node: table, field: kmsKeyArn}`. **Graph GUI: fully structural, high leverage.**

### P-7. Conditional resource gated on observed readiness (+ `or exists`) — the #1 logic pattern

Present in essentially every non-trivial composition. AWS EKS has **six** of them:

```python
ready = lambda o: any, statusPath = "atProvider" -> bool {
    status = o?.Resource?.status
    objstatus = status?.conditions or []
    len(objstatus) > 0 and all_true([c.status == "True" for c in objstatus]) and status and statusPath in status
}
exists: (str) -> bool = lambda o: str -> bool { get(_ocds, o, {}) != {} }

if ready(get(_ocds, "kubernetesCluster", "")) or exists("kubernetesClusterAuth"):
    _items += [ eksv1beta1.ClusterAuth { ... } ]

if ready(get(_ocds, "vpc-cni-addon", "")) or exists("nodeGroupPublic"):
    _items += [ eksv1beta1.NodeGroup { ... } ]

if (ready(get(_ocds, "ebsCSIDriverPodIdentityAssociation", "")) and ready(get(_ocds, "nodeGroupPublic", ""))) or exists("aws-ebs-csi-driver-addon"):
    _items += [ eksv1beta1.Addon { addonName = "aws-ebs-csi-driver" ... } ]
```
<https://github.com/upbound/configuration-aws-eks/blob/main/functions/eks/main.k>

Same idiom at the XR-composing-XR level, with an explicit comment about *why* the `or exists` half
matters:

```python
# AWSLBController resource - only create when EKS is ready (needs kubeconfig secret)
# Also create if it already exists to prevent uninstalling
if _isEksReady or _lbControllerExists:
    awsplatformv1alpha1.AWSLBController { ... }

# Oss resource - only create when LB Controller is ready (to avoid webhook conflicts)
if _isLbControllerReady or _ossExists:
    observev1alpha1.Oss { ... }
```
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>

with the readiness probe written as a comprehension:
```python
_eksReadyCondition = [condition for condition in (_ocds?["EKS"]?.Resource?.status?.conditions or [])
                      if condition.type == "Ready" and condition.status == "True"]
_isEksReady = len(_eksReadyCondition) > 0
```

Counts: `_ocds` read in 21/34 fns (130 hits) · `status?.conditions` in 8 fns (13 hits) ·
`status?.atProvider` in 15 fns (38 hits).

> **DSL implication: a `dependsOnReady` edge is the highest-value single feature in this brief.**
> It must generate both halves (`ready(X) or exists(self)`), because the naive version causes
> resource churn. **Graph GUI: fully structural** — a second edge type (dashed "ordering" edge)
> between two nodes.

### P-8. Conditional resource gated on a *parameter* — very common

```gotemplate
{{- if $spec.parameters.tableReplica }}
{{- if $tableStatus.atProvider }}
---
apiVersion: dynamodb.aws.upbound.io/v1beta1
kind: TableReplica
...
{{- end }}
{{- end }}
```
<https://github.com/upbound/configuration-aws-dynamodb/blob/main/apis/table/composition.yaml>

```python
principalArn = oxr.spec?.parameters?.iam?.principalArn or False
if principalArn:
    _items += [ eksv1beta1.AccessEntry {...}, eksv1beta1.AccessPolicyAssociation {...} ]
```

Two YAML-native encodings of the same idea also exist in the corpus:

**(a) `function-cel-filter`** — 1 composition:
```yaml
- functionRef: {name: crossplane-contrib-function-cel-filter}
  input:
    apiVersion: pt.fn.crossplane.io/v1beta1
    kind: Filters
    filters:
      - name: contributorInsights
        expression: observed.composite.resource.spec.parameters.contributorInsights == true
```
<https://github.com/upbound/configuration-aws-dynamodb/blob/main/apis/composition.yaml#L136-L142>

**(b) `upboundcare/function-conditional-patch-and-transform`** — 5 compositions, per-resource CEL:
```yaml
resources:
  - name: XNetworkAWS
    condition: observed.composite.resource.spec.parameters.cloud == "aws"
    base: {apiVersion: aws.platform.upbound.io/v1alpha1, kind: XNetwork}
```
<https://github.com/upbound/platform-ref-multi-k8s/blob/main/apis/composition.yaml>

**Graph GUI: structural** — a boolean `condition` field on the node (CEL-ish or a path + operator +
value builder). Nested/compound conditions (`if A and B or C`) need an expression string.

### P-9. Loop over an XRD array producing N resources — with a computed name

Simplest go-templating form:
```gotemplate
{{ $parameters := .observed.composite.resource.spec.parameters }}
{{- range $i, $tableItem := $parameters.tableItems }}
---
apiVersion: dynamodb.aws.upbound.io/v1beta1
kind: TableItem
metadata:
  annotations:
    {{ setResourceNameAnnotation (print "item-" $i) }}
spec:
  forProvider:
    item: {{ $tableItem.item }}
    tableName: {{ $parameters.tableName }}
{{ end }}
```
<https://github.com/upbound/configuration-aws-dynamodb/blob/main/apis/tableitemset/composition.yaml>

**Nested loop + computed kind + sanitized name** (the hardest real case in the corpus):
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
spec:
  forProvider:
    {{- if $cidrBlock }}
    cidrIpv4: {{ $cidrBlock }}
    {{- end }}
    securityGroupIdSelector:
      matchControllerRef: true
    {{- if $rule.isSelf }}
    referencedSecurityGroupIdSelector:
      matchControllerRef: true
    {{- end }}
    {{- if $rule.sourceSecurityGroupName }}
    referencedSecurityGroupIdRef:
      name: {{ $rule.sourceSecurityGroupName }}
    {{- end }}
  {{- end }}
{{- end }}
```
<https://github.com/upbound/configuration-aws-securitygroup/blob/main/functions/xsecuritygroups/compose.yaml.gotmpl>

**Loop where the name is a semantic key, and siblings reference each other by label derived from
the same key** (KCL, `configuration-aws-network` — one Subnet + one RouteTableAssociation per item):
```python
_cidrEscaped = lambda cidr = str -> str { regex.replace(cidr, "\.|\/", "-") }
_formatSubnet = lambda s = dict -> str { "{}-{}-{}".format(s.availabilityZone, _cidrEscaped(s.cidrBlock), s.type) }

# resource 1 of the pair — stamps zone + access labels
ec2v1beta1.Subnet{
    metadata = _metadata("subnet-" + _formatSubnet(s)) | {
        labels = {
            zone = s.availabilityZone
            if s.type == "private": access = "private"
            else: access = "public"
        }
    }
    spec = _defaults | { forProvider = {
        cidrBlock = s.cidrBlock
        if s.type == "public": mapPublicIpOnLaunch = True
        tags = { if s.type == "private": "kubernetes.io/role/internal-elb" = "1"
                 else: "kubernetes.io/role/elb" = "1" }
        vpcIdSelector = { matchControllerRef = True }
        availabilityZone = s.availabilityZone
    }}
} for s in oxr.spec.parameters.subnets

# resource 2 of the pair — selects its partner by the labels above
ec2v1beta1.RouteTableAssociation{
    metadata = _metadata("rta-" + _formatSubnet(s))
    spec = _defaults | { forProvider = {
        routeTableIdSelector = { matchControllerRef = True }
        subnetIdSelector = {
            matchControllerRef = True
            matchLabels = {
                if s.type == "private": access = "private"
                else: access = "public"
                zone = s.availabilityZone
            }
        }
    }}
} for s in oxr.spec.parameters.subnets
```
<https://github.com/upbound/configuration-aws-network/blob/main/functions/network/main.k>

Same in Python, name derived from index:
```python
for index, db_subnet in enumerate(database_subnets):
    subnet_name = f"{network_id}-db-sn-{index}"
    delegation_name = ("Microsoft.DBforPostgreSQL/flexibleServers"
                       if db_subnet.serviceType == "postgres"
                       else "Microsoft.DBforMySQL/flexibleServers")
    ...
    resource.update(rsp.desired.resources[subnet_name],
                    db_subnet_resource.model_dump(exclude_unset=True, by_alias=True))
```
<https://github.com/upbound/configuration-azure-network/blob/main/functions/network/main.py>

Loop over a *two-dimensional* param (cartesian product) also appears:
```python
} for repo in oxr.spec.parameters.repositories for team in oxr.spec.parameters.permissions?.teams
```
<https://github.com/upbound/platform-ref-upbound/blob/main/functions/xupboundreposet/main.k#L68>

**Frequency:** 132 array-typed properties across 117 XRDs; 27/34 KCL fns contain a comprehension
(123 `for` occurrences). Loop-heavy XRD arrays seen: `subnets`, `rules`, `tableItems`,
`databaseSubnets`, `nodeConfig`, `repositories`, `replica`, `parameterGroupParams`,
`logDeliveryConfiguration`, `secondaryIpRange`, `resources[].methods`.

> **DSL implication:** a loop node needs (1) source path, (2) **name expression** with sanitize
> helpers, (3) per-item conditional fields, (4) **the ability for one loop iteration to emit
> multiple correlated resources**, (5) sibling refs resolved by labels derived from the same item.
> **Graph GUI: the loop container is structural; the name expression and any computed kind need a
> small expression editor or rawTemplate.**

### P-10. Nested XRs — a composition composing another XR — 104 `models.io.upbound.platform` imports

This is Upbound's core architectural move: leaf Configurations own MRs, platform-refs compose XRs.

```python
import models.io.upbound.platform.aws.v1alpha1 as awsplatformv1alpha1
import models.io.upbound.platform.observe.v1alpha1 as observev1alpha1
import models.io.upbound.platform.gitops.v1alpha1 as gitopsv1alpha1

_items = [
    awsplatformv1alpha1.Network { metadata: _metadata("Network") | { name = params.id + "-network" }
        spec.parameters = _defaults | { id = params.id, region = params.region, subnets = _regionSpecificSubnets } }
    awsplatformv1alpha1.EKS { metadata: _metadata("EKS") | {
            name = params.id + "-eks"
            annotations: { "crossplane.io/external-name" = params.id } }
        spec.parameters = _defaults | { id = params.id, region = params.region,
                                        version = params.version, nodes = params.nodes }
                          | ({iam = params.iam} if params.iam else {}) }
    ...
]
```
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>

Note `| ({iam = params.iam} if params.iam else {})` — **conditionally merging a whole sub-object**,
not just a scalar. Go-template equivalent: `{{- with $params.iam }}iam: {{ . | toYaml | nindent 4 }}{{- end }}`.

Nested XRs wired by **explicit computed name refs** (inline-KCL teaching example):
```python
metadata.name = "{}-cluster".format(oxr.metadata.name)
metadata.namespace = oxr.metadata.namespace
spec.parameters = {
  networkRef.name: "{}-net".format(oxr.metadata.name)
  subnetworkRef.name: "{}-subnet".format(oxr.metadata.name)
}
```
<https://github.com/upbound/configuration-getting-started/blob/main/apis/composition-basics/compositecluster/composition.yaml>

> **DSL implication:** the node palette must include **XR kinds from other blueprints/packages**,
> not just provider MRs. And nested XRs need `metadata.name` + `metadata.namespace` (inherit from
> parent XR) set explicitly — see P-14. **Graph GUI: fully structural.**

### P-11. Readiness overrides — 21 hits / 14 of 34 fns. Two sub-patterns.

**(a) Unconditional "this has no meaningful Ready condition" (10 hits / 5 files).**
Applied to `ProviderConfig` and `Usage` objects:
```python
k8sv1alpha1.ProviderConfig {
    metadata = {
        name = params.id
        generateName = "{}-".format(params.id)
        annotations = {
            **_metadata("providerConfig-kubernetes").annotations
            "krm.kcl.dev/ready": "True"
        }
    }
    spec.credentials = { secretRef = { name = "{}-ekscluster".format(params.id)
                                       namespace = oxr.metadata.namespace
                                       key = "kubeconfig" }
                         source = "Secret" }
}
```
<https://github.com/upbound/configuration-aws-eks/blob/main/functions/eks/main.k#L368-L378>

**(b) Readiness *derived* from an observed status field** — exactly the user's `availableReplicas`
case:
```python
# Check if init-ingress has external IP and mark as ready
if "init-ingress" in ocds:
    _observedIngress = ocds["init-ingress"].Resource
    _hasExternalIP = len(_observedIngress?.status?.loadBalancer?.ingress or []) > 0
    if _hasExternalIP:
        _initIngress.metadata.annotations["krm.kcl.dev/ready"] = "True"
```
<https://github.com/upbound/configuration-k8gb-bluegreen/blob/main/functions/k8gb-operator/main.k#L155-L160>

```python
_serviceHealth = _observedGslb?.status?.serviceHealth or {}
if _serviceHealth:
    _unhealthyDomains = [k for k, v in _serviceHealth if v != "Healthy"]
    _gslbHealthy = len(_unhealthyDomains) == 0
    if _gslbHealthy:
        _gslb.metadata.annotations["krm.kcl.dev/ready"] = "True"
```
<https://github.com/upbound/configuration-k8gb-bluegreen/blob/main/functions/application/main.k#L90-L96>

`configuration-aws-ctp` even documents *why* they do it, in the composition YAML:
> "For Helm Releases that hit the provider-helm v1.2.2 stale-Ready bug ... our Python composition
> function adds readiness annotations when `atProvider.state=="deployed"` ... function-auto-ready
> respects that explicit decision and skips its built-in Ready-condition check."

<https://github.com/upbound/configuration-aws-ctp/blob/main/apis/ctp/composition.yaml>

Legacy P&T equivalent — `readinessChecks` — appears in only **12 of 118** compositions, 20 of the
21 checks being `type: None` ("never block on this resource"), 1 `MatchString`.

> **DSL implication: readiness is a THREE-state per-node property** — `auto` (default),
> `alwaysReady` (→ `gotemplating.fn.crossplane.io/ready: "True"`), and
> `readyWhen: <expression over observed status>`. Sub-pattern (a) is fully structural (checkbox);
> (b) needs a path + comparison builder, with rawTemplate for anything compound.

### P-12. Connection detail publishing — 7 fns emit `CompositeConnectionDetails`; 18 compositions use P&T `connectionDetails`

**Modern (function-native) form** — a synthetic meta object in the output stream:
```python
{
    apiVersion: "meta.krm.kcl.dev/v1alpha1"
    kind: "CompositeConnectionDetails"
    if "EKS" in _ocds:
        data: { kubeconfig = _ocds["EKS"].ConnectionDetails.kubeconfig }
    else:
        data: {}
}
```
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>
(go-templating equivalent kind: `meta.gotemplating.fn.crossplane.io/v1alpha1 / CompositeConnectionDetails`)

**Mixing passthrough with derived + base64:**
```python
{
    apiVersion: "meta.krm.kcl.dev/v1alpha1"
    kind: "CompositeConnectionDetails"
    if "RDSInstanceSmall" in _ocds:
        data: {
            endpoint = _ocds["RDSInstanceSmall"].Resource?.status?.atProvider?.endpoint
            host     = _ocds["RDSInstanceSmall"].Resource?.status?.atProvider?.address
            username = base64.encode(_ocds["RDSInstanceSmall"].Resource?.spec?.forProvider?.username)
            password = _ocds["RDSInstanceSmall"].ConnectionDetails.password
        }
    else:
        data: {}
}
```
<https://github.com/upbound/configuration-aws-database/blob/main/functions/sqlinstance/main.k>

Note: **values sourced from `.status.atProvider` must be base64-encoded by the author; values from
`.ConnectionDetails` are already encoded.** That asymmetry is a classic footgun the generator
should handle automatically.

Producer side: `writeConnectionSecretToRef` 20 hits / 12 fns, usually
`name = "{}-sql".format(oxr.metadata.name)` or `"{}-ekscluster".format(params.id)`.

Legacy: `connectionDetails` in 18 compositions, all 11 typed entries `FromConnectionSecretKey`.
`writeConnectionSecretsToNamespace` still set in 44 compositions (36 `upbound-system`,
8 `crossplane-system`) — **including v2 ones like platform-ref-aws**, even though it's deprecated in v2.

> **Graph GUI: fully structural.** A "connection details" panel on the XR node: rows of
> `{key, source: node.connectionDetails.X | node.status.path | literal}`, with encoding handled.

### P-13. XR status derivation from observed composed resources — 20/34 fns write `_dxr`

Three shapes, all common:

**(a) Simple field lift:**
```python
_dxr = _dxr | {
    status: {
        eks: {
            clusterName: _ocds["kubernetesCluster"]?.Resource?.metadata?.name
            clusterArn: _clusterAtProvider?.arn
            oidcIssuerUrl: _oidcIssuerUrl
            nodeGroupArn: _ngAtProvider?.arn
            nodeGroup: { instanceType: _ngInstanceTypes[0] if len(_ngInstanceTypes) > 0 else "" }
        }
    }
}
_items += [_dxr]
```
<https://github.com/upbound/configuration-aws-eks/blob/main/functions/eks/main.k#L435-L449>
(with defensive index guards: `_oidcList = (_identity[0]?.oidc or []) if len(_identity) > 0 else []`)

**(b) Aggregate a list across loop-produced resources, then filter it:**
```python
_getExternalName = lambda resourceName = str -> str {
    id = option("params")?.ocds?[resourceName]?.Resource?.metadata?.annotations?["crossplane.io/external-name"] or None
}
createdSubnets = [ c for c in [ { id = _getExternalName(r.name), type = r.type }
                                for r in [ {name = "subnet-" + _formatSubnet(s), type = s.type}
                                           for s in oxr.spec.parameters.subnets ] ]
                   if c.id != None ]
...
status = {
    if vpcId: vpcId = vpcId
    subnetIds        = [s.id for s in createdSubnets]
    publicSubnetIds  = [s.id for s in createdSubnets if s.type == "public"]
    privateSubnetIds = [s.id for s in createdSubnets if s.type == "private"]
    securityGroupIds = [securityGroupId]
}
```
<https://github.com/upbound/configuration-aws-network/blob/main/functions/network/main.k>

**(c) Whole-block dump:**
```gotemplate
apiVersion: {{ .observed.composite.resource.apiVersion }}
kind: XTable
{{- if $tableStatus.atProvider }}
status:
  dynamodbTable:
  {{ $tableStatus.atProvider | toYaml | nindent 4 }}
  {{- if $tableReplicaStatus.atProvider }}
  dynamodbTableReplica:
  {{ $tableReplicaStatus.atProvider | toYaml | nindent 4 }}
  {{- end }}
{{ else }}
status: {}
{{- end }}
```
<https://github.com/upbound/configuration-aws-dynamodb/blob/main/apis/table/composition.yaml>

85 of 117 XRDs declare a `status` schema; 10 declare `additionalPrinterColumns`.

Also seen: reading the XR's *own* status back as a cross-resource reference (a two-reconcile
round-trip): `globalTableArn: {{ $xTableStatus.dynamodbTable.arn }}` in the same file.

> **DSL implication:** status derivation is a per-XRD-field mapping list
> (`status.X ← observed.resources[N].resource.status.atProvider.Y`), which the GUI can express
> structurally. **Shapes (a) and (c) are structural. Shape (b) — aggregate-across-loop-then-filter
> — is the one case that genuinely needs an expression/rawTemplate.**

### P-14. Namespaced-XR (v2) mechanics that the generator must get right

From the Python function, with the author's own comments:
```python
return ObjectMeta(
    name=name,
    namespace=observed_xr.metadata.namespace,  # v2: inherit from XR
    labels=labels
)

def create_provider_config_ref() -> dict:
    """Create ProviderConfigRef with v2 required fields."""
    return {
        "kind": "ProviderConfig",  # v2: required kind
        "name": provider_config_name
    }
```
<https://github.com/upbound/configuration-azure-network/blob/main/functions/network/main.py#L49-L69>

Also seen: `namespace = oxr.metadata.namespace` on composed `ProviderConfig` and on
`spec.credentials.secretRef.namespace`; and
`connectionSecretNamespace = oxr.spec?.writeConnectionSecretToRef?.namespace or "upbound-system"`.

> **DSL implication:** `metadata.namespace` inheritance and `providerConfigRef.kind` are v2
> invariants. **Emit them automatically; don't make the user wire them.** Fully structural.

### P-15. `Usage` objects for deletion ordering — 10 hits / 3 fns, plus 3 in the legacy corpus

Two API versions in flight (`protection.crossplane.io/v1beta1` in v2 repos,
`apiextensions.crossplane.io/v1alpha1` in older ones):
```python
{
    apiVersion: "protection.crossplane.io/v1beta1"
    kind: "Usage"
    metadata: _metadata("usageXNetworkByEKS") | { name = params.id + "-network-by-eks" }
    spec: {
        replayDeletion = True
        by: { apiVersion = "aws.platform.upbound.io/v1alpha1", kind = "EKS",
              resourceSelector: { matchControllerRef = True } }
        of: { apiVersion = "aws.platform.upbound.io/v1alpha1", kind = "Network",
              resourceSelector: { matchControllerRef = True } }
    }
}
```
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>

`platform-ref-aws` emits **6** Usage objects for a 5-resource composition. Some select by arbitrary
label instead of controller ref (`matchLabels: {"platform.upbound.io/deletion-ordering": "enabled"}`)
to order against resources composed *elsewhere*.

In go-templating, the target is identified by mirroring the resource-name annotation into a **label**
so the Usage's `resourceSelector.matchLabels` can find it:
```gotemplate
metadata:
  annotations:
    gotemplating.fn.crossplane.io/composition-resource-name: {{ $spec.parameters.providerConfigName }}-{{ $provider.name }}-p
  labels:
    gotemplating.fn.crossplane.io/composition-resource-name: {{ $spec.parameters.providerConfigName }}-{{ $provider.name }}-p
---
apiVersion: apiextensions.crossplane.io/v1alpha1
kind: Usage
spec:
  by:
    apiVersion: kubernetes.crossplane.io/v1alpha2
    kind: Object
    resourceSelector:
      matchControllerRef: true
      matchLabels:
        gotemplating.fn.crossplane.io/composition-resource-name: {{ $spec.parameters.providerConfigName }}-{{ $provider.name }}-p
  of:
    apiVersion: helm.crossplane.io/v1beta1
    kind: Release
    resourceSelector: {matchControllerRef: true, matchLabels: {type: crossplane}}
```
<https://github.com/upbound/platform-ref-upbound-spaces/blob/main/apis/space-init/composition.yaml>

> **DSL implication:** an ordering edge should be able to *generate* a Usage object (with the
> annotation→label mirroring), rather than making users hand-write 25 lines of YAML per edge.
> **Graph GUI: fully structural** — same dashed edge as `dependsOnReady`, different backend.

### P-16. `function-sequencer` for ordering without Usage — 5 compositions

```yaml
- step: sequence-render-resources
  functionRef: {name: crossplane-contrib-function-sequencer}
  input:
    apiVersion: sequencer.fn.crossplane.io/v1beta1
    kind: Input
    rules:
      - sequence:
          - castai-role-sakey
          - cluster-readonly-to-fullaccess
```
> **Graph GUI: fully structural** — the topological order of ordering-edges *is* the sequence list.
> This is a cheap alternative to P-7's hand-written readiness guards and worth emitting by default.

### P-17. `function-extra-resources` — fetching cluster state outside the XR — 4 compositions

```yaml
- functionRef: {name: crossplane-contrib-function-extra-resources}
  step: fetch-control-planes
  input:
    apiVersion: extra-resources.fn.crossplane.io/v1beta1
    kind: Input
    spec:
      extraResources:
      - apiVersion: aws.platform.upbound.io/v1alpha1
        kind: ControlPlane
        into: allControlPlanes
        type: Selector
        selector: {maxMatch: 100, minMatch: 0}
```
<https://github.com/upbound/configuration-aws-ctp/blob/main/apis/ctp/composition.yaml> ·
<https://github.com/upbound/configuration-resilient-ctp/blob/main/apis/resilientcontrolplane/composition.yaml>

Note **zero KCL functions read `option("params").eres` or `ctx`** — the fetched resources are
consumed by the Python functions. 4/118 = niche.
> **Graph GUI: structural (a "lookup" node feeding the graph), but low priority.**

### P-18. Dict-based value mapping — region → AZ list, type → delegation

```python
_regionAzMap = {
    "us-east-1": ["us-east-1a", "us-east-1b"]
    "us-west-2": ["us-west-2a", "us-west-2b"]
    "eu-central-1": ["eu-central-1a", "eu-central-1b"]
    ...
}
_availabilityZones = _regionAzMap[params.region] if params.region in _regionAzMap else ["us-west-2a", "us-west-2b"]
```
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>

P&T equivalent (`type: map` transform) appears 14 times; `type: match` 10 times; `type: string`
127 times (66 `Format`, 9 `Regexp`, 4 `TrimPrefix`, 2 `Convert`).
> The user's existing dict-based mapping covers this. **Graph GUI: structural (a lookup table
> editor + default).**

### P-19. Selector built conditionally on XR shape

```python
clusterNameSelector = {
    matchLabels = {
        "crossplane.io/claim-name" = params.id
    } if oxr.spec?.claimRef?.name else {
        "crossplane.io/composite" = params.id
    }
}
```
<https://github.com/upbound/platform-ref-aws/blob/main/functions/cluster/main.k>
> A v1-claim / v2-XR compatibility shim. **Graph GUI: needs a raw escape** (a ternary inside a
> field value). Appears once — correctly a `rawTemplate` case.

### P-20. Forced re-creation via a hashed resource name

```python
eksv1beta1.AccessEntry {
    # force recreate when principalArn changes
    metadata = _metadata(crypto.sha256("accessEntry-{}".format(principalArn)))
    ...
}
```
<https://github.com/upbound/configuration-aws-eks/blob/main/functions/eks/main.k>
> Composed-resource name derived from a hash of a parameter so that changing the parameter
> *replaces* the resource rather than updating it. **Graph GUI: a "recreate on change of <field>"
> checkbox is expressible structurally**; the sha256 is generated. Appears twice — worth a note,
> maybe not v1.

### P-21. Things Upbound does NOT do (negative results, all verified by grep)

| Pattern | Occurrences in 71 repos |
|---|---|
| `EnvironmentConfig` / `FromEnvironmentFieldPath` / `spec.environment` | **0** |
| `ClusterProviderConfig` in composition sources | **0** (3 in `examples/` only) |
| Composing native `apps/v1 Deployment` / `v1 Service` directly (XP v2 capability) | **0** — they still wrap in `kubernetes.crossplane.io Object` (6 uses) or `helm Release` (9 uses) |
| `getResourceCondition`, `dig`, `b64enc`, `include`/`define` in go-templates | **0** |
| Reading `option("params").ctx` (function context) | **0** |
| `x-kubernetes-validations` on XRD schemas | 5 XRDs |

The `Object`-wrapper habit is worth calling out: **your XMicroservice composing a native Deployment
+ Service is ahead of Upbound's own reference platforms.** The DSL should treat arbitrary GVKs as
first-class node types, not assume "provider MR or nothing".

---

## 4. How they structure multi-provider / multi-cloud variants

Three distinct strategies coexist, and they map to three eras:

**(A) One repo, one API group, per cloud — the current flagship strategy.**
`platform-ref-aws` / `platform-ref-gcp` / `platform-ref-azure` each define `kind: Cluster` in
`{aws,gcp,azure}.platformref.upbound.io`, each with **one** composition and **one** KCL function
(363 / 200 / 187 lines). No shared XRD, no variant selection — full duplication, deliberately.
Their `composition.yaml` files are byte-for-byte identical except for the group and the embedded
function name.

**(B) One XRD with a `cloud` discriminator, branching inside the function** — `configuration-caas`:
```python
cloud = oxr.spec.parameters.cloud
_items = []
if cloud == "aws":
    _items += [ platformawsv1alpha1.XNetwork{...}, platformawsv1alpha1.XEKS{...} ]
# ... elif azure / gcp, each branch composing that cloud's XRs
_isEksReady / _isAksReady / _isGkeReady computed independently up front
```
<https://github.com/upbound/configuration-caas/blob/main/functions/xcluster/main.k>

**(C) One XRD, one composition, per-resource CEL conditions** — `platform-ref-multi-k8s`:
```yaml
- name: XNetworkAWS
  condition: observed.composite.resource.spec.parameters.cloud == "aws"
  base: {apiVersion: aws.platform.upbound.io/v1alpha1, kind: XNetwork}
- name: XEKS
  condition: observed.composite.resource.spec.parameters.cloud == "aws"
  ...
```
(uses `upboundcare/function-conditional-patch-and-transform`, 376 lines, 3 clouds)

**(D) Variant selection delegated to the *child* XR's composition selector** — the composition
patches a label into the nested XR's `spec.compositionSelector`:
```yaml
- type: FromCompositeFieldPath
  fromFieldPath: spec.parameters.networkSelector
  toFieldPath: spec.compositionSelector.matchLabels[type]
```
Seen in `platform-ref-aws-cnoe`, `platform-ref-aws-castai`, `platform-ref-multi-k8s` (3×),
`platform-ref-upbound-spaces` (3×), `configuration-aws-icp`.

> **DSL implication:** (A) says **the generator should make it trivial to produce three
> near-identical blueprints from one parameterised source** — a blueprint-level "provider profile".
> (B)/(C) say a **top-level `variants:` construct** (one XRD, N mutually-exclusive resource sets
> keyed off a discriminator field) is a real, recurring shape worth first-class support; a graph
> GUI can express it as a "variant group" swimlane. (D) is a passthrough field mapping — structural.

---

## 5. File / directory layout conventions

**Near-universal, and extremely consistent.** 115 of 118 composition files are named exactly
`composition.yaml`; 116 of 116 XRD files are named exactly `definition.yaml`; and 115 of 118
composition files sit **in the same directory as their XRD**.

```
configuration-aws-network/
├── upbound.yaml                        # meta.dev.upbound.io/v2alpha1 Project (35 repos)
│                                       #   (or crossplane.yaml = classic Configuration pkg, 34 repos)
│                                       #   (or crossplane-project.yaml = dev.crossplane.io/v1alpha1, 1 repo)
├── apis/
│   └── networks/                       # one dir per XRD, named after the plural
│       ├── definition.yaml             # the XRD
│       ├── composition.yaml            # 16 lines: compositeTypeRef + 2 pipeline steps
│       └── mrap.yaml                   # ManagedResourceActivationPolicy (v2 only, 2 repos)
├── functions/
│   └── network/                        # dir name == the pipeline step name
│       ├── main.k + kcl.mod + kcl.mod.lock          # KCL   (26 repos)
│       ├── main.py + requirements.txt               # Python (7 repos)
│       ├── src/function.ts + package.json           # TS    (1 repo)
│       └── compose.yaml.gotmpl                      # go-tmpl (1 repo)
├── examples/
│   ├── networks/configuration-aws-network.yaml      # example XR
│   └── providerconfig.yaml
├── tests/
│   ├── test-network/main.k                          # `up test run` composition tests
│   ├── test-network-status/main.k
│   └── e2etest-network/main.k                       # `up test run --e2e`
└── .github/workflows/{ci,composition-tests,e2e,tag,yamllint}.yaml
```

- `apis/` present in all; **61/71 have `examples/`**, **36/71 have `tests/`**.
- Directory depth of `composition.yaml`: 3 segments (`repo/apis/composition.yaml`) 27×,
  4 (`repo/apis/<plural>/composition.yaml`) 72×, 5 (`repo/apis/<group>/<plural>/…`) 18×.
- Multi-XRD repos nest by domain: `configuration-getting-started/apis/primitives/{cluster,database,
  network,nodepool,serviceaccount,subnetwork}/` and `apis/composition-basics/{accountscaffold,compositecluster}/`.
- **The embedded-function directory name equals the pipeline step name**, and the generated
  function package name is `<org>-<repo><step>` — e.g. step `network` in
  `configuration-aws-network` → `functionRef.name: upbound-configuration-aws-networknetwork`.
  This is mechanically derivable and the generator must reproduce it exactly.

Project manifest (dependency pinning lives here, not in the composition):
```yaml
apiVersion: meta.dev.upbound.io/v2alpha1
kind: Project
metadata: {name: configuration-aws-network}
spec:
  apiDependencies:
  - http: {url: 'https://raw.githubusercontent.com/crossplane/crossplane/refs/tags/v2.1.4/cluster/crds/apiextensions.crossplane.io_managedresourceactivationpolicies.yaml'}
    type: crd
  dependsOn:
  - {apiVersion: pkg.crossplane.io/v1, kind: Provider, package: xpkg.upbound.io/upbound/provider-aws-ec2, version: v2}
  - {apiVersion: pkg.crossplane.io/v1, kind: Function, package: xpkg.upbound.io/crossplane-contrib/function-auto-ready, version: '>=v0.0.0'}
  repository: xpkg.upbound.io/upbound/configuration-aws-network
  source: github.com/upbound/configuration-aws-network
```
<https://github.com/upbound/configuration-aws-network/blob/main/upbound.yaml>

Configuration-on-Configuration dependency (how P-5 label contracts are versioned):
```yaml
  dependsOn:
  - configuration: xpkg.upbound.io/upbound/configuration-aws-network
    version: v0.24.0
```
<https://github.com/upbound/configuration-aws-securitygroup/blob/main/upbound.yaml>

Newest form (upstream, not Upbound-specific), with **model codegen declared in the manifest**:
```yaml
apiVersion: dev.crossplane.io/v1alpha1
kind: Project
spec:
  crossplane: {version: '>=v2.0.0-0'}
  # Generate TypeScript models from the CRDs of every dependency plus the
  # XRDs in apis/, so the embedded function can import them type-safely.
  schemas:
    languages: [typescript]
  dependencies:
  - type: xpkg
    xpkg: {apiVersion: pkg.crossplane.io/v1, kind: Provider,
           package: xpkg.upbound.io/upbound/provider-aws-ec2, version: '>=v2.7.0'}
```
<https://github.com/upbound/configuration-aws-network-ts/blob/main/crossplane-project.yaml>

Function packages actually referenced across the corpus (from `examples/functions.yaml` +
manifests): `function-auto-ready` 35 (+3 pinned), `function-extra-resources` 4,
`function-patch-and-transform` 14 (across 9 distinct pinned versions v0.2.1→v0.10.7),
`function-go-templating` 3 (v0.4.1, v0.10.0), `function-sequencer` 3, `function-kcl` 1,
`function-cel-filter` 1, `upboundcare/function-conditional-patch-and-transform` 1, plus Upbound's
own `function-claude`, `function-rds-metrics`, `function-aws-query`, `function-management-policies`,
`function-remediation-gate`, `function-event-filter`, `function-analysis-gate`, `function-apply-resource`.

---

## XRD schema-feature census (117 XRDs) — sizing the XRD generator

| Feature | Occurrences |
|---|---|
| `default:` | 428 |
| `required:` | 366 |
| `enum:` | 201 |
| `type: array` | 132 |
| `status` sub-schema | 85 XRDs |
| `additionalProperties` (free-form map, e.g. `tags`) | 40 |
| `pattern:` (regex validation) | 29 |
| `additionalPrinterColumns` | 10 XRDs |
| `x-kubernetes-validations` (CEL) | 5 XRDs |

Best CEL example (mutually-exclusive fields on an array item, guarding a loop):
```yaml
rules:
  type: array
  items:
    type: object
    x-kubernetes-validations:
      - rule: "!(has(self.cidrBlocks) && has(self.sourceSecurityGroupName))"
        message: "cidrBlocks cannot be set together with sourceSecurityGroupName."
      - rule: "!(has(self.fromPort) && has(self.toPort)) || (has(self.cidrBlocks) || has(self.sourceSecurityGroupName))"
        message: "When fromPort and toPort are specified, either cidrBlocks or sourceSecurityGroupName must be set."
      - rule: "!has(self.isSelf) || (self.isSelf == false || (!has(self.cidrBlocks) && !has(self.sourceSecurityGroupName)))"
        message: "When isSelf is true, neither cidrBlocks nor sourceSecurityGroupName can be specified."
      - rule: "!(self.protocol == '-1' && (has(self.fromPort) || has(self.toPort)))"
        message: "When protocol is -1, toPort and fromPort cannot be set."
```
<https://github.com/upbound/configuration-aws-securitygroup/blob/main/apis/xsecuritygroups/definition.yaml>

`default:` outnumbering everything (428 across 117 XRDs, ~3.7 per XRD) is the strongest single
signal for the XRD side of the DSL: **defaults are how Upbound keeps `spec.parameters` small**, and
a generator that doesn't make defaults trivial to declare will produce XRDs nobody wants to use.

---

## Pattern → GUI expressibility summary

| # | Pattern | Files/hits | Graph GUI |
|---|---|---|---|
| P-1 | composition-resource-name annotation | 41 / 30 fns | **structural** (node id) |
| P-2 | `_defaults` spread (providerConfigRef, mgmtPolicies, deletionPolicy, region) | 70/80/81 hits | **structural** (composition settings) |
| P-3 | ref via `matchControllerRef` | 99 / 20 fns | **structural** (default edge) |
| P-4 | ref narrowed by role label | 53 / 19 fns | **structural** (edge + auto label) |
| P-5 | cross-Configuration label contract | 103 hits | **structural** (`emitsLabel` / `externalRef`) |
| P-6 | XRD-level reference triad passthrough | 3 fields × 60 schema lines | **structural**, high leverage |
| P-7 | conditional on observed readiness `+ or exists` | ~15 sites | **structural** (`dependsOnReady` edge) — must emit both halves |
| P-8 | conditional on parameter | very common | **structural** (condition field); compound → expression |
| P-9 | loop over XRD array (incl. nested, computed name/kind) | 132 arrays, 27 fns | container structural; **name expr + dynamic kind need raw escape** |
| P-10 | nested XRs | 104 imports | **structural** (XR node type) |
| P-11a | `ready: "True"` override | 10 / 5 fns | **structural** (checkbox) |
| P-11b | readiness derived from status | ~4 sites | path+comparison builder; compound → raw |
| P-12 | `CompositeConnectionDetails` | 7 fns, 18 legacy | **structural** (panel; auto base64) |
| P-13a/c | XR status lift / whole-block dump | 20 fns, 85 XRDs | **structural** |
| P-13b | aggregate-across-loop + filter | 1 site (network) | **raw escape** |
| P-14 | v2 namespace + providerConfigRef.kind | pervasive | **structural** (auto-emitted) |
| P-15 | `Usage` deletion ordering | 10 / 3 fns | **structural** (ordering edge → generated Usage) |
| P-16 | `function-sequencer` | 5 comps | **structural** (topological order) |
| P-17 | `function-extra-resources` | 4 comps | structural, low priority |
| P-18 | dict value mapping | 14 `map` + several KCL dicts | **structural** (lookup table) |
| P-19 | claim/composite selector shim | 1 | **raw escape** |
| P-20 | hashed name to force recreate | 2 | structural checkbox + generated hash |

**Everything appearing in >20% of the corpus is structurally expressible.** The genuine raw-escape
cases are exactly three: **computed resource names/kinds inside loops (P-9), aggregate-then-filter
status derivation (P-13b), and one-off ternaries inside a field value (P-19).** That is a strong
validation of the "rich DSL + per-field rawTemplate" design.
