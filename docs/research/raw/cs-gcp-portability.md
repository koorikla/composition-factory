# GCP provider portability probe — does the reference grammar hold outside AWS?

**Verdict up front: YES, and GCP is *cleaner* than AWS.** The upjet reference triad, the
description grammar, and the Crossplane v2 namespaced spec envelope are byte-for-byte the same
on GCP as on AWS. Every divergence found traces to a *provider-config* decision (a Go file in
the provider repo), never to the generator. That is the line the DSL must draw.

---

## What this means for the DSL — 5 bullets

1. **Make the reference triad a first-class DSL node type, not a heuristic.** 578/578 GCP refs
   (100%) have a matching `<stem>Selector`; 578/578 selectors have a matching `<stem>Ref`.
   1042 refs across GCP+AWS. This is the single highest-frequency structural pattern in the
   entire provider surface — higher than any go-templating idiom.
2. **Drop the `{stem, stem+"s"}` value-field rule; use `stem` only, plus a per-provider
   override table.** `stem+"s"` fires **34× on AWS and 0× on GCP** — it is not a convention, it
   is 95 hand-written `RefFieldName:` overrides in `provider-upjet-aws/config/`. GCP ships
   exactly **2** override lines. Hard-coding AWS's plural rule would mis-resolve nothing on GCP
   but would silently generalise an AWS-only artefact.
3. **The AWS-derived regexes parse GCP at 100.00% (1156/1156 descriptions) — but they are
   still wrong.** They fail 2/928 on AWS because upjet omits `" in <group>"` for same-package
   references and appends free text. Ship the corrected regexes (§3) — they hit **100% on both
   corpora (2084 descriptions)** and are provably exhaustive because they are the inverse of a
   single `fmt.Sprintf` in upjet.
4. **Detect refs by *shape*, and confirm with *name* + *description* — never by name alone.**
   22 GCP properties and 4 AWS properties are named `*Ref`/`*Refs`/`*Selector` but are ordinary
   API fields (`nodeSelector`, `configMapRef`, `localTrafficSelector`). One AWS field
   (`iam.Role.inlinePolicy`) has the exact `{name, policy}` *shape* of a cluster-scoped ref and
   is not one. Only the 3-way agreement is safe.
5. **Two things must be data-driven per provider, and nothing else:** (a) the ProviderConfig
   surface (GCP requires `projectID`; AWS does not, and their `credentials.source` enums are
   disjoint) and (b) the *location* field (`region` is OpenAPI-`required` on **246/279** AWS MRs
   and on **0** GCP MRs, where the equivalents are `location`/`region`/`zone` on 57/38/11 MRs
   and `project` is *optional* on 319/405). A generator that emits `region:` unconditionally
   produces invalid GCP compositions.

---

## 0. Provenance, licence, method

| item | value |
|---|---|
| Provider family | `upbound/provider-gcp-*` v3.0.1 (all 80 subpackages) + `provider-family-gcp:v3.0.1` |
| Licence | **Apache-2.0** — `meta.crossplane.io/license: Apache-2.0` in every package's `meta.pkg.crossplane.io/v1 Provider` doc; `LICENSE` at `crossplane-contrib/provider-upjet-gcp` is Apache License 2.0 |
| Upstream source | `meta.crossplane.io/source: github.com/crossplane-contrib/provider-upjet-gcp` |
| Digests | `provider-gcp-storage:v3.0.1` → `sha256:733e76cb597812e6b88643d0fb5273d8bc739ef6711e589fc83cb5179294d316`; `provider-family-gcp:v3.0.1` → `sha256:7f5a0df0761efc6ea19a72be2fa06cf7e187becf7bba5a32db165a3deec43df7` |
| AWS control corpus | `upbound/provider-aws-{ec2,rds,s3,sqs,iam,lambda,eks,elasticache,dynamodb,kms,sns,cloudwatch,route53,efs,apigateway,ecs}:v2.7.1` + `provider-family-aws:v2.7.1` (563 CRDs / 279 namespaced MRs) |
| Extraction | reused `scratchpad/xpkgget/main.go` (go-containerregistry, base layer only via the `io.crossplane.xpkg:<digest>=base` config label). **No cluster was touched.** e.g. `provider-gcp-storage` base layer = 126 474 B compressed of an 82 811 724 B image |
| Corpus | 815 GCP CRDs (26 MB NDJSON) at `scratchpad/gcpall/all.ndjson`; 563 AWS CRDs at `scratchpad/awsall/all.ndjson` |
| Analysis | `scratchpad/gcpall/refscan2.py` (structural, shape-first), `scratchpad/feat.py` |

Pull command shape (reproducible, no Docker, no cluster):

```
./xpkgget/xpkgget xpkg.upbound.io/upbound/provider-gcp-storage:v3.0.1 out.yaml
yq -o=json -I=0 'select(.kind=="CustomResourceDefinition")' out.yaml >> all.ndjson
```

---

## 1. Does GCP ship the same dual cluster-scoped + `.m.` namespaced CRD pair?

**Yes. Exactly, with zero exceptions.**

```
GCP v3.0.1, 80 subpackages, 815 CRDs total
  scope Cluster    : 408
  scope Namespaced : 407
  group *.gcp.upbound.io   (cluster) : 407
  group *.gcp.m.upbound.io (namespaced) : 408

Managed resources: 405 kinds, EACH shipped twice.
  clusterKinds=405  mKinds=405  paired=405   (0 cluster-only, 0 m-only)
```

The scope/group counts are off-by-one in opposite directions for exactly one CRD, which is the
same exception AWS has:

