I have everything I need.

# Anatomy of a Managed-Resource CRD Schema

*Evidence base: 282 AWS m-variant CRDs (ec2/iam/rds/s3/eks @ v2.4.0, 39,170 schema nodes), 62 GCP+Azure storage CRDs (8,763 nodes), 9 provider-kubernetes CRDs (v0.18.0 + v1.0.0), plus the live `kind-platform` cluster (Crossplane v2.4.0). Everything marked **[V]** was executed; **[D]** was read in docs.*

## Decisions this enables

1. **Ship `crossplane xpkg get-crds` as the schema-acquisition layer, not a cluster client.** **[V]** It pulled all 204 EC2 CRDs from `xpkg.upbound.io/upbound/provider-aws-ec2:v2.4.0` in **3.5 s** with no cluster, no docker login, caching to `~/.crossplane/cache` (18 MB after 8 providers). This kills the hardest onboarding problem outright. On a live v2 cluster, `kubectl get managedresourcedefinitions` is a second full source that carries the complete `openAPIV3Schema` **even for MRs whose CRD does not exist** (`spec.state: Inactive`) **[V]**.
2. **Build the connection-suggestion engine on a description-string parser, and accept that it is the only option.** **[V]** There are **zero** vendor `x-*` extensions in any provider CRD — the only x-keys present anywhere are the six standard Kubernetes ones. The target kind lives *exclusively* in English prose: `"Reference to a Queue in sqs to populate queueUrl."` The grammar is stable across AWS, GCP and Azure. **34% (58/172) of EC2 ref targets are not guessable from the field name** (`cidrRef`→`VPC`, `typeRef`→`CustomerGateway`), so name-based heuristics are not a fallback.
3. **Do not build enum-driven dropdowns for managed resources.** **[V]** Across 47,933 schema nodes in 344 CRDs from three clouds, the count of value-level `enum`s is **0**. All 5,603 enum nodes are Crossplane boilerplate (`Required|Optional`, `Always|IfNotPresent`, the `managementPolicies` action list). Same for `pattern`, `minimum`, `maximum`, `minLength`, `minItems`, `oneOf`, `anyOf`, `$ref`, `nullable` — **all exactly zero**. A form builder gets type + description and nothing else. The win is mining prose for allowed values: **127 string fields** in the AWS sample document their values in the description with no `enum` (`Fleet.type` → "maintain, request, instant").
4. **Treat "required" as three separate mechanisms, and read the CEL.** **[V]** `spec.forProvider.required` is `['region']` for 268/296 AWS CRDs and **empty for every GCP and Azure CRD**. The genuinely required fields are encoded only as CEL rules on `.spec` with one machine-parsable message template: `spec.forProvider.<FIELD> is a required parameter` (188 rules across the AWS sample, 70 across GCP/Azure, **100% one template**). Regex the message, not the rule.
5. **Fork the envelope renderer on `.m.` in the API group.** **[V]** Confirmed identically on upjet-AWS and non-upjet provider-kubernetes: v2 namespaced MRs **drop `deletionPolicy` and `publishConnectionDetailsTo` entirely**, require `providerConfigRef.kind` (defaulting to `ClusterProviderConfig`), and strip `namespace` from every secret reference. Emitting a v1-shaped MR into a v2 composition produces fields the API server prunes.

---

## 1. The canonical MR spec shape

### Exact JSON paths, v2 namespaced (`queues.sqs.aws.m.upbound.io`, scope `Namespaced`) **[V]**

```
.spec                                    required: [forProvider]
.spec.forProvider                        required: [region]     (AWS only)
.spec.initProvider
.spec.managementPolicies                 default: ["*"]
.spec.providerConfigRef                  required: [kind, name]
.spec.providerConfigRef.kind
.spec.providerConfigRef.name
.spec.writeConnectionSecretToRef         required: [name]
.spec.writeConnectionSecretToRef.name
.status.atProvider
.status.conditions
.status.observedGeneration
```

`spec` property set is **exactly** `['forProvider','initProvider','managementPolicies','providerConfigRef','writeConnectionSecretToRef']` in **102/102** EC2 m-CRDs and **all** GCP/Azure m-CRDs **[V]**. There is no `deletionPolicy` and no `publishConnectionDetailsTo` anywhere in the v2 surface.

### v1 / cluster-scoped legacy (`queues.sqs.aws.upbound.io`, scope `Cluster`) **[V]**