```
gcp.m.upbound.io  ClusterProviderConfig  Cluster      v1beta1   <-- .m. group, Cluster scope
gcp.m.upbound.io  ProviderConfig         Namespaced   v1beta1
gcp.m.upbound.io  ProviderConfigUsage    Namespaced   v1beta1
gcp.upbound.io    ProviderConfig         Cluster      v1beta1
gcp.upbound.io    ProviderConfigUsage    Cluster      v1beta1
```

Structural corroboration from the source repo — the generator emits two parallel trees:

```
$ curl .../repos/crossplane-contrib/provider-upjet-gcp/contents/apis?ref=v3.0.0
cluster
namespaced
$ ... /contents/apis/cluster?ref=v3.0.0    -> 82 dirs
$ ... /contents/apis/namespaced?ref=v3.0.0 -> 82 dirs   (identical name list)
```
https://github.com/crossplane-contrib/provider-upjet-gcp/tree/v3.0.0/apis

**Per-group MR kind counts (each ×2 CRDs), 80 groups:**

```
compute:96 bigquery:19 cloudplatform:16 apigee:15 storage:14 bigtable:10 monitoring:9
pubsub:8 logging:8 identityplatform:8 dialogflowcx:8 kms:7 iap:7 gemini:7 privateca:6
networksecurity:6 dns:6 datacatalog:6 accesscontextmanager:6 sql:5 notebooks:5
networkconnectivity:5 dataproc:5 dataplex:5 cloudrun:5 certificatemanager:5 appengine:5
vertexai:4 tags:4 spanner:4 datalossprevention:4 secretmanager:3 redis:3 healthcare:3
firestore:3 filestore:3 eventarc:3 developerconnect:3 datastream:3 containerazure:3
container:3 beyondcorp:3 alloydb:3 storagetransfer:2 sourcerepo:2 osconfig:2 modelarmor:2
memorystore:2 iam:2 gkehub:2 firebaserules:2 containeraws:2 cloudidentity:2 cloudfunctions:2
cloudbuild:2 binaryauthorization:2 artifact:2 workflows:1 vpcaccess:1 servicenetworking:1
oslogin:1 orgpolicy:1 networkservices:1 networkmanagement:1 mlengine:1 memcache:1 gke:1
essentialcontacts:1 documentai:1 datafusion:1 dataflow:1 containerattached:1
containeranalysis:1 composer:1 cloudtasks:1 cloudscheduler:1 cloudquotas:1 cloudfunctions2:1
cloud:1 activedirectory:1
```

### PATTERN `DualScopeCRDPair`
- **Count:** 405/405 GCP kinds (100%); same on AWS.
- **Excerpt:** `buckets.storage.gcp.upbound.io` (Cluster) / `buckets.storage.gcp.m.upbound.io` (Namespaced) — `xpkg.upbound.io/upbound/provider-gcp-storage:v3.0.1`
- **GUI:** structural. One node type per *kind*; scope is a project-level switch, not a per-node choice. A v2 namespaced XRD always targets the `.m.` twin — pure string transform on the group.

---

## 2. Does the `*Ref` / `*Selector` / value triad hold? Exact counts.

Detection was done **structurally** (property-shape match), not by name, so the naming
convention itself could be measured rather than assumed.

Shapes present in `spec.forProvider` of the 405 GCP namespaced MRs:

```
   578  OBJ[matchControllerRef,matchLabels,namespace,policy]   <- NamespacedSelector
   541  OBJ[name,namespace,policy]                             <- NamespacedReference (singular)
    37  ARRAY[name,namespace,policy]                           <- []NamespacedReference (list)
   116  OBJ[key,name]                                          <- LocalSecretKeySelector (115 real + 1 lookalike)
```

### Headline numbers

| metric | **GCP** (405 MRs) | AWS (279 MRs) |
|---|---|---|
| structural refs in `forProvider` | **578** (541 singular + 37 list) | 464 (409 + 55) |
| structural selectors in `forProvider` | **578** | 464 |
| refs with a sibling `<stem>Selector` | **578 / 578 = 100.00%** | 464 / 464 = 100.00% |
| selectors with a sibling `<stem>Ref`/`Refs` | **578 / 578 = 100.00%** | 464 / 464 = 100.00% |
| value field == **exact stem** | **577** | 429 |
| value field == **stem + "s"** | **0** | **34** |
| unresolved | **1** (0.17%) | 1 (0.22%) |
| **resolve rate** | **99.83%** | 99.78% |
| ref names ending in `Ref`/`Refs` | 578/578 | 464/464 |
| list-refs ending in `Refs` (never `Ref`) | 37/37 | 55/55 |
| target GVK resolvable from the description | **578/578 = 100%** | 444/464 (20 misses are packages I didn't pull) |
| cross-API-group refs | **197 = 34.1%** | 139 = 30.0% |
| refs nested under an array (`[]` in path) | **103 = 17.8%** | 29 = 6.2% |
| MRs carrying ≥1 ref | 285 / 405 (70%) | 229 / 279 (82%) |
| refs per MR (mode / max) | 1 (155 MRs) / 12 | 1 (103 MRs) / 10 |
| `initProvider` mirror of the triad | 523 refs / 523 selectors | 452 / 451 |

`initProvider` carries a near-complete mirror of the triad (523 of 578 on GCP). A generator that
writes only `forProvider` is correct; one that writes both must keep them consistent.

### PATTERN `ReferenceTriad` — the load-bearing one

Real excerpt, `connections.servicenetworking.gcp.m.upbound.io`, `spec.forProvider`
(`xpkg.upbound.io/upbound/provider-gcp-servicenetworking:v3.0.1`;
source https://github.com/crossplane-contrib/provider-upjet-gcp/tree/v3.0.0/apis/namespaced/servicenetworking):

```json
"network":         { "type": "string",
   "description": "Name of VPC network connected with service producers using VPC peering." },
"networkRef":      { "type": "object",
   "description": "Reference to a Network in compute to populate network." },
"networkSelector": { "type": "object",
   "description": "Selector for a Network in compute to populate network." }
```

- **Count:** 541 singular triads on GCP, 409 on AWS.
- **GUI:** fully structural. This *is* the edge in a node graph: draw an arrow, the generator
  emits `networkRef.name` (or `networkSelector.matchControllerRef: true`).

### PATTERN `ListReferenceTriad`

`serviceperimeters.accesscontextmanager.gcp.m.upbound.io`, `spec.forProvider.spec` — verbatim:

```json
"accessLevels": {
  "description": "A list of AccessLevel resource names that allow resources within\nthe ServicePerimeter to be accessed from the internet. ...",
  "items": { "type": "string" }, "type": "array", "x-kubernetes-list-type": "set" },
"accessLevelsRefs": {
  "description": "References to AccessLevel in accesscontextmanager to populate accessLevels.",
  "items": { "description": "A NamespacedReference to a named object.",
    "properties": {
      "name":      { "description": "Name of the referenced object.", "type": "string" },
      "namespace": { "description": "Namespace of the referenced object", "type": "string" },
      "policy":    { "properties": {
          "resolution": { "default": "Required", "enum": ["Required","Optional"], "type": "string" },
          "resolve":    { "enum": ["Always","IfNotPresent"], "type": "string" } },
        "type": "object" } },
    "required": ["name"], "type": "object" },
  "type": "array" },
"accessLevelsSelector": {
  "description": "Selector for a list of AccessLevel in accesscontextmanager to populate accessLevels.",
  "properties": { "matchControllerRef": {...}, "matchLabels": {...}, "namespace": {...}, "policy": {...} },
  "type": "object" }
```

- **Count:** 37 on GCP (all resolve via **exact stem**), 55 on AWS (34 via `stem+"s"`, 21 exact).
- **GUI:** structural, but the edge is 1→N. `accessLevelsRefs` is an *array* of refs: the node
  graph needs a fan-out port, and the DSL needs a list-append form. `matchControllerRef: true`
  on the selector is the common shorthand for "every sibling of this kind".

### PATTERN `RefFieldNameOverride` — the *only* thing that breaks stem resolution

The one GCP failure:

```
UNRES: policies.dns.gcp.m.upbound.io  spec.forProvider.networks[].networkRef
       siblings = ['networkRef', 'networkSelector', 'networkUrl']
       description = "Reference to a Network in compute to populate networkUrl."
```

Its cause is one hand-written override, verbatim from
https://github.com/crossplane-contrib/provider-upjet-gcp/blob/main/config/namespaced/dns/config.go :

```go
p.AddResourceConfigurator("google_dns_policy", func(r *config.Resource) {
    r.References["networks.network_url"] = config.Reference{
        TerraformName:     "google_compute_network",
        RefFieldName:      "NetworkRef",
        SelectorFieldName: "NetworkSelector",
        Extractor:         common.ExtractResourceIDFuncPath,
    }
})
```

**GCP ships 2 `RefFieldName` lines in total (1 resource, duplicated cluster+namespaced).
AWS ships 95.** AWS's are systematic — a blanket singularisation rule, verbatim from
https://github.com/crossplane-contrib/provider-upjet-aws/blob/main/config/overrides.go :

```go
case strings.HasSuffix(k, "security_group_ids"):
    r.References[k] = config.Reference{
        TerraformName:     "aws_security_group",
        RefFieldName:      name.NewFromSnake(strings.TrimSuffix(k, "s")).Camel + "Refs",
        SelectorFieldName: name.NewFromSnake(strings.TrimSuffix(k, "s")).Camel + "Selector",
    }
...
case "subnet_ids":
    r.References["subnet_ids"] = config.Reference{
        TerraformName:     "aws_subnet",
        RefFieldName:      "SubnetIDRefs",
        SelectorFieldName: "SubnetIDSelector",
    }
```

That is the entire origin of the `stem+"s"` rule. Examples it produces (AWS only):

```
instances.ec2.aws.m.upbound.io   spec.forProvider.vpcSecurityGroupIdRefs -> vpcSecurityGroupIds
networkacls.ec2.aws.m.upbound.io spec.forProvider.subnetIdRefs           -> subnetIds
vpclinks.apigateway.aws.m...     spec.forProvider.targetArnRefs          -> targetArns
services.ecs.aws.m.upbound.io    spec.forProvider.networkConfiguration.securityGroupRefs -> securityGroups
```

The **defensible algorithm** (proved by the upjet source, §3) is: *ignore the name entirely for
value-field resolution — read the target field out of the description's `to populate <field>`
capture.* That is right 578/578 on GCP and 463/464 on AWS (the one AWS miss,
`eks.AccessEntry.principalArnFromRoleRef` → `principalArn`, is also right — it's the *stem* rule
that fails there, not the description).

- **Count:** 1 GCP resource, ~34 AWS ref sites.
- **GUI:** structural *if* the generator reads `to populate <field>` instead of guessing from the
  name. No raw escape needed. If it guesses from the name, this needs a per-provider override map.

### PATTERN `NestedReferenceUnderArray`

103/578 GCP refs (17.8%) sit under an array — nearly 3× AWS's 6.2%. Deepest observed:

```
deidentifytemplates.datalossprevention.gcp.m.upbound.io
  spec.forProvider.deidentifyConfig.recordTransformations.fieldTransformations[]
    .infoTypeTransformations.transformations[].primitiveTransformation
    .cryptoDeterministicConfig.cryptoKey.unwrapped.keySecretRef
```

- **GUI:** **partly structural, partly raw.** A ref at `spec.forProvider.settings.ipConfiguration.privateNetworkRef`
  (plain nested object) is a normal edge with a dotted path. A ref at
  `spec.forProvider.networks[].networkRef` requires the GUI to bind the edge to a *specific array
  element*, which a node graph cannot express without an index or a loop. **This is the strongest
  argument for a per-field `rawTemplate` escape in the reference layer**, and it is ~3× more
  common on GCP than on AWS.

### PATTERN `CrossGroupReference`

197/578 GCP refs (34.1%) target a different API group than the resource that declares them —
notably `servicenetworking` → `compute`, `sql` → `compute`, `cloudrun` → `secretmanager`.
Target-group histogram from the descriptions:

```
compute:222 cloudplatform:55 bigquery:49 storage:28 kms:19 secretmanager:17 pubsub:17
dialogflowcx:17 apigee:14 sql:14 privateca:10 alloydb:6 certificatemanager:6 tags:6
dataplex:6 accesscontextmanager:5 networksecurity:5 container:5 datacatalog:5 ...
(54 distinct target groups, 128 distinct target Kinds)
```

- **GUI:** structural, but the node palette must be **cross-package**. `compute` is the hub —
  a GCP graph editor that loads only the packages named in the blueprint will fail to resolve
  a third of its edges. Load the family, or resolve lazily on demand.

### PATTERN `SecretKeySelectorField` (sensitive parameter — NOT a cross-resource ref)

```
GCP namespaced : 115 real, shape OBJ[key,name],           required ["key","name"]
GCP cluster    : 115 real, shape OBJ[key,name,namespace]
AWS namespaced :  23 real, shape OBJ[key,name]
```

Real excerpt (`users.sql.gcp.m.upbound.io`):

```json
"passwordSecretRef": {
  "description": "The password for the user. Can be updated. For Postgres\ninstances this is a Required field, ...",
  "properties": { "key": {...}, "name": {...} },
  "required": ["key","name"], "type": "object" }
```

Note the description is **the business field's own prose**, not a grammar sentence — 10 of 115
carry the generic `"A LocalSecretKeySelector is a reference to a secret key\nin the same namespace
with the referencing object."` and the other 105 do not. So secret refs are **not** parseable from
the description; they are identified by shape + `required: ["key","name"]`.

- **Count:** 115 GCP, 23 AWS. 5× denser on GCP.
- **GUI:** structural but a *different* node type — it points at a `v1/Secret` in the XR's
  namespace, not at another composed resource. Model it as a distinct port colour. In v2
  namespaced it is `{key, name}` with **no namespace** — do not emit one.

### PATTERN `NameSuffixFalsePositive` — why name-only detection is unsafe

22 GCP properties (11 unique, ×2 for `initProvider`) are named `*Ref`/`*Refs`/`*Selector` and
are *not* Crossplane references:

```
services.cloudrun.gcp.m...  .containers[].env[].valueFrom.secretKeyRef  ['key','name','nameRef','nameSelector']
services.cloudrun.gcp.m...  .containers[].envFrom[].configMapRef        ['localObjectReference','optional']
services.cloudrun.gcp.m...  .containers[].envFrom[].secretRef           ['localObjectReference','optional']
services.cloudrun.gcp.m...  .template.spec.nodeSelector                 []
v2jobs.cloudrun.gcp.m...    .env[].valueSource.secretKeyRef             ['secret','secretRef','secretSelector','version']
v2jobs.cloudrun.gcp.m...    .template.template.nodeSelector             ['accelerator']
v2services.cloudrun.gcp.m.. .template.nodeSelector                      ['accelerator']
vpntunnels.compute.gcp.m... .localTrafficSelector                       ARRAY (of string)
vpntunnels.compute.gcp.m... .remoteTrafficSelector                      ARRAY (of string)
workflowtemplates.dataproc  .placement.clusterSelector                  ['clusterLabels','zone']
```

AWS has 4 (`users.elasticache … passwordsSecretRef`, an *array* of `{key,name}`).

And the converse — a *shape* match that is not a reference (AWS, cluster-scoped ref shape
`{name, policy}`), verbatim from `roles.iam.aws.m.upbound.io`:

```json
"inlinePolicy": {
  "description": "Configuration block defining an exclusive set of IAM inline policies ...",
  "items": { "properties": {
      "name":   { "description": "Friendly name of the role. ...", "type": "string" },
      "policy": { "description": "Policy document as a JSON formatted string.", "type": "string" } },
    "type": "object" },
  "type": "array" }
```

Same for `routers.compute.gcp.upbound.io spec.forProvider.md5AuthenticationKeys`, which has the
secret-selector shape `{key, name}` but no `required` block.

- **GUI:** these must be rendered as **ordinary fields**, not ports. Requiring name-suffix **AND**
  shape **AND** (for cross-resource refs) a parsing description eliminates all 26 across both
  providers with zero false negatives.

---

## 3. Does the description grammar hold? Parse rates, failures verbatim, corrected regex.

### The AWS-derived regexes, applied to GCP

```
^(Reference|References) to (?:a |an )?(?P<Kind>\w+) in (?P<group>\w+) to populate (?P<field>\w+)\.$
^Selector for (?:a list of |a |an )?(?P<Kind>\w+) in (?P<group>\w+) to populate (?P<field>\w+)\.$
```

```
GCP  REF  : 578 / 578 = 100.00%   FAIL = 0
GCP  SEL  : 578 / 578 = 100.00%   FAIL = 0
AWS  REF  : 463 / 464 =  99.78%   FAIL = 1
AWS  SEL  : 463 / 464 =  99.78%   FAIL = 1
```

**Zero GCP failures.** (An earlier name-based pass reported a 45% parse rate; that was an
artefact of scanning `matchControllerRef` and `*SecretRef` properties, which are not references.
The structural pass above is the correct denominator.)

Additionally, the GCP namespaced and cluster-scoped descriptions are **byte-identical**:
`1156 / 1156` ref+selector descriptions match across the pair. And GCP ref/selector descriptions
agree with the field name stem in **577/578** cases (the one mismatch is the `dns` override above).

### Every description that FAILS to parse, verbatim

GCP: **none.**

AWS (2, both on the same resource):

```
clusterauths.eks.aws.m.upbound.io  spec.forProvider.clusterNameRef
  'Reference to a Cluster to populate clusterName.\nEither ClusterName, ClusterNameRef or ClusterNameSelector has to be given.'

clusterauths.eks.aws.m.upbound.io  spec.forProvider.clusterNameSelector
  'Selector for a Cluster to populate clusterName.\nEither ClusterName, ClusterNameRef or ClusterNameSelector has to be given.'
```

Two distinct defects: (a) **no `" in <group>"` segment**, (b) **trailing free text after the
period**.

### Why — the grammar is one `fmt.Sprintf`, so it is provider-independent by construction

https://github.com/crossplane/upjet/blob/main/pkg/types/reference.go (lines ~84–115):

```go
_, isSlice := f.FieldType.(*types.Slice)
rfn := name.ReferenceFieldName(f.Name, isSlice, f.Reference.RefFieldName)
sfn := name.SelectorFieldName(f.Name, f.Reference.SelectorFieldName)
...
refComment := fmt.Sprintf("// Reference to a %s to populate %s.\n%s",
    friendlyTypeDescription(f.Reference.Type), f.Name.LowerCamelComputed, commentOptional.Build())
selComment := fmt.Sprintf("// Selector for a %s to populate %s.\n%s",
    friendlyTypeDescription(f.Reference.Type), f.Name.LowerCamelComputed, commentOptional.Build())
if isSlice {
    refComment = fmt.Sprintf("// References to %s to populate %s.\n%s", ...)
    selComment = fmt.Sprintf("// Selector for a list of %s to populate %s.\n%s", ...)
}
```

```go
func friendlyTypeDescription(path string) string {
    if !strings.Contains(path, ".") {
        return path                              // <-- the AWS eks failure: no " in <group>"
    }
    typeName := path[strings.LastIndex(path, ".")+1:]
    dirs := strings.Split(path, "/")
    groupName := dirs[len(dirs)-2]
    return fmt.Sprintf("%s in %s", typeName, groupName)
}
```

and the naming rule, https://github.com/crossplane/upjet/blob/main/pkg/types/name/reference.go :

```go
func ReferenceFieldName(n Name, plural bool, camelOverride string) Name {
    if camelOverride != "" { return NewFromCamel(camelOverride) }   // <-- the only escape
    temp := n.Snake + "_ref"
    if plural { temp += "s" }
    return NewFromSnake(temp)
}
func SelectorFieldName(n Name, camelOverride string) Name {
    if camelOverride != "" { return NewFromCamel(camelOverride) }
    return NewFromSnake(n.Snake + "_selector")
}
```

Consequences the DSL can rely on as *invariants*, not heuristics:
- The article is always `"a "`, never `"an "` — confirmed empirically: **0** occurrences of
  `"to an "` / `"for an "` across 1156 GCP descriptions. The `an ` alternative is harmless dead
  code.
- `"Selector for a list of "` appears exactly when the ref is a slice: **37/37** on GCP.
- The default ref name is `snake(valueField) + "_ref"` (+`"s"` if slice), re-camelised — so
  `stem == valueField` unless `RefFieldName` is overridden.

### CORRECTED REGEXES (validated: 100% on GCP *and* AWS)

```
^(?P<plural>References?) to (?:a )?(?P<Kind>\w+)(?: in (?P<group>\w+))? to populate (?P<field>\w+)\.(?:\n[\s\S]*)?$
^Selector for (?:(?P<list>a list of )|a )?(?P<Kind>\w+)(?: in (?P<group>\w+))? to populate (?P<field>\w+)\.(?:\n[\s\S]*)?$
```

```
GCP REF: 578/578 = 100.00%      GCP SEL: 578/578 = 100.00%
AWS REF: 464/464 = 100.00%      AWS SEL: 464/464 = 100.00%
                    -> 2084 / 2084 descriptions across both providers
```

Resolution rules to pair with them:
- **value field** := the `field` capture (never the name stem).
- **plural** := `References` (or `Selector for a list of`) — matches `isSlice` exactly.
- **target group** := the `group` capture; when absent, **default to the declaring resource's own
  short group** (that is precisely what `friendlyTypeDescription`'s early return means).
- **target API group** := `<group>.gcp.m.upbound.io` for a v2 namespaced blueprint,
  `<group>.gcp.upbound.io` for cluster scope. The description never carries the scope — the
  generator supplies it. This resolved **578/578** GCP refs to a real CRD in the corpus.

---

## 4. Is the v2 namespaced spec envelope identical to AWS's?

**Yes — structurally identical, field for field, default for default.** Diffed with descriptions
stripped, across all 405 GCP and 279 AWS namespaced MRs.

```
GCP .m.      spec props: ['forProvider','initProvider','managementPolicies',
                          'providerConfigRef','writeConnectionSecretToRef']   required: ['forProvider']
AWS .m.      spec props: ['forProvider','initProvider','managementPolicies',
                          'providerConfigRef','writeConnectionSecretToRef']   required: ['forProvider']
GCP cluster  spec props: [... plus 'deletionPolicy' ...]                      required: ['forProvider']
AWS cluster  spec props: [... plus 'deletionPolicy' ...]                      required: ['forProvider']
```

Per-field diff (GCP.m vs AWS.m, `identical? True` on every one):

```
spec.providerConfigRef  {"default":{"kind":"ClusterProviderConfig","name":"default"},
                         "properties":{"kind":{"type":"string"},"name":{"type":"string"}},
                         "required":["kind","name"],"type":"object"}
spec.writeConnectionSecretToRef
                        {"properties":{"name":{"type":"string"}},
                         "required":["name"],"type":"object"}
spec.managementPolicies {"default":["*"],
                         "items":{"enum":["Observe","Create","Update","Delete","LateInitialize","*"],
                                  "type":"string"},"type":"array"}
spec.deletionPolicy            ABSENT in both
spec.publishConnectionDetailsTo ABSENT in both
```

Confirms all four claims:
- **no `deletionPolicy`** in `.m.` (present, `{"default":"Delete","enum":["Orphan","Delete"]}`, in
  cluster scope only)
- **`providerConfigRef` requires `kind`**, defaults to `{kind: ClusterProviderConfig, name: default}`.
  Cluster scope instead is `{"default":{"name":"default"}, "required":["name"]}` **with a `policy`
  sub-object** — the `policy` field is gone in `.m.`
- **`writeConnectionSecretToRef` is name-only** (cluster: `required:["name","namespace"]`)
- `publishConnectionDetailsTo` is gone from both.

**Status envelope: 1 distinct shape across the entire GCP corpus (409 CRDs) and 1 across AWS
(279) — and they are byte-identical to each other:**

```json
{"properties":{
  "conditions":{"items":{"properties":{
      "lastTransitionTime":{"format":"date-time","type":"string"},
      "message":{"type":"string"},"observedGeneration":{"format":"int64","type":"integer"},
      "reason":{"type":"string"},"status":{"type":"string"},"type":{"type":"string"}},
    "required":["lastTransitionTime","reason","status","type"],"type":"object"},
    "type":"array","x-kubernetes-list-map-keys":["type"],"x-kubernetes-list-type":"map"},
  "lastHandledReconcileAt":{"type":"string"},
  "observedGeneration":{"format":"int64","type":"integer"}},
 "type":"object"}
```

### PATTERN `NamespacedSpecEnvelope`
- **Count:** 405/405 GCP, 279/279 AWS — the base envelope is invariant; only spec-level CEL
  differs (§5), producing 172 GCP / 118 AWS *textual* variants of the same object.
- **GUI:** structural and **provider-independent — hard-code it.** Emit
  `providerConfigRef: {kind, name}`, `writeConnectionSecretToRef: {name}`, `managementPolicies`,
  and never `deletionPolicy` for a v2 namespaced target.

**Real-world confirmation** — a production v2-namespaced GCP composition,
https://github.com/upbound/configuration-gcp-database/blob/main/functions/sqlinstance/main.k :

```kcl
_defaults = {
    managementPolicies = params.managementPolicies or ["*"]
    if providerConfigRefName:
        providerConfigRef = {
            kind = "ProviderConfig"          # kind is mandatory in v2
            name = providerConfigRefName
        }
}
...
computev1beta1.GlobalAddress{ spec = _defaults | { forProvider = {
    address = "10.205.0.0"  addressType = "INTERNAL"  prefixLength = 16  purpose = "VPC_PEERING"
    networkSelector = { matchLabels = {
        "networks.gcp.platform.upbound.io/network-id" = params.networkRef.id } } } } }
servicenetworkingv1beta1.Connection{ spec = _defaults | { forProvider = {
    reservedPeeringRangesSelector = { matchControllerRef = True }      # LIST selector
    service = "servicenetworking.googleapis.com"
    networkSelector = { matchLabels = { ... } } } } }
sqlv1beta1.DatabaseInstance{ spec = _defaults | { forProvider = {
    settings = { ipConfiguration = {
        privateNetworkRef = { name = params.networkRef.id            # NESTED ref
                              namespace = oxr.metadata.namespace } } } } } }
sqlv1beta1.User{ spec = _defaults | {
    writeConnectionSecretToRef = { name = _connection_secret_name("user") }   # name-only
    forProvider = {
        instanceSelector = { matchControllerRef = True }
        passwordSecretRef = { name = ..., key = ... } } } }                   # {key,name}, no namespace
```

Every DSL construct the survey predicts appears here: `matchLabels` selectors for cross-XR
wiring, `matchControllerRef: true` for sibling wiring (singular *and* list), a ref nested two
levels inside `forProvider`, a name-only `writeConnectionSecretToRef`, a `{name,key}` secret ref,
and a `kind`-bearing `providerConfigRef`.

---

## 5. GCP-specific schema features absent from AWS

Measured over `spec` (incl. `forProvider`/`initProvider`) of all namespaced MRs.

| feature | **GCP** (405 MRs) | AWS (279 MRs) | note |
|---|---|---|---|
| `oneOf` | **0** | 0 | — |
| `anyOf` | **0** | 0 | — |
| `allOf` | **0** | 0 | — |
| `not` | **0** | 0 | — |
| **real enums** (excluding Crossplane's `policy.resolution`/`resolve`/`managementPolicies`) | **0** | **0** | terraform-derived string fields are never enum-constrained on either provider |
| `format` keywords in `spec` | **0** | 0 | (`date-time` appears only in `status.conditions`) |
| `default` outside ref plumbing | **405** — all of them `spec.managementPolicies: ["*"]` | 280 (279 × `["*"]`, plus one `"10m0s"` on an AWS timeout) | GCP has **no** business-field defaults at all |
| `x-kubernetes-validations` (CEL) | **454** rules on MRs, 194 distinct, in **249/405 MRs (61%)** | 206 rules, 136 distinct, in 150/279 (54%) | |
| `x-kubernetes-map-type` | **644** | 384 | `atomic` on `tags`/`labels` maps |
| `x-kubernetes-list-type` | **260** | 374 | `set` on string arrays |
| `x-kubernetes-list-map-keys` | status only | status only | |
| `required` blocks inside `forProvider` | 818 total, **125 not inside a ref** | 736 total, **247 not inside a ref** | |
| `region` present / **OpenAPI-required** | 66 / **38** | 246 / **246** | |
| `project` present / required | **319** / 0 | 0 / 0 | |
| `location` present / required | 101 / 57 | 2 / 0 | |
| `zone` present / required | 23 / 11 | 0 / 0 | |
| `labels` / `tags` present | 107 / 18 | 1 / 135 | |

### PATTERN `RequiredParamCEL`
Every CEL rule on an MR is one of exactly **two** mechanical templates:

```
!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies)
  || has(self.forProvider.<F>)
  || (has(self.initProvider) && has(self.initProvider.<F>))
        message: "spec.forProvider.<F> is a required parameter"
```
```
!('*' in self.managementPolicies || 'Create' in self.managementPolicies || 'Update' in self.managementPolicies)
  || has(self.forProvider.<F>)
        message: "spec.forProvider.<F> is a required parameter"     # for *SecretRef fields, which have no initProvider twin
```

- **Count:** GCP 454 rules, of which **443** template-1 and **11** template-2 (all `*SecretRef`:
  `compute.BackendBucketSignedURLKey.keyValueSecretRef`, `compute.SSLCertificate.certificateSecretRef`,
  `identityplatform.*.clientIdSecretRef`, `oslogin.SSHPublicKey.keySecretRef`, …).
  AWS 206 rules, 204 / 2 respectively. **Zero** non-template CEL on any MR in either provider.
  (The only hand-written CEL in the corpus is on `ClusterProviderConfig.spec.reconciliationPolicy`
  — 4 rules, identical text on both providers.)
- **GUI:** structural. Parse the `message` (`spec.forProvider.<F> is a required parameter`) to
  drive "this port must be filled" validation. No raw escape.

### PATTERN `RegionRequiredAsymmetry` — the biggest concrete portability trap
AWS: `spec.forProvider.required: ["region"]` on **246 / 279** MRs (88%).
GCP: **0** MRs require `region`; instead 57 require `location`, 38 require `region`, 11 require
`zone`, and `project` — present on 319/405 — is *never* required (it falls back to
`ClusterProviderConfig.spec.projectID`).

```
GCP examples:  backups.alloydb -> required ['location']
               appconnectors.beyondcorp -> required ['region']
               datasetiammembers.bigquery -> required ['member','role']
AWS examples:  accounts.apigateway -> required ['region']   (× ~246)
```

- **GUI:** structural, but only if the "where does this thing live" field is **derived from the
  CRD's own `required` list**, not from a hard-coded `region` template. Hard-coding AWS's
  `region` produces schema-invalid GCP compositions and omits `location`/`zone` where they are
  mandatory.

### PATTERN `ExtractorInvisibility` — a semantic gap no schema exposes
The description says *which field* gets populated, never *what value* lands there. That is
decided by an `Extractor` in the provider's Go config, invisible in the CRD:

```
GCP  (config/namespaced, 178 config.Reference blocks, 80 with an Extractor):
   37  common.ExtractResourceIDFuncPath
   24  common.PathSelfLinkExtractor          <- populates a selfLink URL, not a name
    6  resource.ExtractResourceID()
    4  common.ExtractProjectIDFuncPath
    2  resource.ExtractParamPath("name",true)
    2  PathInstanceGroupExtractor
    1  common.ExtractFolderIDFuncPath
    1  resource.ExtractParamPath("email",true)
AWS  (config/namespaced, 427 blocks, 162 with an Extractor):
   99+14  common.PathARNExtractor             <- populates an ARN
   19+2   common.PathTerraformIDExtractor
    9     resource.ExtractResourceID()
    ...   ExtractParamPath("arn"|"domain"|"repository"|...)
```

- **Count:** 80 of 178 `config.Reference{}` blocks in GCP's `config/namespaced/`, and 162 of 427 in AWS's, declare a non-default `Extractor` (blocks, not CRD ref sites — the rest are generated by `KnownReferencers` loops and `provider-metadata.yaml`).
- **GUI:** structural for *drawing* the edge (the generator only writes `xRef.name`, and the
  provider does the extraction at reconcile time). **But it is invisible for previewing or
  validating** — a graph editor cannot show the user what value will end up in `networkUrl`. Do
  not attempt to compute it; label the edge with the target Kind and leave the value to the
  provider.

### PATTERN `ProviderConfigSurface` — genuinely provider-specific
```
clusterproviderconfigs.gcp.m.upbound.io  props: [billingProject, credentials, projectID,
                                                 reconciliationPolicy, userProjectOverride]
                                         required: ['credentials','projectID']
   credentials.source enum: [None, Secret, AccessToken, ImpersonateServiceAccount,
                             InjectedIdentity, Environment, Filesystem, Upbound]

clusterproviderconfigs.aws.m.upbound.io  props: [assumeRoleChain, credentials, endpoint,
                                                 reconciliationPolicy, s3_use_path_style,
                                                 skip_credentials_validation, skip_metadata_api_check,
                                                 skip_region_validation, skip_requesting_account_id]
                                         required: ['credentials']
   credentials.source enum: [None, Secret, IRSA, WebIdentity, PodIdentity, Upbound]
```
`reconciliationPolicy` is the only shared non-`credentials` field, and it is byte-identical.

- **GUI:** the ProviderConfig *reference* is structural and identical everywhere
  (`{kind, name}`); the ProviderConfig *object* must be schema-driven per provider. Don't model it
  in the DSL beyond a `providerConfigRef` node.

---

## 6. VERDICT

**The AWS-derived inference layer is highly portable — it survives GCP essentially unchanged —
but the version currently in hand contains two AWS-shaped assumptions that must be replaced with
data, and one method error that must be replaced with a better method.**

### What is genuinely universal (hard-code it; it is generated by upjet, not by the provider)

| invariant | GCP evidence | AWS evidence |
|---|---|---|
| Every kind ships cluster + `.m.` | 405/405 pairs | same |
| Ref ⇄ Selector are always siblings | 578/578 both ways | 464/464 both ways |
| `NamespacedReference` shape `{name, namespace, policy{resolution,resolve}}`, `required:[name]` | 541 obj + 37 array | 409 + 55 |
| `NamespacedSelector` shape `{matchControllerRef, matchLabels, namespace, policy}` | 578 | 464 |
| Description grammar (corrected regexes) | 1156/1156 | 928/928 |
| `"a list of"` ⇔ slice | 37/37 | 55/55 |
| Namespaced spec envelope (no `deletionPolicy`; `providerConfigRef` requires `kind`; `writeConnectionSecretToRef` name-only; `managementPolicies` default `["*"]`) | 405/405 | 279/279 |
| Status envelope | 1 distinct shape | identical to GCP |
| CEL = required-parameter templates only | 454/454 on MRs | 206/206 |
| No `oneOf`/`anyOf`/`allOf`/`not`/real enums/formats anywhere in `spec` | 0 each | 0 each |
| Cluster and namespaced descriptions identical | 1156/1156 | (assumed, same generator) |

### What must become data-driven per provider (not hard-coded)

1. **Value-field resolution.** Replace `{stem, stem+"s"}` with **the `to populate <field>` capture
   from the description**. `stem+"s"` is 34 AWS override sites, 0 on GCP. Keep a *fallback* stem
   rule only for the (currently empty) case of a ref whose description does not parse.
2. **The location/scoping field.** `region` is required on 246/279 AWS MRs and on 0/405 GCP MRs.
   Read `spec.forProvider.required[]` and the required-param CEL messages per CRD; never assume a
   field name. GCP needs `location` (57), `region` (38), `zone` (11), and treats `project` (319
   MRs) as ProviderConfig-inherited.
3. **The ProviderConfig object schema and its `credentials.source` enum** — fully disjoint
   between providers. Only `providerConfigRef: {kind, name}` is shared.
4. **The target-group → API-group mapping.** The description's `group` is a *short* group
   (`compute`, `cloudplatform`); the full group is `<short>.<provider>.[m.]upbound.io`. The
   provider stem (`gcp` / `aws`) and the scope suffix must come from project config.
5. **Package-set discovery.** 34% of GCP refs cross API groups (`compute` is referenced by 222
   of 578). The tool must know the whole family, or resolve packages lazily — a per-package
   catalogue is not enough.

### What must be a raw escape hatch

- **Refs nested under an array** — 103/578 GCP (17.8%), 29/464 AWS (6.2%). A node-graph edge
  cannot address `spec.forProvider.networks[3].networkRef` without an index or a loop construct.
  Everything else in this survey is expressible structurally.

### Method correction (important)

Reference detection must be **shape-first**, then confirmed by name suffix and by a parsing
description. Name-only detection admits 22 GCP and 4 AWS false positives (`nodeSelector`,
`configMapRef`, `localTrafficSelector`, …); shape-only detection admits `iam.Role.inlinePolicy`
(AWS) and `compute.Router.md5AuthenticationKeys` (GCP). The 3-way conjunction is exact on both
corpora: **1042 true positives, 0 false positives, 0 false negatives.**

Secret-key selectors are a *separate* pattern (115 GCP / 23 AWS) identified by shape
`{key, name}` + `required: ["key","name"]`; they have business prose for descriptions and must
never be routed through the reference grammar.

### Residual risk

The grammar's stability rests on `upjet/pkg/types/reference.go` — one file, two `fmt.Sprintf`
calls, one `friendlyTypeDescription`. It is not a *spec*, but it is a *single point of
generation*, shared by every upjet provider (aws, gcp, azure, and the community set). A change
there breaks every provider at once, which makes it detectable by a single regression test over
one package. **Recommendation: pin a golden test that parses the ref/selector descriptions of one
CRD per provider family and asserts 100%.** That converts the residual risk from "an unspecified
convention we depend on" into "a build-time assertion".

---

### Appendix — artefacts

```
scratchpad/gcpall/all.ndjson     815 GCP CRDs   (80 subpackages @ v3.0.1 + family)
scratchpad/gcpall/index.tsv      group / kind / scope / versions
scratchpad/gcpall/refscan2.py    shape-first triad + grammar scanner
scratchpad/gcpall/pull.sh        subpackage puller (xpkgget)
scratchpad/awsall/all.ndjson     563 AWS CRDs   (16 subpackages @ v2.7.1 + family)
scratchpad/feat.py               enum / CEL / default / x-kubernetes feature scanner
```