```
.spec.deletionPolicy                     enum: [Orphan, Delete]  default: Delete
.spec.forProvider
.spec.initProvider
.spec.managementPolicies
.spec.providerConfigRef                  required: [name]        (kind absent)
.spec.providerConfigRef.policy.resolution   enum: [Required, Optional]
.spec.providerConfigRef.policy.resolve      enum: [Always, IfNotPresent]
.spec.writeConnectionSecretToRef         required: [name, namespace]
```

116/116 legacy EC2 schemas carry `deletionPolicy`; 0/102 m-variants do **[V]**.

### The v1 → v2 envelope diff (the table to hard-code)

| Path | legacy (`*.upbound.io`) | v2 (`*.m.upbound.io`) |
|---|---|---|
| `spec.deletionPolicy` | present, `enum:[Orphan,Delete]`, default `Delete` | **absent** |
| `spec.publishConnectionDetailsTo` | absent in AWS family; present in provider-kubernetes | **absent** |
| `spec.providerConfigRef` | `{name, policy}`, required `[name]`, default `{name: default}` | `{kind, name}`, required `[kind,name]`, default `{kind: ClusterProviderConfig, name: default}` |
| `spec.writeConnectionSecretToRef` | `{name, namespace}`, both required | `{name}` only |
| `<f>SecretRef` | `{key, name, namespace}` | `{key, name}` |
| `<f>Ref` | `{name, policy}` | `{name, namespace, policy}` |
| ref item description | `"A Reference to a named object."` | `"A NamespacedReference to a named object."` |
| CRD scope | `Cluster` | `Namespaced` |

### `providerConfigRef.kind` — what it accepts

The schema is bare `type: string` with **no enum and no CEL** **[V]** — a form builder must supply the allowed values itself. Resolvable empirically from installed CRDs **[V]**:

```
providerconfigs.aws.m.upbound.io          Namespaced  kind=ProviderConfig
clusterproviderconfigs.aws.m.upbound.io   Cluster     kind=ClusterProviderConfig
providerconfigs.aws.upbound.io            Cluster     kind=ProviderConfig     (legacy group)
```

So: **enumerate CRDs in the MR's provider group whose kind ends in `ProviderConfig`** rather than hard-coding two strings. Docs confirm the two values and that omitting the field defaults to a `ClusterProviderConfig` named `default` **[D]**. Note the trap: in the *legacy* group `ProviderConfig` is Cluster-scoped; in the `.m.` group `ProviderConfig` is Namespaced and `ClusterProviderConfig` is the cluster one. The same kind string means different scopes in different groups.

### Universal vs. provider-specific

`forProvider` is **not** universal. `ObservedObjectCollection` (provider-kubernetes) has **no `forProvider`, no `writeConnectionSecretToRef`, no `managementPolicies`** — its spec is `['objectTemplate','observeObjects','providerConfigRef']` **[V]**. Only `spec.providerConfigRef` survived every CRD I inspected.

Provider-specific envelope extensions are real and must be passed through, not dropped — provider-kubernetes `Object` adds `spec.references[]`, `spec.readiness` (with its own CEL rule and a 4-value policy enum), `spec.watch`, `spec.connectionDetails[]` **[V]**. And v1alpha1 uses singular `managementPolicy` (string enum) while v1alpha2 uses plural `managementPolicies` (array) — on the *same CRD* **[V]**.

**Generator rule:** never hard-code the envelope. Compute `envelope = spec.properties − {forProvider, initProvider}` and render whatever is left from its own schema.

---

## 2. Cross-resource references

### Real triad from the live cluster **[V]** (`queuepolicies.sqs.aws.m.upbound.io`)

`spec.forProvider` properties: `['policy','queueUrl','queueUrlRef','queueUrlSelector','region']`

```jsonc
"queueUrl":       { "type": "string",
                    "description": "URL of the SQS Queue to which to attach the policy." },

"queueUrlRef":    { "type": "object", "required": ["name"],
                    "description": "Reference to a Queue in sqs to populate queueUrl.",
                    "properties": {
                      "name":      { "type": "string" },
                      "namespace": { "type": "string" },
                      "policy":    { "properties": {
                          "resolution": { "enum": ["Required","Optional"], "default": "Required" },
                          "resolve":    { "enum": ["Always","IfNotPresent"] } } } } },

"queueUrlSelector": { "type": "object",
                    "description": "Selector for a Queue in sqs to populate queueUrl.",
                    "properties": {
                      "matchControllerRef": { "type": "boolean" },
                      "matchLabels":        { "type": "object",
                                              "additionalProperties": {"type":"string"} },
                      "namespace":          { "type": "string" },
                      "policy":             { /* same as above */ } } }
```

### Programmatic detection — what actually works

**There is no machine-readable link.** Verified by exhaustively collecting every key starting with `x-` or `$` across 344 CRDs from four providers **[V]**:

```
x-kubernetes-list-type 1314 · x-kubernetes-map-type 905 · x-kubernetes-list-map-keys 373
x-kubernetes-validations 184 · x-kubernetes-embedded-resource 6 · x-kubernetes-preserve-unknown-fields 6
```

Nothing else. CRD `metadata.annotations` carries only `controller-gen.kubebuilder.io/version` and `kustomize.config.k8s.io/id` **[V]**. The description string is the sole carrier.

**The grammar** (identical in AWS, GCP, Azure — 100% parse rate, 172/172 refs and 172/172 selectors in EC2, 0 unparsed) **[V]**:

```
^(Reference|References) to (?:a |an )?(?P<Kind>\w+) in (?P<group>\w+) to populate (?P<field>\w+)\.$
^Selector for (?:a list of |a |an )?(?P<Kind>\w+) in (?P<group>\w+) to populate (?P<field>\w+)\.$
```

`group` is the **short** group segment (`sqs`, `kms`, `elbv2`, `cloudwatchlogs`, `network`, `cloudplatform`); the full group is `<short>.<family-domain>` — e.g. `kms` → `kms.aws.m.upbound.io`. Resolve it against your CRD index rather than string-concatenating, because the family domain differs per provider and per v1/v2 group.

**Structural detection (the reliable half):**
- `name.endsWith("Ref")` and `type == "object"` and `properties ⊇ {name}` → single ref
- `name.endsWith("Refs")` and `type == "array"` → list ref. **Read the description off the array node, not `items`** — the item's description degrades to the useless `"A NamespacedReference to a named object."` **[V]**
- `name.endsWith("Selector")` and `properties ⊇ {matchLabels, matchControllerRef}` → selector
- Exclude `matchControllerRef` explicitly; it ends in `Ref` and is a boolean (172 false positives in EC2 alone).
- Exclude `name.endsWith("SecretRef")` — different category entirely (§ taxonomy #15).

**Which value field the triad populates:** the strict rule `Ref → stem`, `Refs → stem + "s"` is correct for **167/172**; the 5 failures are fields already ending in a plural (`cidrBlocksRefs` → `cidrBlocks`, `gatewayLoadBalancerArnsRefs` → `gatewayLoadBalancerArns`) **[V]**. Relaxing to `{stem, stem+"s"}` gives **172/172**, and the triad is always complete — all 172 have both a `Selector` sibling and a real value field at the same nesting level **[V]**. Still: parse the description, use the rule only as a cross-check.

**Which target kind:** **not derivable.** 58/172 EC2 refs (34%) name a kind the field name does not contain **[V]**:

```
kmsKeyIdRef                   -> Key (kms)              cidrRef                 -> VPC (ec2)
defaultNetworkAclIdRef        -> VPC (ec2)              typeRef                 -> CustomerGateway (ec2)
allocationIdRef               -> EIP (ec2)              versionRef              -> LaunchTemplate (ec2)
logDestinationRef             -> Group (cloudwatchlogs) iamRoleArnRef           -> Role (iam)
networkInterfaceIdRef         -> Instance (ec2)         cidrBlocksRefs          -> VPC (ec2)
connectionNotificationArnRef  -> Topic (sns)            networkLoadBalancerArnsRefs -> LB (elbv2)
```

Note `NetworkInterfaceSgAttachment.networkInterfaceIdRef → Instance` and `ManagedPrefixListEntry.cidrRef → VPC` — these are upstream config choices that look wrong but are what the controller will actually resolve. Render what the description says.

**Cross-package targets are the norm.** `provider-aws-ec2` refs reach into 6 other groups: `ec2` 153, `kms` 8, `iam` 5, `elbv2` 2, `vpclattice` 2, `sns` 1, `cloudwatchlogs` 1 **[V]**. A dragged EC2 `Instance` will suggest a `kms.Key` whose CRD may not be installed. Build the kind index across *all* packages resolvable via `get-crds`, and mark unresolved targets as "requires provider-aws-kms" rather than dropping the edge.

**What the reference resolves to is not in the schema at all.** Upjet's `Reference.Extractor` defaults to the target's external name; many refs override it to `status.atProvider.arn` or a `ExtractParamPath` into `spec.forProvider` **[D]** — this lives in the provider's Go config and is unrecoverable from the CRD. Consequence for a canvas: you can draw the edge and emit a correct `Ref`, but you **cannot** tell the user which value flows across it, and you cannot replicate the wiring by hand-patching `status.atProvider.id` without risking the wrong field.

---

## 3. Required vs optional

Three independent mechanisms, in increasing order of how much they matter:

**(a) `required` arrays.** Distribution of `spec.forProvider.required` **[V]**:

| provider set | value | count |
|---|---|---|
| AWS (ec2/iam/rds/s3/eks) | `["region"]` | 268 |
| AWS | `[]` | 25 |
| AWS | `["key","region","resourceId"]` | 2 |
| AWS | `["policyArn","region"]` | 1 |
| GCP storage | `[]` | 14 |
| Azure storage | `[]` | 17 |

This is exactly the pattern the brief anticipated: **`region` is the only top-level required field on almost every AWS MR, and on GCP/Azure nothing is required at all.** `spec.required` is always `["forProvider"]`.

**(b) CEL on `.spec` — the real required fields.** Every one of the 188 AWS + 70 GCP/Azure rules uses one message template **[V]**. Full rule from `accounts.storage.azure.m.upbound.io`:

```
message: "spec.forProvider.accountReplicationType is a required parameter"
rule: "!('*' in self.managementPolicies || 'Create' in self.managementPolicies
       || 'Update' in self.managementPolicies)
       || has(self.forProvider.accountReplicationType)
       || (has(self.initProvider) && has(self.initProvider.accountReplicationType))"
```

Semantics a form must reproduce: the field is required **only when** `managementPolicies` includes `*`, `Create`, or `Update`, and is satisfied by **either** `forProvider` **or** `initProvider`. Observe-only resources legitimately omit it.

Worked examples **[V]**:

| Resource | `forProvider.required` | CEL-required |
|---|---|---|
| `Queue` (sqs, live cluster) | `["region"]` | *(none)* |
| `AMI` (ec2) | `["region"]` | `name` |
| `Instance` (rds) | `["region"]` | `instanceClass` |
| `Bucket` (gcp storage) | *(none)* | `location` |
| `Account` (azure storage) | *(none)* | `accountReplicationType`, `accountTier`, `location` |

**(c) Nested `required`.** 176 nested-required entries in the EC2 forProvider subtree — and **every single one is on a reference or secret object**, never on a user-facing data field **[V]**: `.vpcIdRef → name`, `.route[].gatewayIdRef → name`, `.tunnel1PresharedKeySecretRef → [key, name]`. So there is no such thing as a deeply-nested required business field in these schemas; treat nested `required` as belonging to the ref/secret widget and never surface it as a form validation on an optional block the user hasn't opened.

`spec.initProvider.required` is empty in **364/365** m-CRDs (the lone exception is `ServerCertificate.privateKeySecretRef`) **[V]** — `initProvider` is a pure optional mirror.

---

## 4. OpenAPI constructs actually present

### Full census — 282 AWS m-CRDs, 39,170 nodes **[V]**

| keyword | nodes | % | verdict |
|---|---|---|---|
| `description` | 32,997 | 84.2% | **must support** |
| `enum` | 4,708 | 12.02% | **all boilerplate — 0 real** |
| `default` | 2,857 | 7.29% | must support |
| `additionalProperties` | 1,835 | 4.68% | **must support** (maps) |
| `x-kubernetes-list-type` | 1,065 | 2.72% | should support (`set` → chips) |
| `format` | 889 | 2.27% | ignore (`date-time` 297, `int64` 592 — all Crossplane status boilerplate) |
| `x-kubernetes-map-type` | 819 | 2.09% | always `granular`; ignore |
| `x-kubernetes-list-map-keys` | 296 | 0.76% | only `status.conditions` → `["type"]`; ignore |
| `x-kubernetes-validations` | 131 | 0.33% | **must support** (this is "required") |
| `pattern` | **0** | 0% | skip |
| `minimum` / `maximum` | **0** / **0** | 0% | skip |
| `minLength`/`maxLength`/`minItems`/`maxItems`/`minProperties`/`maxProperties` | **0** | 0% | skip |
| `multipleOf` / `uniqueItems` | **0** | 0% | skip |
| `oneOf` / `anyOf` / `allOf` / `not` | **0** | 0% | skip |
| `$ref` | **0** | 0% | skip |
| `x-kubernetes-preserve-unknown-fields` | **0** | 0% | **needed for other providers** |
| `x-kubernetes-int-or-string` | **0** | 0% | skip |
| `nullable` | **0** | 0% | skip |

GCP + Azure (62 CRDs, 8,763 nodes) reproduce this **exactly** — same zeros, 0 real enums, CEL 0.56% **[V]**.

Types: `string` 7,166 · `object` 3,513 · `boolean` 847 · `array` 731 · `number` 726 · `integer` 204 (EC2 only). Note **`number` outnumbers `integer` 3.5:1** — upjet emits `type: number` for integral fields (`delaySeconds`, `maxMessageSize`). A form builder that renders `number` as a float input will produce `30.0` where AWS wants `30`. Coerce `number` to integer when no fractional value is plausible, or always emit unquoted integers in the template.

Only **106 of 205** arrays in `spec.forProvider` carry `x-kubernetes-list-type` **[V]**; the other 99 default to `atomic`.

### SQS Queue CRD — exact counts **[V]**

`sqs.aws.m.upbound.io/v1beta1`, 542 lines of YAML:

```
total schema nodes                86
  object 14 · string 41 · number 18 · boolean 9 · array 2 · integer 2
nodes with no description          7
spec.forProvider subtree          20 nodes, 18 scalar leaf fields, max depth 4
status.atProvider                 22 fields
enum                               1   (managementPolicies.items — the ONLY enum in the file)
default                            2   (managementPolicies, providerConfigRef)
format                             3   (all in status.conditions / observedGeneration)
additionalProperties               4   (tags/tagsAll in spec.forProvider, spec.initProvider, status)
x-kubernetes-map-type              4   (all "granular")
x-kubernetes-list-type             1   (status.conditions → "map")
x-kubernetes-list-map-keys         1   (status.conditions → ["type"])
pattern, minimum, maximum, oneOf, anyOf, CEL, preserve-unknown-fields:  0
cross-resource references:         0
```

`spec.forProvider` is 18 flat scalars, `required: ["region"]`, no refs, no CEL. It is the simplest possible MR and a good default fixture — but a *bad* representative: `launchtemplates.ec2` has **799 nodes** and `instances.ec2` has 558 **[V]**.

### Depth

Max property depth **7** in EC2, **11** across the full AWS sample **[V]**. Deepest path:

```
.spec.forProvider.launchTemplateConfig[].launchTemplateSpecification.launchTemplateIdRef.policy.resolution
```

Depth histogram (AWS, 296 schemas): `3: 13,284 · 4: 10,612 · 5: 6,701 · 6: 1,574 · 7: 1,592 · 8: 530 · 9: 513 · 10: 51 · 11: 54`. The mass sits at depth 3–5 (i.e. `spec.forProvider.<field>` and one or two nested blocks). Design the form for depth 5 and provide a raw-YAML escape below that; don't build 11 levels of accordion.

### Description quality

10,349 descriptions in the AWS `forProvider` subtrees; median length 73 chars, mean 114 **[V]**. 6,173 nodes (15.8%) have **no** description at all. Content is verbatim Terraform registry prose:

- **3.7%** (387) contain snake_case Terraform identifiers that leak the abstraction — e.g. `passwordSecretRef`: *"Cannot be set if manage_master_user_password is set to true."*; `Queue.policy`: *"It is preferred to use the aws_sqs_queue_policy resource."* Consider a display-time rewrite (`aws_sqs_queue_policy` → `QueuePolicy`).
- **3.3%** (1,074) embed URLs; they are plain text, not markdown links.
- **127** string fields state allowed values in prose with no `enum` — this is your enum source. Patterns that hit: `Valid values are X, Y`, `Possible values are`, `must be one of`, `Defaults to`.
- Two `region` description variants only: `"Region where this resource will be managed. Defaults to the Region set..."` (263) and `"Region is the region you'd like your resource to be created in."` (8) **[V]** — reliable enough to special-case the region widget by description match as well as by name.

---

## 5. `status.atProvider` — the downstream surface

`status.atProvider` is a **late-initialized mirror of `forProvider` plus computed outputs**. For SQS `Queue` **[V]**: all 18 `forProvider` fields reappear, plus `id`, `arn`, `url`, `tagsAll` (22 total). `id` = *"URL for the created Amazon SQS queue"*; `url` = *"Same as id"* — literal duplication.

Across 295 AWS m-CRDs **[V]**: mean `forProvider` 12.2 fields, mean `atProvider` 13.9, mean shared 9.1. `atProvider` is a strict superset of `forProvider` in only **57/295** — the non-shared `forProvider` fields are almost entirely refs (461) and selectors (444), with just **5** genuine non-ref exceptions in the whole sample (`autoGeneratePassword` on RDS Cluster/Instance; `clusterName`/`region`/`refreshPeriod` on EKS `ClusterAuth`).

`atProvider`-only fields, by frequency **[V]**: `id` 295 · `tagsAll` 166 · `arn` 147 · 781 others.

**Downstream referencing.** Every MR exposes `id` (295/295) and most expose `arn` (147/295). Under function-go-templating the composed-resource observation is at `.observed.resources.<name>.resource.status.atProvider.<field>`, which is how one composed resource feeds another. Two rules for a generator:

- **Never emit `status.atProvider` paths without a guard.** They are absent on the first reconcile. The live composition on this cluster already models the correct idiom **[V]**: `{{- $available = dig "resource" "status" "availableReplicas" 0 $deployed -}}` behind an `if $deployed`.
- **Prefer the native `<f>Ref` over templating `status.atProvider.id`.** The ref is resolved by the provider controller with the correct extractor (which may be `arn`, not `id` — and you cannot tell which from the schema, § 2). Reserve `status.atProvider` templating for values with no ref triad.

Schema shape for the generator's "output ports": `status.atProvider` child types across EC2 are `string` 1,070 · `object` 219 · `boolean` 122 · `array` 107 · `number` 81 **[V]**. Surface `id` and `arn` as pinned ports, then the flat scalars.

Also on `status`: `conditions` (`x-kubernetes-list-type: map`, `list-map-keys: ["type"]`) and `observedGeneration`. `additionalPrinterColumns` gives you free node-badge fields **[V]**: `SYNCED`, `READY` (both from `status.conditions[?(@.type=='...')].status`), `EXTERNAL-NAME` (from `metadata.annotations.crossplane.io/external-name`), `AGE`.

---

## 6. Deprecated / duplicated fields, and tags

**Version deprecation.** 14 of 102 legacy EC2 CRDs serve two versions (`v1beta1` + `v1beta2`); storage is on `v1beta2` for `routes` but on `v1beta1` for the other 13 **[V]**. Every m-variant serves exactly one version. `provider-kubernetes` v1.0.0 marks `objects.kubernetes.crossplane.io` `v1alpha1` with `deprecated: true` **[V]**. **Generator rule: pick the `storage: true` version, skip `deprecated: true`, and never assume `versions[0]`.**

**Duplicated fields, confirmed:**
- `initProvider` duplicates `forProvider` minus `region` in **101/102** EC2 CRDs **[V]**. Render it as a mode toggle on the existing form, never as a second field tree.
- `status.atProvider` duplicates `forProvider` (§ 5).
- `Queue.status.atProvider.url` duplicates `.id` verbatim.

**`tagsAll` — a computed field leaking into spec.** Normally `status`-only (166 occurrences), but it appears in **`spec.forProvider`** on nested EC2 block devices **[V]**:

```
.spec.forProvider.ebsBlockDevice[].tagsAll     (6)
.spec.forProvider.rootBlockDevice[].tagsAll    (2)
.spec.forProvider.rootBlockDevice.tagsAll      (4)
.spec.initProvider.*.tagsAll                   (12)
```

These are provider-computed (`tags` + provider `default_tags`) and setting them causes drift. **Suppress `tagsAll` from any spec-side form.**

**Tags conventions per family** **[V]** — the shape is universal, the *name* and *placement* are not:

| family | spec field | shape | status companion |
|---|---|---|---|
| AWS | `spec.forProvider.tags` (382) | `object` + `additionalProperties:{type:string}` + `x-kubernetes-map-type: granular` | `tags` (193) **and** `tagsAll` (178) |
| Azure | `spec.forProvider.tags` (6) | identical | `tags` (3) |
| GCP | `spec.forProvider.labels` (6) | identical | `labels` (3) |

Every tags/labels node in all 344 CRDs is the same `map[string]string` with `x-kubernetes-map-type: granular` — zero variation **[V]**. Detect by shape (`type:object` ∧ `additionalProperties.type == "string"`), then special-case the *name* (`tags`|`labels`) for the dedicated widget. `granular` means server-side apply merges per-key, so a composition can safely set platform tags without clobbering user tags.

---

## Field kind taxonomy for the code generator

Classify each schema node in this order; **first match wins**. Paths are relative to the CRD root.

### Tier 0 — structural, never a form field

| # | Kind | Detection | Generator behaviour |
|---|---|---|---|
| 0.1 | `ROOT_SCAFFOLD` | `.apiVersion`, `.kind`, `.metadata` | Emit from CRD `group`/`version`/`names.kind`; never render |
| 0.2 | `ENVELOPE` | direct children of `.spec` other than `forProvider`/`initProvider` | Render once in a shared "Resource options" panel, generated *from its own schema* (see § 1 — do not hard-code) |
| 0.3 | `INIT_MIRROR` | `.spec.initProvider.*` | Suppress the subtree; expose an "apply at creation only" toggle per field on the `forProvider` form |
| 0.4 | `STATUS_OUTPUT` | `.status.atProvider.*` | Not a field — these are the node's **output ports** on the canvas. Pin `id`, `arn`; guard every template reference (§ 5) |
| 0.5 | `STATUS_META` | `.status.conditions`, `.status.observedGeneration` | Node badges; drive from `additionalPrinterColumns` |
| 0.6 | `COMPUTED_LEAK` | name is `tagsAll` anywhere under `.spec` | **Suppress** (§ 6) |

### Tier 1 — reference & secret widgets (check before scalars; these end in `Ref`)

| # | Kind | Detection | Widget |
|---|---|---|---|
| 1.1 | `SECRET_REF` | name ends `SecretRef`, `properties ⊇ {name, key}` | Secret picker (name + key). **Never a text input.** v2 shape `{key,name}`, legacy `{key,name,namespace}` |
| 1.2 | `WRITE_ONLY_SECRET_REF` | name ends `WoSecretRef` | Same, labelled write-only (never read back) |
| 1.3 | `XRESOURCE_REF` | name ends `Ref`, `type == object`, has `name` prop, **not** 1.1/1.2, **not** `matchControllerRef` | Hidden from the plain form; **this is the canvas edge**. Parse description for target kind + group + populated field |
| 1.4 | `XRESOURCE_REF_LIST` | name ends `Refs`, `type == array` | Multi-edge. Read description off the **array** node, not `items` |
| 1.5 | `XRESOURCE_SELECTOR` | name ends `Selector`, has `matchLabels` ∧ `matchControllerRef` | Label-selector widget; alternative binding mode for the same edge |
| 1.6 | `REF_TARGET_VALUE` | scalar whose name is the `populate` target of a 1.3/1.4 sibling | Mutually exclusive with its Ref/Selector — render as a 3-way: literal / reference / selector |
| 1.7 | `REF_POLICY` | `.policy.{resolution,resolve}` inside any of the above | Advanced-only; the two boilerplate enums |

### Tier 2 — data fields

| # | Kind | Detection | Widget |
|---|---|---|---|
| 2.1 | `ENUM_SCALAR` | `enum` present ∧ not a boilerplate enum | Select. **Expect zero from MRs; common from XRDs** |
| 2.2 | `PROSE_ENUM` | `type: string`, no `enum`, description matches `Valid values are\|Possible values are\|must be one of\|Defaults to` + comma list | Combobox with parsed suggestions, free text allowed (127 in the AWS sample) |
| 2.3 | `OPAQUE_JSON` | `type: string` ∧ description matches `\bJSON\b\|policy document` | Code editor, JSON mode + lint. `Queue.policy`, `IAM Policy.policy`, `Role.inlinePolicy[].policy` |
| 2.4 | `PLACEMENT` | name ∈ {`region`,`location`} at `forProvider` root | Region/location picker; the one field required on nearly every AWS MR |
| 2.5 | `MAP_TAGS` | `type: object` ∧ `additionalProperties.type == "string"` ∧ name ∈ {`tags`,`labels`} | Tag editor; auto-inject platform tags (safe: `granular`) |
| 2.6 | `MAP_STRING` | `type: object` ∧ `additionalProperties.type == "string"` ∧ not 2.5 | Key/value rows |
| 2.7 | `ARRAY_OF_OBJECT` | `type: array` ∧ `items.type == "object"` | Repeatable card list (318 in EC2) |
| 2.8 | `ARRAY_SET` | `type: array` ∧ scalar items ∧ `x-kubernetes-list-type: set` | Chip input, dedupe, order-insensitive |
| 2.9 | `ARRAY_ATOMIC` | `type: array` ∧ scalar items ∧ no list-type | Ordered list; whole-array replace on patch (99/205 in AWS) |
| 2.10 | `OBJECT_BLOCK` | `type: object` ∧ has `properties` | Collapsible fieldset; collapse by default below depth 3 |
| 2.11 | `SCALAR_INT` | `type: integer`, **or** `type: number` with no fractional semantics | Integer input; **emit unquoted, no decimal point** (§ 4) |
| 2.12 | `SCALAR_NUM` | `type: number`, genuinely fractional | Number input |
| 2.13 | `SCALAR_BOOL` | `type: boolean` | Checkbox / tri-state (unset ≠ false — `default` matters) |
| 2.14 | `SCALAR_STRING` | `type: string`, nothing above matched | Text input. **The overwhelming majority** — 3,493 in AWS `forProvider` |
| 2.15 | `EMBEDDED_MANIFEST` | `x-kubernetes-preserve-unknown-fields: true` (± `x-kubernetes-embedded-resource`) | **No form is possible** — raw YAML editor. `provider-kubernetes` `Object.spec.forProvider.manifest` |
| 2.16 | `UNKNOWN` | anything else | Raw YAML escape hatch. Always provide one |

### Requiredness resolver (orthogonal flag on every Tier-2 node)

```
required(path) =
     path ∈ parent.required                              # 'region' on AWS; ref .name
  OR ∃ CEL rule on .spec whose message == 
       "spec.forProvider.<path> is a required parameter" # the real source
       AND managementPolicies ∩ {*, Create, Update} ≠ ∅
       AND not satisfied by initProvider
```

Render CEL-required fields as required **only** while `managementPolicies` includes `*`/`Create`/`Update`; grey them out for observe-only resources.

---

## Reproduction

```bash
cat > prov.yaml <<'EOF'
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata: {name: p}
spec: {package: xpkg.upbound.io/upbound/provider-aws-ec2:v2.4.0}
EOF
crossplane xpkg get-crds prov.yaml --output-dir ./crds --flat          # 204 CRDs, 3.5s
crossplane xpkg get-crds prov.yaml --output-dir ./sj --flat --json-schema  # 218 JSON Schemas (per-version)
```

Flags **[V]**: `--flat`, `--json-schema`, `-o/--output-dir`, `--cache-dir` (default `~/.crossplane/cache`), `--clean-cache`, `--update-cache`, `--no-cache`, `--crossplane-image`. Input accepts `crossplane.yaml`, a directory, `-` for stdin, or a `Provider`/`Function`/`Configuration` manifest. Also present: `crossplane xrd convert` (XRD → CRD) and `crossplane xrd generate` (XR or SimpleSchema → XRD) **[V]**.

## Unconfirmed / caveats

- **Not verified:** which target field each `Ref` extractor actually reads. Docs state the default is external name with common overrides to `status.atProvider.arn` and `ExtractParamPath` **[D]**; I did not read the provider-aws Go config, and it is not in the CRD.
- **Not verified:** MRD schema completeness for an **Inactive** MRD. All 8 MRDs on this cluster are `Active` because the `default` ManagedResourceActivationPolicy is `activate: ["*"]` **[V]**. The Active MRDs do carry the full schema **[V]**; I inferred (did not test) that Inactive ones do too. Worth one test with a narrowed MRAP before relying on it.
- `MRD.spec.connectionDetails` is `null` on all 8 SQS MRDs **[V]** — the documented connection-details discovery path is unpopulated by this provider, so don't depend on it.
- **Provider bias:** 344 of the 353 CRDs analysed are upjet-generated. The zero-enum / zero-pattern result is an *upjet* property, not a Crossplane one. `provider-kubernetes` (non-upjet) does carry a real enum (`readiness.policy`) and a real CEL business rule **[V]**. Hand-written providers will have richer schemas — build for them and treat upjet's flatness as the common case, not the only case.
- `xpkg.upbound.io/v2/<repo>/tags/list` returns an empty tag list **[V]**, so version discovery needs the marketplace API or a user-supplied tag.

**Sources:** [Managed Resources · Crossplane v2.4](https://docs.crossplane.io/latest/managed-resources/managed-resources/) · [upjet: configuring-a-resource](https://github.com/crossplane/upjet/blob/main/docs/configuring-a-resource.md) · [upjet config package](https://pkg.go.dev/github.com/upbound/upjet/pkg/config) · [upbound/provider-gcp-storage](https://marketplace.upbound.io/providers/upbound/provider-gcp-storage/latest)