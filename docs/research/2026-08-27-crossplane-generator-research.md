# Crossplane Composition + XRD Generator — Grounding Research

**Date:** 2026-08-27
**Scope:** provider-agnostic Composition + XRD generator, drag-and-drop GUI + CLI, emitting `function-go-templating` Compositions.
**Target stack observed throughout:** Crossplane server **v2.4.0** (`kind-platform`), Crossplane CLI **v2.5.0**, k8s **v1.36.1**, `function-go-templating` **v0.12.0**, `function-auto-ready` **v0.5.0**, Docker 29.7.2, ArgoCD **v3.5.1**.

## Provenance legend

| Tag | Meaning |
|---|---|
| **[V]** | Verified by *running it* during the originating research brief |
| **[V-me]** | Verified by *running it during this synthesis*, 2026-08-27 |
| **[D]** | Read in docs / source / help text only — not executed |
| **[U]** | Explicitly flagged unverified by the originating brief |
| **UNRESOLVED** | Two briefs disagree; not silently reconciled |

### Source briefs

| Brief | Lines | Status |
|---|---|---|
| `raw/crd-schema-shape.md` | 421 | present |
| `raw/go-templating.md` | 668 | present |
| `raw/xrd-v2.md` | 512 | present |
| `raw/validation-tooling.md` | 586 | present |
| `raw/prior-art.md` | 314 | present |
| `raw/canvas-ux.md` | 419 | present |
| `raw/cli-and-gitops.md` | 545 | present |
| `raw/schema-sourcing.md` | — | **MISSING** |

### ⚠️ Missing brief: `schema-sourcing.md`

**`/Users/kaurkallas/compositionfactory/docs/research/raw/schema-sourcing.md` does not exist.** [V-me] The `raw/` directory contains exactly seven files; there is no journal, log, or partial output anywhere in the repository (`find` over the whole project tree returns only the seven briefs). **No content has been invented to fill the gap.**

What *did* survive is a set of **executable scratch artifacts** in the workflow's shared scratchpad at
`/private/tmp/claude-501/-Users-kaurkallas-compositionfactory/96c83f73-f673-491a-aa88-c90f84f1eafb/scratchpad/`, including a complete, compiling Go xpkg extractor at `xpkgget/main.go`. I verified that program myself: it builds clean, and its output is **byte-identical (MD5 `6023a8c7e51284baa6c9186abb791357`)** to the package stream the `crossplane` CLI extracts. [V-me] It is reproduced in full in §3.6 and is the single most load-bearing recovery from the lost brief.

**What remains genuinely unknown because the brief is missing** (do not assume these are answered):

- Registry **version/tag discovery** at scale. The only datapoint anywhere is a negative one: `xpkg.upbound.io/v2/<repo>/tags/list` returns an **empty tag list** [V], so tag enumeration needs the Upbound marketplace API or a user-supplied tag. No brief established a working discovery path.
- **Authentication** behaviour for private registries and Upbound-hosted private repos beyond "`authn.DefaultKeychain` reads `~/.docker/config.json`" [D, from the recovered source].
- A **provider catalogue / index** strategy — how the GUI populates a palette of *installable* providers rather than *installed* ones.
- Rate limits, mirror/air-gap strategy, and cache invalidation policy for the schema store.

Treat §3.6 as the recovered core and the four bullets above as an open work item (see §6, Q1).

---

## 1. Executive summary — the 12 facts that most constrain the design

1. **Every upjet provider ships every managed resource twice** — a cluster-scoped legacy group (`ec2.aws.upbound.io`) and a v2 namespaced `.m.` group (`ec2.aws.m.upbound.io`) — so the generator must choose a variant, and the choice is dictated by the XRD's `scope`. [V] [V-me]
2. **There is no machine-readable cross-resource reference link in any CRD** — zero vendor `x-*` extensions across 344 CRDs from four providers; the target kind exists *only* as English prose in the `description` string. [V]
3. **Upjet MR schemas contain zero real enums, patterns, bounds, `oneOf`, `$ref` or `nullable`** — a form builder gets `type` + `description` and nothing else, so prose-mining is the only enum source. [V]
4. **The genuinely required fields are encoded only in CEL rule *messages***, not in `required` arrays: `spec.forProvider.<FIELD> is a required parameter`, 100% one template across 258 rules. [V]
5. **The v2 namespaced MR envelope is structurally different from v1** — no `deletionPolicy`, no `publishConnectionDetailsTo`, `providerConfigRef` requires `kind`, and `namespace` is stripped from every secret ref — so a v1-shaped MR emitted into a v2 Composition gets silently pruned by the API server. [V]
6. **`LegacyCluster` is not a valid `scope` in `apiextensions.crossplane.io/v2`** (docs say otherwise and are wrong), and omitting `scope` makes the server default to `Namespaced` while `crossplane xrd convert` defaults to `LegacyCluster` — so `scope:` must always be emitted explicitly. [V]
7. **XRD schema fields are policed by selective copying and overwrite, not rejection**: `spec.crossplane` and `status.conditions` are silently replaced, most illegal constructs apply cleanly and fail only as a missing `Established` condition on the derived CRD. [V]
8. **`function-go-templating`'s `options: ["missingkey=error"]` is a top-level input field, not nested under `inline`** (the README is wrong, and the nested form is a fatal error) — and without it a missing XR field silently renders the literal string `<no value>` at any depth. [V]
9. **`.desired.composed` does not exist** — the README documents a path that isn't there; it is `.desired.resources.<name>.resource`, and Crossplane v2 nests XR plumbing under `.observed.composite.resource.spec.crossplane`. [V]
10. **`crossplane composition render` is byte-for-byte deterministic** — frozen timestamps, content-hashed names, deterministic owner UIDs, three runs to an identical MD5 — which makes golden-file testing a first-class strategy rather than a hope. [V]
11. **`crossplane resource validate` is the only real validation gate**: `--xrd` on render performs *defaulting only* and caught nothing in a deliberately-invalid XR, and missing schemas exit **0** unless `--error-on-missing-schemas` is passed. [V]
12. **Parsing existing go-templates back into a node graph is not feasible** — the template AST's TEXT nodes are non-YAML fragments, document shape is data-dependent, and indentation is semantic — so "adopt as opaque `rawTemplate`" is the honest 90% of round-tripping at ~5% of the risk. [V]

Two further facts that reshape scope but didn't make the top 12: **schema delivery to the browser is a non-problem** (all 204 EC2 CRDs are 54 KB brotli [V]), and **the competitive gap is real and large** — `crossplane composition generate` emits a 12-line stub with zero composed resources [V].

---

## 2. Hard constraints & gotchas

### 2.1 The dual cluster-scoped / `.m.` namespaced CRD variants — and which to emit

**The fact.** Every upjet provider package contains each managed resource **twice**. Verified independently three ways:

- `provider-aws-ec2:v2.4.0` → **204 CRDs**: 102 in `ec2.aws.m.upbound.io` (scope `Namespaced`) and 102 in `ec2.aws.upbound.io` (scope `Cluster`). [V]
- `crossplane xpkg get-crds` on a `provider-aws-sqs` manifest writes **both** group trees. [V]
- I extracted the `provider-aws-sqs:v2` package stream myself: **8 CRDs, exactly 4 in `sqs.aws.m.upbound.io` and 4 in `sqs.aws.upbound.io`.** [V-me]

```
$ awk '/^  group: /{print $2}' verify-sqs.yaml | sort | uniq -c
   4 sqs.aws.m.upbound.io
   4 sqs.aws.upbound.io
```

**Which to emit: the `.m.` namespaced variant**, for a v2 `scope: Namespaced` XRD. This is not a style preference — the two variants have **structurally different spec envelopes**, and emitting the wrong shape produces fields the API server prunes. The exact diff to hard-code:

| Path | legacy (`*.upbound.io`, `Cluster`) | v2 (`*.m.upbound.io`, `Namespaced`) |
|---|---|---|
| `spec.deletionPolicy` | present, `enum:[Orphan,Delete]`, default `Delete` | **absent** |
| `spec.publishConnectionDetailsTo` | absent in AWS family; present in provider-kubernetes | **absent** |
| `spec.providerConfigRef` | `{name, policy}`, required `[name]`, default `{name: default}` | `{kind, name}`, required `[kind, name]`, default `{kind: ClusterProviderConfig, name: default}` |
| `spec.writeConnectionSecretToRef` | `{name, namespace}`, both required | `{name}` only |
| `<f>SecretRef` | `{key, name, namespace}` | `{key, name}` |
| `<f>Ref` | `{name, policy}` | `{name, namespace, policy}` |
| ref item description | `"A Reference to a named object."` | `"A NamespacedReference to a named object."` |
| CRD scope | `Cluster` | `Namespaced` |

Counts: **116/116** legacy EC2 schemas carry `deletionPolicy`; **0/102** m-variants do. [V] The `spec` property set is **exactly** `['forProvider','initProvider','managementPolicies','providerConfigRef','writeConnectionSecretToRef']` in 102/102 EC2 m-CRDs and all GCP/Azure m-CRDs. [V]

**Detection rule:** fork the envelope renderer on the presence of `.m.` in the API group. Confirmed identically on upjet-AWS *and* non-upjet provider-kubernetes. [V]

**Three traps inside this one constraint:**

- **`providerConfigRef.kind` has no enum and no CEL** — it is a bare `type: string`. [V] The form builder must supply the allowed values itself. Do not hard-code two strings; **enumerate CRDs in the MR's provider group whose kind ends in `ProviderConfig`**. The reason is a genuine name collision:
  ```
  providerconfigs.aws.m.upbound.io          Namespaced  kind=ProviderConfig
  clusterproviderconfigs.aws.m.upbound.io   Cluster     kind=ClusterProviderConfig
  providerconfigs.aws.upbound.io            Cluster     kind=ProviderConfig     (legacy group)
  ```
  `ProviderConfig` means a **Namespaced** resource in the `.m.` group and a **Cluster** resource in the legacy group. [V]
- **The envelope is not universal.** `ObservedObjectCollection` (provider-kubernetes) has **no `forProvider`, no `writeConnectionSecretToRef`, no `managementPolicies`** — its spec is `['objectTemplate','observeObjects','providerConfigRef']`. Only `spec.providerConfigRef` survived every CRD inspected. [V] **Generator rule: never hard-code the envelope. Compute `envelope = spec.properties − {forProvider, initProvider}` and render whatever is left from its own schema.**
- **Version selection.** 14 of 102 legacy EC2 CRDs serve two versions (`v1beta1` + `v1beta2`), and storage is `v1beta2` for `routes` but `v1beta1` for the other 13. Every m-variant serves exactly one version. `provider-kubernetes` v1.0.0 marks `objects.kubernetes.crossplane.io` `v1alpha1` `deprecated: true`. [V] **Pick the `storage: true` version, skip `deprecated: true`, never assume `versions[0]`.**

**The palette-population trap (Crossplane v2, new enough that all prior art predates it).** Crossplane v2 gates which MRs actually exist via `ManagedResourceDefinition` (`mrd`) + `ManagedResourceActivationPolicy` (`mrap`). A canvas must populate its palette from **`mrd` with `state: Active`** (or the package's CRDs when offline), *not* from `kubectl get crd` — with large providers where most MRs ship Inactive, a naive CRD listing shows resources the cluster will not accept. [V] On a live v2 cluster `kubectl get managedresourcedefinitions` is a second full schema source carrying the complete `openAPIV3Schema` **even for MRs whose CRD does not exist** (`spec.state: Inactive`). [V] — though whether an *Inactive* MRD carries the full schema was **inferred, not tested**: all 8 MRDs on the test cluster were Active because the `default` MRAP is `activate: ["*"]`. [U] Worth one test with a narrowed MRAP before relying on it.

Also: `MRD.spec.connectionDetails` is `null` on all 8 SQS MRDs [V] — the documented connection-details discovery path is unpopulated by this provider. Don't depend on it.

### 2.2 Cross-resource reference triads — discoverability from CRD schemas alone

**Verdict: the *structure* is fully discoverable; the *target kind* is not, except by parsing English prose. There is no machine-readable link.**

This was verified exhaustively — every key starting with `x-` or `$` was collected across 344 CRDs from four providers. The complete census: [V]

```
x-kubernetes-list-type 1314 · x-kubernetes-map-type 905 · x-kubernetes-list-map-keys 373
x-kubernetes-validations 184 · x-kubernetes-embedded-resource 6 · x-kubernetes-preserve-unknown-fields 6
```

Nothing else. **Zero vendor extensions.** CRD `metadata.annotations` carries only `controller-gen.kubebuilder.io/version` and `kustomize.config.k8s.io/id`. [V] The description string is the sole carrier.

**A real triad**, from the live cluster (`queuepolicies.sqs.aws.m.upbound.io`). `spec.forProvider` properties are `['policy','queueUrl','queueUrlRef','queueUrlSelector','region']`: [V]

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

**What IS reliably discoverable — structural detection:** [V]

- `name.endsWith("Ref")` ∧ `type == "object"` ∧ `properties ⊇ {name}` → single ref
- `name.endsWith("Refs")` ∧ `type == "array"` → list ref. **Read the description off the array node, not `items`** — the item description degrades to the useless `"A NamespacedReference to a named object."`
- `name.endsWith("Selector")` ∧ `properties ⊇ {matchLabels, matchControllerRef}` → selector
- **Exclude `matchControllerRef` explicitly** — it ends in `Ref` and is a boolean (172 false positives in EC2 alone)
- **Exclude `name.endsWith("SecretRef")`** — a different category entirely

**Which value field the triad populates:** the strict rule `Ref → stem`, `Refs → stem + "s"` is correct for **167/172**; the 5 failures are fields already ending in a plural (`cidrBlocksRefs` → `cidrBlocks`, `gatewayLoadBalancerArnsRefs` → `gatewayLoadBalancerArns`). Relaxing to `{stem, stem+"s"}` gives **172/172**, and the triad is always complete — all 172 have both a `Selector` sibling and a real value field at the same nesting level. [V]

**What is NOT discoverable without prose — the target kind.** The grammar is identical across AWS, GCP and Azure, with a **100% parse rate (172/172 refs and 172/172 selectors in EC2, 0 unparsed)**: [V]

```
^(Reference|References) to (?:a |an )?(?P<Kind>\w+) in (?P<group>\w+) to populate (?P<field>\w+)\.$
^Selector for (?:a list of |a |an )?(?P<Kind>\w+) in (?P<group>\w+) to populate (?P<field>\w+)\.$
```

`group` is the **short** group segment (`sqs`, `kms`, `elbv2`, `cloudwatchlogs`, `network`, `cloudplatform`); the full group is `<short>.<family-domain>`. **Resolve it against your CRD index rather than string-concatenating** — the family domain differs per provider and per v1/v2 group.

**Name-based heuristics are not a fallback: 34% (58/172) of EC2 ref targets are not guessable from the field name.** [V]

```
kmsKeyIdRef                   -> Key (kms)              cidrRef                 -> VPC (ec2)
defaultNetworkAclIdRef        -> VPC (ec2)              typeRef                 -> CustomerGateway (ec2)
allocationIdRef               -> EIP (ec2)              versionRef              -> LaunchTemplate (ec2)
logDestinationRef             -> Group (cloudwatchlogs) iamRoleArnRef           -> Role (iam)
networkInterfaceIdRef         -> Instance (ec2)         cidrBlocksRefs          -> VPC (ec2)
connectionNotificationArnRef  -> Topic (sns)            networkLoadBalancerArnsRefs -> LB (elbv2)
```

Note `NetworkInterfaceSgAttachment.networkInterfaceIdRef → Instance` and `ManagedPrefixListEntry.cidrRef → VPC` — these look wrong but are what the controller actually resolves. **Render what the description says.**

**Cross-package targets are the norm, not the exception.** `provider-aws-ec2` refs reach into 6 other groups: `ec2` 153, `kms` 8, `iam` 5, `elbv2` 2, `vpclattice` 2, `sns` 1, `cloudwatchlogs` 1. [V] A dragged EC2 `Instance` will suggest a `kms.Key` whose CRD may not be installed. **Build the kind index across all packages resolvable via `get-crds`, and mark unresolved targets as "requires provider-aws-kms" rather than dropping the edge.**

**The hard limit — what the reference *resolves to* is not in the schema at all.** Upjet's `Reference.Extractor` defaults to the target's external name; many refs override it to `status.atProvider.arn` or an `ExtractParamPath` into `spec.forProvider`. This lives in the provider's Go config and is **unrecoverable from the CRD**. [D — the provider-aws Go config was not read; flagged unverified in the source brief] **Consequence for the canvas: you can draw the edge and emit a correct `Ref`, but you cannot tell the user which value flows across it, and you cannot safely replicate the wiring by hand-patching `status.atProvider.id`.**

**Therefore: prefer the native `<f>Ref` over templating `status.atProvider.id`.** The ref is resolved by the provider controller with the correct extractor (which may be `arn`, not `id`). Reserve `status.atProvider` templating for values with no ref triad — and **never emit a `status.atProvider` path without a guard**, since they are absent on the first reconcile.

**Portability caveat.** 344 of the 353 CRDs analysed are upjet-generated. The `*Ref`/`*Selector` convention is an upjet/crossplane-runtime convention, **not a spec**. Both `canvas-ux.md` and `crd-schema-shape.md` independently flag this as the project's central risk. Build the inference as an explicit, testable, data-driven layer with per-provider overrides, and **prototype against a second non-upjet provider before committing.**

### 2.3 Schema size vs browser delivery

**Verdict: transfer size is a non-problem. Serve from the backend anyway, for structural reasons — not for bytes.**

The measurements, all 204 EC2 CRDs: [V]

| Tier | raw | gzip | brotli |
|---|---|---|---|
| Full `openAPIV3Schema` × 204 | 4,275,487 | 238,371 | **53,680** |
| Descriptions stripped | 1,226,557 | 37,479 | **15,219** |
| `spec.forProvider` only, no descriptions | 364,287 | 17,981 | **7,848** |
| Index only (group/kind/version/prop-count) | 15,215 | 1,332 | **1,022** |

An entire provider family compresses **~18:1** to 54 KB brotli, because CRD schemas contain **zero `$ref`** and are massively repetitive. `JSON.parse` of the full 4.27 MB costs **9.4 ms median** (5 runs: 11.7, 10.8, 9.4, 9.4, 9.3) retaining **6.1 MB** heap. [V] Per-CRD with descriptions stripped: max `LaunchTemplate` 34,083 B, median 4,565 B, min 1,421 B. [V]

**The acquisition side is even better than the delivery side.** Extracting a provider package touches only the small "base" layer, never the multi-hundred-megabyte runtime layer: [V-me]

```
$ ./xpkgget xpkg.upbound.io/upbound/provider-aws-sqs:v2 /tmp/verify-sqs.yaml
ref            = xpkg.upbound.io/upbound/provider-aws-sqs:v2
manifest type  = application/vnd.oci.image.index.v1+json
image digest   = sha256:1aff5a5aa39ec5c103782c098fe28a2774793e68c1419bc450a26c0a361e35f7
layers total   = 18
base layer     = sha256:04115f40bbaf016f4e530ef00fc2b7d2171061d71a1d4f243b1970985c44cc98 (config label io.crossplane.xpkg:<digest>=base)
base layer B   = 20071 compressed
stream bytes   = 182766 uncompressed
image bytes    = 271482755 (all layers, NOT downloaded)
```

**20,071 bytes downloaded out of a 271,482,755-byte image — a ~13,500:1 reduction — in 1.84 s wall, anonymous, no Docker, no cluster.** [V-me] This is the single best number in the whole research corpus.

**Why serve from the Go backend anyway** (the reasons are structural, not bandwidth):

1. **N providers, not one.** EC2 is one member of `provider-family-aws`. Bundling schemas means re-releasing the binary every time any provider publishes.
2. **The cluster is the authoritative source** when one is available — including the `.m.`/legacy split and per-CRD version differences a bundled snapshot would get wrong.
3. **Descriptions are 71% of the payload and you want them** — they are the field help text. Serve per-CRD on demand rather than choosing between bloat and dropping them.

**Recommended loading strategy** (all figures [V]):

1. **Eager at startup:** the index tier — group/kind/version/scope/property-count for every CRD, filtered to `.m.` groups. **1.0 KB brotli for 204 CRDs.**
2. **On demand per node type:** the single CRD's full schema on drop/form-open. Median **4.5 KB** raw. Cache in a `Map`; a session touches maybe 5–20 kinds.
3. **Never ship schemas in the JS bundle.**
4. **Turn on compression in Go.** ⚠️ `http.FileServer` does **not** compress — no `Content-Encoding` on any response in the verified harness. [V] With an 18:1 ratio this is the highest-leverage line of server code in the project.
5. **`ETag` + `If-None-Match` per CRD**, keyed on `metadata.resourceVersion`. Schemas change only on provider upgrade → nearly every later request is a 304.

**Rendering cost is also a non-problem, for a non-obvious reason.** rjsf defaults arrays to zero items, so anything nested beneath an array costs nothing until the user clicks "Add". Kyverno's `ClusterPolicy.spec` — 300 KB, depth 15, 1,445 properties — rendered in **19 ms** producing **10 inputs**. EC2's largest CRD, `LaunchTemplate` (263 props), renders in **45 ms** producing 88 inputs. [V] You get lazy rendering for free; do not build a virtualized form renderer pre-emptively. The one case needing intervention is 200+ *scalar* siblings at one level — **none exist in the EC2 provider.**

**UNRESOLVED — schema depth.** The two briefs that measured EC2 nesting depth disagree:

| Brief | EC2 max depth | Deepest path cited |
|---|---|---|
| `crd-schema-shape.md` [V] | **7** (11 across full AWS sample) | `.spec.forProvider.launchTemplateConfig[].launchTemplateSpecification.launchTemplateIdRef.policy.resolution` |
| `canvas-ux.md` [V] | **9** | `Fleet.spec.forProvider.launchTemplateConfig[].override[].instanceRequirements.acceleratorCount.max` |

Almost certainly a counting-convention difference (whether array `items` levels and the schema root count). **Not silently reconciled.** Both agree on the design consequence: the mass sits at depth 3–5 (histogram: `3: 13,284 · 4: 10,612 · 5: 6,701 · 6: 1,574 · 7: 1,592 · 8: 530 · 9: 513 · 10: 51 · 11: 54` [V]), so **design the form for depth 5 and provide a raw-YAML escape below that.** Don't build 11 levels of accordion.

**UNRESOLVED (minor) — SQS `Queue` schema size and node count.** `canvas-ux.md` measures the `openAPIV3Schema` at **17,161 bytes / 79 property nodes / max depth 4**; `prior-art.md` reports **17,652 bytes** for the live CRD and **17,829** for the CLI-generated JSON Schema; `crd-schema-shape.md` reports **86 total schema nodes** and 542 lines of YAML. Different measurement points (live CRD vs. package vs. generated) and different node-counting rules. Immaterial to design; recorded so nobody "fixes" one number to match another.

### 2.4 XRD v2 reserved and injected fields under `spec.crossplane`

**The mechanism is selective copying and overwrite — not rejection.** This is the single most misunderstood area, and the docs are wrong about it.

**Only two fields are hard-blocked by v2 admission CEL:** [V]

```yaml
x-kubernetes-validations:
- rule: "!has(self.claimNames)"
  message: "Claims aren't supported in apiextensions.crossplane.io/v2"
- rule: "!has(self.connectionSecretKeys)"
  message: "XR connection secrets aren't supported in apiextensions.crossplane.io/v2"
```

```
$ kubectl create --dry-run=server -f t17.yaml
The CompositeResourceDefinition "xtests.example.org" is invalid:
* spec: Invalid value: Claims aren't supported in apiextensions.crossplane.io/v2
* spec: Invalid value: XR connection secrets aren't supported in apiextensions.crossplane.io/v2
```

**Everything else that "shouldn't" be there is silently accepted, then either inert or rejected downstream by the derived CRD.**

**What `xcrd.genCrdVersion` actually copies off your schema** — a fixed, small set; everything else is dropped: [D — crossplane-runtime `pkg/xcrd/crd.go`, read from `main`, behaviour cross-checked against the live 2.4.0 cluster and matched exactly]

| Level | Copied from your schema | Everything else |
|---|---|---|
| root | `Description`, `XValidations` | **dropped** — incl. root `required` (replaced by `[spec]`), `oneOf`, `additionalProperties`, `x-kubernetes-preserve-unknown-fields` |
| `metadata` | only `properties.name.maxLength`, and only if **stricter** than 63 | **dropped** — `metadata.labels`/`annotations`/`generateName` schemas silently vanish |
| `apiVersion`, `kind` | untouched (always `{type: string}`) | your versions overwritten |
| `spec` | `Description`, `Required` (**appended**), `XPreserveUnknownFields`, `XValidations` (**appended**), `OneOf` (**appended**), `Properties` (`maps.Copy`) | **dropped** — `additionalProperties`, `anyOf`, `allOf`, `not`, `min/maxProperties`, `default`, `nullable`, `type` |
| `status` | `Description`, `Required` (**replaced**), `XValidations` (**replaced**), `OneOf` (**replaced**), `Properties` (`maps.Copy`) | dropped, incl. `XPreserveUnknownFields` |

Then Crossplane does `maps.Copy(spec.Properties, CompositeResourceSpecProps(...))` — **Crossplane wins every key collision.**

**The injected `spec.crossplane` block, exact v2.4 layout** (description: *"Configures how Crossplane will reconcile this composite resource"*): [V]

```yaml
spec:
  crossplane:
    compositionRef:              {name: string}                    required: [name]
    compositionRevisionRef:      {name: string}                    required: [name]
    compositionSelector:         {matchLabels: map[string]string}  required: [matchLabels]
    compositionRevisionSelector: {matchLabels: map[string]string}  required: [matchLabels]
    compositionUpdatePolicy:     string enum:[Automatic,Manual]  default: <spec.defaultCompositionUpdatePolicy>
    resourceRefs:                array, x-kubernetes-list-type: atomic
      items: {apiVersion, kind, name}  required: [apiVersion, kind]
  <your fields...>
status:
  conditions: array, x-kubernetes-list-type: map, x-kubernetes-list-map-keys: [type]
    items: {lastTransitionTime(date-time), message, observedGeneration(int64), reason, status, type}
    required: [lastTransitionTime, reason, status, type]
  <your fields...>
metadata:
  name: {type: string, maxLength: 63}
required: [spec]                        # injected at the schema root
subresources: {status: {}}              # always
```

**`Cluster` vs `Namespaced` — the only schema difference** is `spec.crossplane.resourceRefs[].namespace` (type string), present for `Cluster` and **absent** for `Namespaced`. [V] (Source comment: *"Namespaced XRs don't get to reference composed resources in other namespaces."*)

**Reservation semantics — the table that matters:** [V]

| Top-level property | Behaviour |
|---|---|
| `spec` | Your properties merged. `spec.crossplane` reserved. |
| `spec.crossplane` | **Silently replaced.** Not rejected. |
| `status` | Arbitrary `status.*` preserved (e.g. `status.url`, `status.phase`); `status.required` and `status.oneOf` honoured. |
| `status.conditions` | **Silently overwritten** with the canonical array. |
| `status.crossplane` | **NOT reserved in v2** — a user-defined `status.crossplane` survives verbatim. Docs claim it's disallowed; **false**. Still avoid it. |
| `metadata` | Accepted but **stripped down to `name.maxLength`**. |
| `apiVersion`, `kind` | Accepted, ignored. |
| root `required` | Accepted, **replaced by `[spec]`**. |
| root `x-kubernetes-validations` | **Preserved.** |

> **Docs discrepancy — flagged.** The docs say Crossplane "doesn't allow" fields under `spec.crossplane`, `status.crossplane`, and `status.conditions`. Empirically **nothing is rejected**: the first and third are overwritten, the second is kept. A form builder should treat `spec.crossplane` and `status.conditions` as reserved names in the UI and **warn, not error**, rather than relying on server rejection.

**Also injected/derived:** `names.categories` gets `composite` appended; XR labels `crossplane.io/composite: <xr-name>`; XR finalizer `composite.apiextensions.crossplane.io`; XRD finalizer `defined.apiextensions.crossplane.io`; the CRD gets an ownerReference to the XRD. CRD **labels** = XRD `metadata.labels` ∪ `spec.metadata.labels`; CRD **annotations** = `spec.metadata.annotations` **only** (the XRD's own annotations are not propagated). [V]

**`additionalPrinterColumns` — your columns first, then five appended:** [V]

| name | type | jsonPath | priority |
|---|---|---|---|
| `SYNCED` | string | `.status.conditions[?(@.type=='Synced')].status` | 0 |
| `READY` | string | `.status.conditions[?(@.type=='Ready')].status` | 0 |
| `COMPOSITION` | string | `.spec.crossplane.compositionRef.name` | 0 |
| `COMPOSITIONREVISION` | string | `.spec.crossplane.compositionRevisionRef.name` | **1** |
| `AGE` | date | `.metadata.creationTimestamp` | 0 |

**The GitOps drift trap.** v1 is still the *storage* version (v2 is served but not stored, `conversion.strategy: None`), so a v2 XRD is persisted through the v1 schema and **v1's defaults are applied and read back**: [V]

```yaml
spec:
  defaultCompositeDeletePolicy: Background     # ← injected, inert in v2
  defaultCompositionUpdatePolicy: Automatic    # ← injected
  scope: Namespaced                            # ← v2 default
```

Emitted YAML will never match the server object unless the generator also emits these. **Emit `defaultCompositionUpdatePolicy: Automatic` explicitly**; either emit `defaultCompositeDeletePolicy: Background` too or accept a permanent diff.

**The `scope` landmine.** `LegacyCluster` is **not valid** under `apiextensions.crossplane.io/v2` on 2.4.0: [V]

```
$ kubectl explain xrd.spec.scope --api-version=apiextensions.crossplane.io/v2
FIELD: scope <string>
ENUM:
    Namespaced
    Cluster

$ kubectl create --dry-run=server -f t1.yaml     # scope: LegacyCluster, apiVersion v2
* spec.scope: Unsupported value: "LegacyCluster": supported values: "Namespaced", "Cluster"
```

Docs at <https://docs.crossplane.io/latest/composition/composite-resource-definitions/> claim three values. **That is false for the v2 API version.** LegacyCluster is a **v1 emitter mode**, not a v2 scope option.

Worse, the defaults diverge between server and CLI. `crossplane xrd convert` parses everything through the **v1 Go types regardless of the file's `apiVersion`**, and `ptr.Deref(xrd.Spec.Scope, LegacyCluster)` means an omitted `scope` renders a **LegacyCluster** CRD offline while the same file applied to the cluster renders a **Namespaced** one. [V] **Always emit `scope:` explicitly.**

**Client-side validation checklist — none of this is caught by XRD admission:** [V]

1. `metadata.name == names.plural + "." + group` — enforced only on the derived CRD; the XRD itself applies cleanly and fails as a missing `Established` condition
2. exactly one `versions[].referenceable == true`, and that version has `served: true` (the "referenceable must be served" half is **documented but not enforced**)
3. every `versions[].name` matches `[a-z]([-a-z0-9]*[a-z0-9])?`
4. every `shortNames[]` / `categories[]` matches the same DNS-1035 regex
5. every `additionalPrinterColumns[].type ∈ {boolean, date, integer, number, string}`
6. every schema property has an explicit `type`
7. no `$ref`; no `uniqueItems: true`; no `additionalProperties` alongside `properties`
8. `spec.crossplane` and `status.conditions` are reserved names
9. `versions[].schema.openAPIV3Schema` always present (omitting passes admission; the controller then errors `custom resource validation cannot be nil`)
10. `versions[].subresources` accepts only `scale` — writing `status` is a strict-decoding error

**Structural-schema rules that *do* reject, on the derived CRD:** [V]

| Construct | Verdict |
|---|---|
| property with no `type` | `properties[noType].type: Required value: must not be empty for specified object fields` |
| `$ref: "#/definitions/Foo"` | `$ref is not supported` |
| `additionalProperties` with `properties` (incl. `false`) | `additionalProperties and properties are mutual exclusive` |
| `uniqueItems: true` | `Forbidden: uniqueItems cannot be set to true since the runtime complexity becomes quadratic` |
| `default` violating its `format` | `default: Invalid value: "abc": in body must be of type email` |
| `x-kubernetes-validations[].fieldPath: "."` | `must be a valid path` (use `.region`) |

**Error reporting is staged** — the `$ref`/`additionalProperties` class suppresses the structural pass, so a schema with both problem kinds reports only the first batch. **The builder must run its own complete validation rather than round-tripping to the apiserver once.**

**`spec.conversion`: omit it entirely unless a real webhook exists.** The **v1** XRD schema carries a mis-written CEL rule that makes *any* conversion block fail, including `strategy: None`. v2 has no such rule, but omitting is free and safe. [V] Crossplane does not host a conversion webhook for XRs.

**The `X` prefix is conventional, not required.** `kind: Queue`, `plural: queues`, `name: queues.example.org` passes admission *and* derives a valid CRD. [V] In v2 (no claims) the historical reason for the X is gone. **Offer it as a default, not a constraint.**

### 2.5 Parsing existing go-templates back into a node graph — honest verdict

**Verdict: not feasible. Do not build it.** This is the most decisively-answered question in the corpus, and it was answered by *actually parsing the user's production template* with Go's `text/template/parse`. [V] (Source at `…/scratchpad/tmplexp/main.go`.)

**First result — parsing fails outright by default:**

```
PARSE ERROR: template: x:9: function "dict" not defined
```

`template.Parse` resolves function names at parse time. To parse third-party templates you must either replicate function-go-templating's entire FuncMap (all 209 Sprig functions plus 11 custom) or use `parse.SkipFuncCheck`. With `parse.New` + `Mode = parse.SkipFuncCheck | parse.ParseComments`:

```
ACTION    $spec := .observed.composite.resource.spec
TEXT      "apiVersion: sqs.aws.m.upbound.io/v1beta1\nkind: Queue\nmetadata:\n  an…"
ACTION    setResourceNameAnnotation "main-queue"
TEXT      "spec:\n  forProvider:\n    region:"
ACTION    index (dict "EU" "eu-north-1" "US" "us-east-2") $spec.location
WITH      $spec.visibilityTimeoutSeconds
  TEXT      "visibilityTimeoutSeconds:"
  ACTION    .
WITH      $spec.maxMessageSize
  TEXT      "maxMessageSize:"
  ACTION    .
WITH      $spec.tags
  TEXT      "tags:"
  RANGE     $key, $value := .
    ACTION    $key
    TEXT      ":"
    ACTION    $value | quote
TEXT      "providerConfigRef:\n    # Namespaced managed resources may reference e…"
ACTION    $spec.providerName
```

**Four reasons this cannot become a graph:**

1. **The TEXT nodes are not YAML.** `"spec:\n  forProvider:\n    region:"` is a dangling mapping key. You cannot hand any fragment to a YAML parser. The two grammars are **interleaved, not nested**, so no composition of `text/template` + `gopkg.in/yaml.v3` yields a document tree.
2. **The document's shape is data-dependent.** Three `WITH` nodes mean `spec.forProvider` has 2³ possible key sets. "The graph" does not exist until you bind an XR — but a canvas node must represent *one* structure.
3. **Indentation is semantic and lives in TEXT.** `{{- with }}` / `{{- end }}` chomping decides whether the emitted YAML is valid. Any graph→template regeneration must reproduce whitespace exactly or silently corrupt a working Composition.
4. **Comments are lost in the direction that matters.** YAML comments in the *XRD* (the repo's real XRD documents why `enum` beats `oneOf` for kubernetes-ingestor form generation) are destroyed by a parse→model→re-emit cycle. On a `selfHeal: true` repo that is a one-way loss of institutional knowledge inside a PR diff.

**The three import tiers — ship 0 and 1, never 2:**

- **Tier 0 — `generate` only (default).** Blueprint → artifacts. One direction.
- **Tier 1 — `adopt` (ship this).** Read an existing Composition + XRD; map everything *structured* into the blueprint (`metadata.name`, `compositeTypeRef`, pipeline steps, `functionRef` names, `input.apiVersion`, the whole XRD `spec`); capture each go-templating step's template into `rawTemplate:` **as an opaque verbatim string**. Losslessly byte-reproducible. The canvas shows a "custom template" node that opens a text editor. **This is the honest 90% of the value at ~5% of the risk.**
- **Tier 2 — AST → graph. Do not build.** Only tractable for templates your own tool emitted — which you can detect far more cheaply with a provenance marker.
- **Tier 2.5 — `render`-based visualization (nice-to-have).** Run `crossplane composition render` against a sample XR and build *display* nodes from the concrete output. Verified working: 1.0 s warm, yielding a real `Queue` with `region: eu-north-1` resolved from the `dict` lookup. Recovers topology, discards conditionals. **Must be read-only — never let it write back.**

Corroborating: **every** comparable system is one-directional — Terraform (HCL → opaque plan), cdk8s, Backstage scaffolder templates, Kratix Promise, Crossplane Project. [V/D] That is not an accident.

**Corollary — declare round-tripping a non-goal in v1 and say so loudly.** Use `eemeli/yaml`'s AST with source positions (not `js-yaml`) so comments are retained and surgical edits stay possible later.

### 2.6 Other constraints that will break the design if ignored

**`<no value>` — the silent killer.** Default Go template `missingkey` behaviour on `map[string]any` substitutes the literal text `<no value>` **at any depth, never erroring**: [V]

| Expression | Result |
|---|---|
| `{{ .observed.composite.resource.spec.doesNotExist }}` | `<no value>` |
| `{{ .observed.composite.resource.spec.a.b.c }}` (no `a` at all) | `<no value>` — no nil-pointer error |
| `{{ if .observed…spec.missing }}Y{{else}}N{{end}}` | `N` — safe |
| `{{ .observed…spec.missing \| default "fallback" }}` | `fallback` — safe |
| `{{ dig "spec" "missing" "digfallback" .observed.composite.resource }}` | `digfallback` — safe |
| `{{- with .observed…spec.missing }}SET{{end}}` | *(empty)* — safe |

**`<no value>` is a legal string, so no schema check can see it.** A full validate → render → validate pipeline passes with **exit 0** on `name: <no value>`. [V] The *only* catch is `grep`. Mitigations in order: (a) `options: ["missingkey=error"]` at the input top level; (b) wrap every optional field in `{{- with }}`; (c) `| default`; (d) mark every dereferenced field `required` in the generated XRD; (e) ship a `grep -rn '<no value>\|<nil>'` guard in the generated Makefile. Note `providerName: ""` renders as `name: null` and **is** caught — the empty-string case is safe, the missing-key case is not.

**YAML type coercion on unquoted scalars.** [V]

| XR spec (all strings) | Unquoted | With `\| quote` |
|---|---|---|
| `versionish: "1.10"` | `1.1` ← **data loss** | `"1.10"` |
| `onish: "on"` | `true` | `"on"` |
| `yesish: "yes"` | `true` | `"yes"` |
| `nullish: "null"` | `null` | `"null"` |
| `emptyish: ""` | `null` | `""` |
| `delaySeconds: 5` (int) | `5` (correct) | `"5"` ← **wrong type** |

The decoder follows YAML 1.1 booleans. **Rule: emit `| quote` iff the schema type is `string`; emit bare for `integer`, `number`, `boolean`.** Getting this backwards fails provider CRD validation in one direction and silently corrupts values in the other.

**Non-string annotation values are FATAL, not coerced:** [V]

```
invalid annotations in resource 'sqs.aws.m.upbound.io/v1beta1, Kind=Queue resource-name=q':
.metadata.annotations accessor error: contains non-string value in the map under key "my-count":
5 is of the type int64, expected string
```

**Every templated annotation and label value needs `| quote`, unconditionally.**

**`number` outnumbers `integer` 3.5:1.** Upjet emits `type: number` for integral fields (`delaySeconds`, `maxMessageSize`) — 726 `number` vs 204 `integer` in EC2. [V] A form rendering `number` as a float produces `30.0` where AWS wants `30`. **Coerce, or always emit unquoted integers.**

**Indentation: `nindent`, never `indent`.** `{{ toYaml .spec.tags | indent 6 }}` produces broken YAML and a fatal decode error, because `indent N` prefixes *every* line including the first, which already carries the template's literal indentation. [V] **Always `{{- toYaml X | nindent N }}`, N = parent key depth + 2.**

**`{{- with }}` is the only safe emitter for object-typed fields** — it solves nil-safety, rebinding, and whitespace at once. Caveat inherited from Go: it treats `0`, `false`, `""` as absent, so for a genuinely optional integer that may legitimately be `0`, use `{{ if hasKey .spec "field" }}`. And `with` rebinds `.`, so reach outer scope with `$`.

**`randomChoice` is not idempotent** — seeded from `time.Now().UnixNano()` per call. Using it for a value landing in a managed resource causes an **infinite update loop**. [V]

**No server-side validation of function input exists.** The GoTemplate input CRD is generated but **never installed** (`input.go`: "we never install its CRD"), so all input errors surface only at reconcile/render time. Consequently the CEL rule `Exactly one of 'template' or 'templates' must be set` **never runs** — with both set, `template` silently wins. [V] **The generator must validate its own output.**

**`CompositeConnectionDetails` is a dead end on a v2 stack.** The function still emits `desired.composite.connectionDetails`, but the proto states plainly that for modern XRs *"this will be ignored."* [V/D] **Compose an explicit `v1.Secret` instead.** Same for `ClaimConditions` with a claim-less XRD: `target: CompositeAndClaim` degrades to `Composite`.

**Extra resources arrive on the *second* invocation.** A template that declares a requirement and dereferences `.extraResources` unguarded fails on the first pass: [V]

```
cannot execute template: template: manifests:18:20: executing "manifests" at
<index .extraResources "settings">: error calling index: index of untyped nil
```

**Every read must be guarded.** Prefer `getExtraResources` — it tries `requiredResources[%s].items` then `extraResources[%s].items` and is version-agnostic by construction.

**`tagsAll` leaks into `spec` and causes drift.** Normally status-only, it appears in `spec.forProvider` on nested EC2 block devices (`.ebsBlockDevice[].tagsAll` ×6, `.rootBlockDevice[].tagsAll` ×2, `.rootBlockDevice.tagsAll` ×4, `.initProvider.*.tagsAll` ×12). These are provider-computed; setting them causes drift. **Suppress `tagsAll` from any spec-side form.** [V]

**`initProvider` duplicates `forProvider` minus `region` in 101/102 EC2 CRDs.** [V] Render it as a **mode toggle on the existing form**, never a second field tree.

**Render is Docker-mandatory — the engine itself is a container.** Not just the functions: [V]

```
distracted_nightingale | xpkg.crossplane.io/crossplane/crossplane:stable | 8080/tcp
angry_lehmann          | .../function-go-templating:v0.12.0             | 9443/tcp
lucid_haslett          | .../function-auto-ready:v0.5.0                 | 9443/tcp
```

A Docker **network** is created per render, so restricted/rootless runners are out. `--crossplane-binary` wants the **core server** binary (which has a hidden `internal render`), not the CLI — passing the CLI gives `error: unexpected argument internal : exit status 80`. [V] The image ships linux-only; whether a darwin core build works is **[U]**. **Split CI: `validate` needs no Docker; `render` does.**

**Render leaks containers** — after a handful of runs, orphaned `function-*` containers stay `Up`. In CI set `render.crossplane.io/runtime-docker-name` or reap in `t.Cleanup`. [V]

**ArgoCD: three things never to emit.** [V]
- **Never `argocd.argoproj.io/tracking-id`** — ArgoCD injects it at apply time under `resourceTrackingMethod: annotation`; a wrong app-name prefix makes ArgoCD believe another Application owns the resource. (Its namespace segment is `crossplane-system` even for cluster-scoped objects, reflecting the Application's `destination.namespace` — another reason never to synthesize it.)
- **Never a `kustomization.yaml` by default.** ArgoCD auto-detects source type from repo content. Dropping one in flips these Applications from Directory to Kustomize, and then **any file absent from `resources:` becomes invisible — which under `prune: true` means ArgoCD deletes the corresponding live object.** *(Auto-detection is [V] from `status.sourceType`; the specific Kustomize-under-`recurse` interaction is reasoned, **[U]** — test in a scratch repo before shipping `--layout kustomize`.)*
- **Never a generated-at annotation.** It changes every run; under `selfHeal: true` + `prune: true` that is a perpetual sync loop. **Put provenance in YAML comments** — discarded at parse time, zero diff.

**Determinism is a correctness requirement, not a nicety.** Sorted keys, stable field order, LF only, trailing newline, no version stamps, **trailing whitespace stripped from every template line** (it changes YAML block-scalar round-tripping and produces phantom ArgoCD diffs). Under `selfHeal: true` any nondeterminism becomes live-cluster churn.

**`source: Inline` is the only viable template source.** `FileSystem` reads from the *function pod's* filesystem, requiring a custom function image or a Crossplane Project build — incompatible with a plain ArgoCD directory sync. [V/D]

**`functionRef.name` must be resolved, never guessed.** `crossplane composition generate` emits `crossplane-contrib-function-auto-ready` while the installed Function objects are named `function-auto-ready` and `function-go-templating` — applying the generated file dangles the ref. [V] **Resolve against live `Function` objects, or take it as explicit blueprint input.**

**Never auto-pluralize.** The Crossplane CLI added `--plural` precisely because it breaks — its own help cites `postgres` → `postgreses`. [D] Require it; default to `lower(kind)+"s"` only in the interactive UI where a human can correct it.

**The tool must work in a plain directory.** Both `crossplane xrd generate` and `crossplane composition generate` hard-require `crossplane-project.yaml`. A GitOps repo is not and should not become a Crossplane Project. **Make project-awareness opt-in.** [V]

**AJV strict mode is a hard blocker for the GUI.** [V]

```
sqs-queue.json (17,631 B)  strict=true : FAILED -> strict mode: unknown keyword: "x-kubernetes-map-type"
sqs-queue.json            strict=false: COMPILED ok in 10ms
big.json (320,182 B)      strict=true : FAILED -> strict mode: unknown keyword: "x-kubernetes-map-type"
big.json                  strict=false: COMPILED ok in 111ms
```

Plus silent degradation in **both** modes: `unknown format "date-time" ignored`, `"int64" ignored`, `"int32" ignored`. **Run `strict: false` and add custom int64/int32 range validation yourself.**

---

## 3. Verified reference material

### 3.1 The canonical MR spec shape

**v2 namespaced** (`queues.sqs.aws.m.upbound.io`, scope `Namespaced`): [V]

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

**v1 / cluster-scoped legacy** (`queues.sqs.aws.upbound.io`, scope `Cluster`): [V]

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

### 3.2 OpenAPI keyword census — 282 AWS m-CRDs, 39,170 nodes [V]

| keyword | nodes | % | verdict |
|---|---|---|---|
| `description` | 32,997 | 84.2% | **must support** |
| `enum` | 4,708 | 12.02% | **all boilerplate — 0 real** |
| `default` | 2,857 | 7.29% | must support |
| `additionalProperties` | 1,835 | 4.68% | **must support** (maps) |
| `x-kubernetes-list-type` | 1,065 | 2.72% | should support (`set` → chips) |
| `format` | 889 | 2.27% | ignore (`date-time` 297, `int64` 592 — all status boilerplate) |
| `x-kubernetes-map-type` | 819 | 2.09% | always `granular`; ignore |
| `x-kubernetes-list-map-keys` | 296 | 0.76% | only `status.conditions` → `["type"]`; ignore |
| `x-kubernetes-validations` | 131 | 0.33% | **must support — this is "required"** |
| `pattern` | **0** | 0% | skip |
| `minimum` / `maximum` | **0** / **0** | 0% | skip |
| `minLength`/`maxLength`/`minItems`/`maxItems`/`minProperties`/`maxProperties` | **0** | 0% | skip |
| `multipleOf` / `uniqueItems` | **0** | 0% | skip |
| `oneOf` / `anyOf` / `allOf` / `not` | **0** | 0% | skip |
| `$ref` | **0** | 0% | skip |
| `x-kubernetes-preserve-unknown-fields` | **0** | 0% | **needed for other providers** |
| `x-kubernetes-int-or-string` | **0** | 0% | skip |
| `nullable` | **0** | 0% | skip |

GCP + Azure (62 CRDs, 8,763 nodes) reproduce this **exactly** — same zeros, 0 real enums, CEL 0.56%. [V]

Types: `string` 7,166 · `object` 3,513 · `boolean` 847 · `array` 731 · `number` 726 · `integer` 204. Only **106 of 205** arrays in `spec.forProvider` carry `x-kubernetes-list-type`; the other 99 default to `atomic`. [V]

**Scoping note.** The zeros above are an **upjet** property, not a Crossplane one. Across all 81 CRDs on the test cluster (110 versions, incl. ArgoCD/Kyverno/cert-manager/DeploymentRuntimeConfig), the picture inverts: `list-type` 2562, `map-type` 933, `preserve-unknown-fields` 705, `int-or-string` 354, `list-map-keys` 274, CEL `validations` 62, `embedded-resource` 5; `enum` 639, `anyOf` 354, `oneOf` 107; max schema 376,061 B, p90 81,264, median 17,819; **max nesting depth 34**. **46 of 81 (57%)** contain at least one form-hostile construct. [V] Non-upjet `provider-kubernetes` carries a real enum (`readiness.policy`) and a real CEL business rule. [V] **Build for hand-written providers; treat upjet's flatness as the common case, not the only case.**

### 3.3 Requiredness — the three mechanisms

**(a) `required` arrays** — `spec.forProvider.required` distribution: [V]

| provider set | value | count |
|---|---|---|
| AWS (ec2/iam/rds/s3/eks) | `["region"]` | 268 |
| AWS | `[]` | 25 |
| AWS | `["key","region","resourceId"]` | 2 |
| AWS | `["policyArn","region"]` | 1 |
| GCP storage | `[]` | 14 |
| Azure storage | `[]` | 17 |

`spec.required` is always `["forProvider"]`. `spec.initProvider.required` is empty in **364/365** m-CRDs (lone exception: `ServerCertificate.privateKeySecretRef`). [V]

**(b) CEL on `.spec` — the real required fields.** All 188 AWS + 70 GCP/Azure rules use **one** message template. Full rule from `accounts.storage.azure.m.upbound.io`: [V]

```
message: "spec.forProvider.accountReplicationType is a required parameter"
rule: "!('*' in self.managementPolicies || 'Create' in self.managementPolicies
       || 'Update' in self.managementPolicies)
       || has(self.forProvider.accountReplicationType)
       || (has(self.initProvider) && has(self.initProvider.accountReplicationType))"
```

Semantics a form must reproduce: required **only when** `managementPolicies` includes `*`, `Create`, or `Update`, and satisfied by **either** `forProvider` **or** `initProvider`.

Worked examples: [V]

| Resource | `forProvider.required` | CEL-required |
|---|---|---|
| `Queue` (sqs) | `["region"]` | *(none)* |
| `AMI` (ec2) | `["region"]` | `name` |
| `Instance` (rds) | `["region"]` | `instanceClass` |
| `Bucket` (gcp storage) | *(none)* | `location` |
| `Account` (azure storage) | *(none)* | `accountReplicationType`, `accountTier`, `location` |

**(c) Nested `required`** — 176 entries in the EC2 forProvider subtree, and **every single one is on a reference or secret object**, never a user-facing data field. [V] Treat nested `required` as belonging to the ref/secret widget.

**Resolver:**

```
required(path) =
     path ∈ parent.required                              # 'region' on AWS; ref .name
  OR ∃ CEL rule on .spec whose message ==
       "spec.forProvider.<path> is a required parameter"
       AND managementPolicies ∩ {*, Create, Update} ≠ ∅
       AND not satisfied by initProvider
```

### 3.4 Field-kind taxonomy for the code generator

Classify each node in this order; **first match wins**.

**Tier 0 — structural, never a form field**

| # | Kind | Detection | Behaviour |
|---|---|---|---|
| 0.1 | `ROOT_SCAFFOLD` | `.apiVersion`, `.kind`, `.metadata` | Emit from CRD `group`/`version`/`names.kind`; never render |
| 0.2 | `ENVELOPE` | direct children of `.spec` other than `forProvider`/`initProvider` | Shared "Resource options" panel, generated *from its own schema* |
| 0.3 | `INIT_MIRROR` | `.spec.initProvider.*` | Suppress subtree; per-field "apply at creation only" toggle |
| 0.4 | `STATUS_OUTPUT` | `.status.atProvider.*` | Output **ports** on the canvas. Pin `id`, `arn`; guard every template ref |
| 0.5 | `STATUS_META` | `.status.conditions`, `.status.observedGeneration` | Node badges from `additionalPrinterColumns` |
| 0.6 | `COMPUTED_LEAK` | name is `tagsAll` anywhere under `.spec` | **Suppress** |

**Tier 1 — reference & secret widgets (check before scalars)**

| # | Kind | Detection | Widget |
|---|---|---|---|
| 1.1 | `SECRET_REF` | ends `SecretRef`, `properties ⊇ {name, key}` | Secret picker. **Never a text input** |
| 1.2 | `WRITE_ONLY_SECRET_REF` | ends `WoSecretRef` | Same, labelled write-only |
| 1.3 | `XRESOURCE_REF` | ends `Ref`, object, has `name`, not 1.1/1.2, not `matchControllerRef` | Hidden from plain form; **this is the canvas edge** |
| 1.4 | `XRESOURCE_REF_LIST` | ends `Refs`, array | Multi-edge. Description off the **array** node |
| 1.5 | `XRESOURCE_SELECTOR` | ends `Selector`, has `matchLabels` ∧ `matchControllerRef` | Label-selector widget |
| 1.6 | `REF_TARGET_VALUE` | scalar named as the `populate` target of a 1.3/1.4 sibling | 3-way: literal / reference / selector |
| 1.7 | `REF_POLICY` | `.policy.{resolution,resolve}` | Advanced-only |

**Tier 2 — data fields**

| # | Kind | Detection | Widget |
|---|---|---|---|
| 2.1 | `ENUM_SCALAR` | non-boilerplate `enum` | Select. **Expect zero from MRs; common from XRDs** |
| 2.2 | `PROSE_ENUM` | string, no `enum`, description matches `Valid values are\|Possible values are\|must be one of\|Defaults to` + comma list | Combobox, free text allowed (127 in AWS sample) |
| 2.3 | `OPAQUE_JSON` | string ∧ description matches `\bJSON\b\|policy document` | Code editor, JSON mode |
| 2.4 | `PLACEMENT` | name ∈ {`region`,`location`} at `forProvider` root | Region picker |
| 2.5 | `MAP_TAGS` | object ∧ `additionalProperties.type == "string"` ∧ name ∈ {`tags`,`labels`} | Tag editor; safe to auto-inject (granular) |
| 2.6 | `MAP_STRING` | as above, other name | Key/value rows |
| 2.7 | `ARRAY_OF_OBJECT` | array ∧ `items.type == "object"` | Repeatable cards (318 in EC2) |
| 2.8 | `ARRAY_SET` | array ∧ scalar items ∧ `list-type: set` | Chip input |
| 2.9 | `ARRAY_ATOMIC` | array ∧ scalar items ∧ no list-type | Ordered list (99/205 in AWS) |
| 2.10 | `OBJECT_BLOCK` | object with `properties` | Collapsible; collapse below depth 3 |
| 2.11 | `SCALAR_INT` | `integer`, **or** `number` with no fractional semantics | **Emit unquoted, no decimal point** |
| 2.12 | `SCALAR_NUM` | genuinely fractional `number` | Number input |
| 2.13 | `SCALAR_BOOL` | boolean | Tri-state (unset ≠ false) |
| 2.14 | `SCALAR_STRING` | nothing above matched | Text input — 3,493 in AWS `forProvider` |
| 2.15 | `EMBEDDED_MANIFEST` | `x-kubernetes-preserve-unknown-fields: true` | **No form is possible** — raw YAML editor |
| 2.16 | `UNKNOWN` | anything else | Raw YAML escape hatch. Always provide one |

**Tags conventions per family** — shape universal, name and placement are not: [V]

| family | spec field | shape | status companion |
|---|---|---|---|
| AWS | `spec.forProvider.tags` (382) | `object` + `additionalProperties:{type:string}` + `x-kubernetes-map-type: granular` | `tags` (193) **and** `tagsAll` (178) |
| Azure | `spec.forProvider.tags` (6) | identical | `tags` (3) |
| GCP | `spec.forProvider.labels` (6) | identical | `labels` (3) |

Every tags/labels node in all 344 CRDs is the same `map[string]string` with `granular` — **zero variation**. [V]

### 3.5 `function-go-templating` v0.12.0 — complete reference

**Provenance:** tag `v0.12.0`, commit `e249e8cb4e7ae5d58043358f6573c92975874605`, released 2026-03-22. Image `xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.12.0`. Installed revision `function-go-templating-677316af26e4`, `revision: 1`, `State: Active`. `status.capabilities: [composition]`. gRPC endpoint `dns:///function-go-templating.crossplane-system:9443`. Deps: `sprig/v3 v3.3.0`, `function-sdk-go v0.6.2`, `crossplane-runtime/v2 v2.2.0`, `gopkg.in/yaml.v3 v3.0.1`, `go 1.25.6`. [V]

**Latest is v0.12.4 (2026-08-25);** release notes for v0.12.1–v0.12.4 are **exclusively** Go-runtime and dependency CVE remediation — no functional, schema, or template-function changes. Targeting v0.12.0 semantics is safe against v0.12.4. [D] Both `xpkg.crossplane.io` and `xpkg.upbound.io` serve the identical package (`docker manifest inspect` succeeds for both). [V]

#### Input schema — Go types (`input/v1beta1/input.go`)

```go
type GoTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Delims      *Delims                    `json:"delims,omitempty"`
	Source      TemplateSource             `json:"source"`                // REQUIRED
	Inline      *TemplateSourceInline      `json:"inline,omitempty"`
	FileSystem  *TemplateSourceFileSystem  `json:"fileSystem,omitempty"`
	Environment *TemplateSourceEnvironment `json:"environment,omitempty"`
	Options     *[]string                  `json:"options,omitempty"`
}

type TemplateSourceInline      struct { Template string `json:"template,omitempty"`; Templates []string `json:"templates,omitempty"` }
type TemplateSourceFileSystem  struct { DirPath string `json:"dirPath,omitempty"` }
type TemplateSourceEnvironment struct { Key string `json:"key,omitempty"` }
type Delims struct { Left *string `json:"left,omitempty"`; Right *string `json:"right,omitempty"` }
```

`TemplateSource` constants: `"Inline"`, `"FileSystem"`, `"Environment"`. **There are no others.**

#### Canonical YAML — the form to generate

```yaml
apiVersion: apiextensions.crossplane.io/v1     # Composition API is still v1 in Crossplane 2.4.0
kind: Composition
spec:
  mode: Pipeline
  pipeline:
  - step: render-templates
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline                  # Inline | FileSystem | Environment
      options:                        # OPTIONAL, TOP-LEVEL (not under inline!)
      - missingkey=error
      delims:                         # OPTIONAL — both halves or silently ignored
        left: "[["
        right: "]]"
      inline:
        template: |
          <go template text, '---'-separated documents>
```

#### Verified input-schema behaviours [V]

| Behaviour | Evidence |
|---|---|
| Input unmarshalled **strictly**; unknown field is fatal | `cannot get function input *v1beta1.GoTemplate from *v1.RunFunctionRequest: ... json: unable to unmarshal Go value of type v1beta1.GoTemplate: unknown name "bogusField"` |
| `options` under `inline` is fatal (**README is wrong**) | `... unknown name "options"` against `v1beta1.TemplateSourceInline` |
| Bogus template option is caught, not a crash | `cannot apply template options: panic occurred while applying template options: unrecognized option: not-a-real-option` |
| `options: ["missingkey=error"]` works | `cannot execute template: template: manifests:4:24: executing "manifests" at <.observed.composite.resource.spec.a.b.c>: map has no entry for key "a"` |
| `template` **and** `templates` both set → `template` silently wins | CEL guard exists only in the never-installed CRD |
| Neither set | `inline.template or inline.templates should be provided` |
| Missing / unknown `source` | `source is required` / `invalid source: %s` |
| `FileSystem` with nonexistent dir | `invalid function input: cannot read tmpl from the folder {/templates}: open /templates: no such file or directory` |
| `Environment` when context key absent | `invalid function input: cannot read tmpl from the environment: apiextensions.crossplane.io/environment key does not exist in context` |
| `Environment` end-to-end | via `--context-values='apiextensions.crossplane.io/environment={"myTemplate":"..."}'` → `fromEnvTemplate: eu-north-1` |

`FileSystem` traversal walks `dirPath` recursively, **skips hidden dirs and files** (leading `.`), concatenates every remaining file and appends `"\n---\n"`. No extension filter.

**Undocumented CLI escape hatch** (`main.go`): `--default-source` / `FUNCTION_GO_TEMPLATING_DEFAULT_SOURCE`. Other flags: `--debug/-d`, `--network` (default `tcp`), `--address` (default `:9443`), `--tls-certs-dir` (`TLS_SERVER_CERTS_DIR`), `--insecure`, `--max-recv-message-size` (default 4 MB).

#### The complete custom template-function list — 11 functions, exact Go signatures

From `function_maps.go` `getFunctions()` plus `initInclude`. [V]

| Template name | Go signature | Returns / notes |
|---|---|---|
| `randomChoice` | `randomChoice(choices ...string) string` | Uniform pick. **Non-deterministic per reconcile** |
| `toYaml` | `toYaml(val any) (string, error)` | `yaml.Marshal` (gopkg.in/yaml.v3). Trailing newline included |
| `fromYaml` | `fromYaml(val string) (any, error)` | `yaml.Unmarshal` into `any` |
| `getResourceCondition` | `getResourceCondition(ct string, res map[string]any) xpv1.Condition` | Returns a **Go struct** — capitalized fields |
| `setResourceNameAnnotation` | `setResourceNameAnnotation(name string) string` | Returns the literal line `gotemplating.fn.crossplane.io/composition-resource-name: <name>` — a whole YAML **line**, not a value |
| `getComposedResource` | `getComposedResource(req map[string]any, name string) map[string]any` | `nil` if absent |
| `getCompositeResource` | `getCompositeResource(req map[string]any) map[string]any` | `nil` on error |
| `getExtraResources` | `getExtraResources(req map[string]any, name string) []any` | `nil` if absent |
| `getExtraResourcesFromContext` | `getExtraResourcesFromContext(req map[string]any, name string) []any` | Reads the **context** key, not the request |
| `getCredentialData` | `getCredentialData(mReq map[string]any, credName string) map[string][]byte` | Base64-**decoded** bytes. **Omitted from the README table** |
| `include` | `include(name string, data any) (string, error)` | Renders a `define`d template to a string. `recursionMaxNums = 1000` |

**Verified return values**, one render against a mocked observed `Queue` named `the-queue`: [V]

```
a_getComposedResource:         arn:aws:sqs:eu-north-1:123456789012:my-queue
b_getComposedResource_missing: NIL          # nil → falsy, safe for {{ if }}
c_getCompositeResource:        eu-north-1
d_cond_status:                 True         # (getResourceCondition "Ready" (getComposedResource . "the-queue")).Status
e_cond_via_observed:           Available    # .Reason, arg = (index .observed.resources "the-queue")
f_cond_missing:                Unknown      # missing condition type → Status "Unknown", never an error
g_cond_type:                   Ready        # .Type
h_randomChoice:                only
i_fromYaml:                    42
m_include:                     {"a":"eu-north-1","b":5}
n_setResourceNameAnnotation:   gotemplating.fn.crossplane.io/composition-resource-name: demo
```

**`getResourceCondition` returns `xpv1.Condition`, a Go struct — fields are PascalCase, not the JSON names.** From `crossplane-runtime/v2@v2.2.0/apis/common/condition.go`:

```go
type Condition struct {
	Type               ConditionType          // .Type
	Status             corev1.ConditionStatus // .Status  → "True"/"False"/"Unknown"
	LastTransitionTime metav1.Time            // .LastTransitionTime
	Reason             ConditionReason        // .Reason
	Message            string                 // .Message
	ObservedGeneration int64                  // .ObservedGeneration
}
```

`{{ .status }}` (lowercase) yields nothing. **A generator emitting condition checks must use `.Status`.** `getResourceCondition` accepts **either** shape: it tries fieldpath `resource.status` first (pass `(index .observed.resources "n")`), then falls back to `status` (pass `(getComposedResource . "n")`). Both verified.

**Dotted resource names are safe.** `getComposedResource` builds fieldpath `observed.resources[%s]resource` (no `.` before `resource` — unusual but correct crossplane-runtime syntax). Verified with `my.dotted-name`. [V]

**Function registration order** (`GetNewTemplateWithFunctionMaps`): custom map first → `include` → **Sprig last**. **Sprig therefore overrides on a name collision.** No collision exists in v3.3.0, but this ordering is load-bearing if either side adds a name. [V]

#### Sprig availability

- **Sprig v3.3.0, providing 211 functions** (verified by compiling against `github.com/Masterminds/sprig/v3@v3.3.0` and printing `len(sprig.FuncMap())`). [V]
- **Exactly two are deleted**, with this source comment: *"Sprig's env and expandenv can lead to information leakage (injected tokens/passwords). Both Helm and ArgoCD remove these due to security implications."*
  - `env` → `invalid function input: cannot parse the provided templates: template: manifests:4: function "env" not defined` [V]
  - `expandenv` → same [V]
- **209 Sprig functions remain**, plus 11 custom, plus Go built-ins (`and`, `or`, `not`, `len`, `index`, `slice`, `print`, `printf`, `println`, `call`, `html`, `js`, `urlquery`, `eq`, `ne`, `lt`, `le`, `gt`, `ge`).

Verified working and likely reached for: `quote`, `squote`, `b64enc`, `b64dec`, `indent`, `nindent`, `default`, `dig`, `hasKey`, `keys`, `join`, `toJson`, `fromJson`, `mustToJson`, `toPrettyJson`, `toRawJson`, `toString`, `int`, `until`, `ternary`, `semver`, `trim`, `merge`, `dict`, `list`, `lower`, `upper`, `trunc`, `sha256sum`, `uuidv4`, `randAlphaNum`.

**`toYaml`/`fromYaml` are NOT Sprig** — verified absent from the 211. `toJson`/`fromJson`/`mustToJson`/`toPrettyJson`/`toRawJson` **are** Sprig.

**Security note.** The removals stop at `env`/`expandenv`. Still reachable and network- or entropy-active: **`getHostByName`** (live DNS lookup — an exfiltration channel from a template), `genPrivateKey`, `genCA`, `genSelfSignedCert`, `bcrypt`, `derivePassword`, `encryptAES`/`decryptAES`, `randBytes`, `uuidv4`. **If the generator ever renders untrusted template text, lint against these.**

#### Template context — exact dotted paths

The dot `.` is the **entire `RunFunctionRequest`**, protojson-marshalled to `map[string]any`. Keys are **lowerCamelCase protojson names**.

**Top-level keys, verified by `{{ range $k, $v := . }}`:** [V]

```
context           always present (may be {})
desired           always present (may be {})
input             always present — your own GoTemplate input, reflected back
meta              always present — {tag, capabilities}
observed          always present
credentials       ONLY when the pipeline step declares `credentials:`
extraResources    ONLY after requirements are satisfied (2nd invocation)
requiredResources ONLY after requirements are satisfied (2nd invocation)
requiredSchemas   never populated by this function — INFERRED, not observed
```

**Verified paths:** [V]

```
.observed.composite.resource                      # full XR: apiVersion, kind, metadata, spec, status
.observed.composite.resource.metadata.name
.observed.composite.resource.metadata.namespace   # populated for v2 Namespaced XRs
.observed.composite.resource.metadata.uid
.observed.composite.resource.metadata.labels."crossplane.io/composite"
.observed.composite.resource.spec.<your XRD fields>
.observed.composite.resource.spec.crossplane.compositionRef.name    # v2 nesting
.observed.composite.resource.spec.crossplane.resourceRefs[]         # v2 nesting
.observed.composite.resource.status.conditions[]
.observed.composite.connectionDetails              # map[string]<base64 string>

.observed.resources                                # map keyed by composition-resource-name
.observed.resources.<name>.resource                # full observed MR incl. status.atProvider
.observed.resources.<name>.connectionDetails       # map[string]<base64 string>
# .observed.resources.<name>.ready is NEVER set on the request

.desired.composite.resource                        # PARTIAL — only what prior steps set
.desired.composite.connectionDetails
.desired.resources.<name>.resource                 # PARTIAL
.desired.resources.<name>.ready                    # "READY_TRUE" | "READY_FALSE" | "READY_UNSPECIFIED"

.context.<key>                                     # use index for keys with dots/slashes
.credentials.<name>.credentialData.data.<key>      # base64 STRING
.extraResources.<key>.items[].resource
.requiredResources.<key>.items[].resource
.meta.tag                                          # opaque request hash
.meta.capabilities[]
```

**`.desired.composed` does not exist.** Verified: `has_desired_composed_README: "NO"` vs `has_desired_resources: "YES"`. Backed by the proto: `message State { Resource composite = 1; map<string, Resource> resources = 2; }`. [V]

**`.environment` does not exist as a top-level key.** In Crossplane v2.4.0 the composition environment lives only in the context at `index .context "apiextensions.crossplane.io/environment"`, and only if an earlier step put it there. `kubectl explain composition.spec.environment` → `error: field "environment" does not exist` — the native `spec.environment` block is **gone** in v2. The `EnvironmentConfig` CRD does still exist. [V]

#### Meta kinds — all `apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1`

| Kind | Payload field | Effect |
|---|---|---|
| `CompositeConnectionDetails` | `data` (map, **base64 values**) | Sets `desiredComposite.ConnectionDetails` — **ignored for v2 XRs** |
| `Context` | `data` (map) | Deep-merges into response context (`mergo.WithOverride`) |
| `ExtraResources` | `requirements` (map) | Sets `rsp.Requirements` |
| `ClaimConditions` | `conditions` (list) | Appends to `rsp.Conditions` |

**The error message for an unknown kind omits `ClaimConditions`** — verbatim: [V]

```
invalid kind "Bogus" for apiVersion "meta.gotemplating.fn.crossplane.io/v1alpha1"
 - must be one of CompositeConnectionDetails, Context or ExtraResources
```

`ClaimConditions` nonetheless works; only the `default:` branch's message string is stale. **Do not treat this message as the authoritative kind list.**

`ExtraResources` requirement shape (camelCase, deliberately):

```yaml
apiVersion: meta.gotemplating.fn.crossplane.io/v1alpha1
kind: ExtraResources
requirements:
  <key>:
    apiVersion: v1          # required
    kind: ConfigMap         # required
    matchName: team-settings   # OR matchLabels (matchLabels ignored if matchName set)
    matchLabels:
      app: my-app
    namespace: team-a       # omit for cluster-scoped
```

A namespaced requirement is **v2-only** (`fn.go` writes the deprecated v1-compat `ExtraResources` map only when `namespace == ""`). Duplicate keys are fatal: `duplicate extra resource key %q`.

`ClaimConditions` reserved types are rejected — reserved set is `Ready`, `Synced`, `Healthy`: [V]

```
cannot set ClaimCondition type: Ready is a reserved Crossplane Condition
```

Any `status` other than `"True"`/`"False"` **silently** becomes `STATUS_CONDITION_UNKNOWN`; any `target` other than `CompositeAndClaim` **silently** becomes `TARGET_COMPOSITE`. **Neither typo is reported.**

#### Readiness and the composite-status special rule

```yaml
metadata:
  annotations:
    {{ setResourceNameAnnotation "the-queue" }}
    gotemplating.fn.crossplane.io/ready: "True"
```

Values are **case-sensitive**: exactly `True`, `False`, `Unspecified`. [V]

```
invalid function input: invalid "gotemplating.fn.crossplane.io/ready" annotation value "true": must be True, False, or Unspecified
```

Missing name annotation is fatal: [V]

```
"Queue" template is missing required "gotemplating.fn.crossplane.io/composition-resource-name" annotation
```

Both meta annotations are stripped before emit — but `metadata.annotations: {}` is left behind as an empty map. [V]

**The composite-status rule:** if a rendered document's `apiVersion` **and** `kind` match the observed composite **and** it carries no `composition-resource-name` annotation, the function merges its `status` into the desired composite and **does not create a composed resource**. Adding the annotation flips it to creating a composed resource of the XR's own type. [V]

#### Multi-document output

Decoded with `k8s.io/apimachinery/pkg/util/yaml.NewYAMLOrJSONDecoder` over the whole buffer. Documents separated by a line that is exactly `---` after trimming. **Leading `---`, trailing `---`, consecutive `---`, and documents emptied by `{{- if false }}…{{- end }}` are all tolerated and skipped.** [V] `inline.templates` entries are joined with `"\n---\n"`, so the two forms are exactly equivalent.

#### README defects in v0.12.0 (source/behaviour is authoritative)

| # | README says | Reality [V] |
|---|---|---|
| 1 | `options` nested under `inline:` | Top-level on `GoTemplate`. Nested = fatal |
| 2 | `{{ (index .desired.composed "name").resource… }}` | No `.desired.composed`. It is `.desired.resources` |
| 3 | Additional Functions table lists 10 | 11 exist; **`getCredentialData` is missing** |
| 4 | `crossplane beta render` | `crossplane render` in CLI v2.5.0 |
| 5 | — | The `invalid kind` runtime error omits `ClaimConditions`, which is supported |
| 6 | ExtraResources sketch shows `"key": [ … ]` | Actual shape is `"key": {"items": [ {"resource": {…}} ]}` |

#### Reference skeleton for the generator [V — renders cleanly on the target stack]

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.aws.platform.hooli.tech
spec:
  compositeTypeRef:
    apiVersion: platform.hooli.tech/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
  - step: render-templates
    functionRef:
      name: function-go-templating
    input:
      apiVersion: gotemplating.fn.crossplane.io/v1beta1
      kind: GoTemplate
      source: Inline
      options:
      - missingkey=error
      inline:
        template: |
          ---
          apiVersion: sqs.aws.m.upbound.io/v1beta1
          kind: Queue
          metadata:
            annotations:
              {{ setResourceNameAnnotation "queue" }}
          spec:
            forProvider:
              # required string
              region: {{ .observed.composite.resource.spec.region | quote }}
              # optional integer
              {{- with .observed.composite.resource.spec.delaySeconds }}
              delaySeconds: {{ . }}
              {{- end }}
              # optional object
              {{- with .observed.composite.resource.spec.tags }}
              tags:
                {{- toYaml . | nindent 6 }}
              {{- end }}
            providerConfigRef:
              kind: ClusterProviderConfig
              name: default
          ---
          # status write-back: same apiVersion+kind as the XR, NO name annotation
          apiVersion: platform.hooli.tech/v1alpha1
          kind: XQueue
          status:
            {{- with (getComposedResource . "queue") }}
            queueUrl: {{ dig "status" "atProvider" "url" "" . | quote }}
            ready: {{ (getResourceCondition "Ready" .).Status | quote }}
            {{- end }}
  - step: automatically-detect-ready-composed-resources
    functionRef:
      name: function-auto-ready
```

### 3.6 Working Go code for xpkg extraction

**Provenance:** recovered from the workflow's shared scratchpad at `xpkgget/main.go`. The authoring brief (`schema-sourcing.md`) is missing, so this code has no accompanying narrative. **I verified it myself:** it compiles clean under `go 1.27.0`, runs anonymously against `xpkg.upbound.io` in 1.84 s, and its output is **byte-identical (MD5 `6023a8c7e51284baa6c9186abb791357`)** to the package stream `crossplane xpkg extract` produces. [V-me]

`go.mod`:

```
module xpkgget

go 1.27.0

require github.com/google/go-containerregistry v0.22.0

require (
	github.com/docker/cli v29.7.2+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.3 // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	gotest.tools/v3 v3.5.2 // indirect
)
```

`main.go`:

```go
// xpkgget extracts the Crossplane package stream (package.yaml) from any xpkg
// OCI image, without Docker and without a cluster. It downloads ONLY the
// package ("base") layer -- never the provider runtime layer.
package main

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Per the Crossplane xpkg specification the package layer is identified either
// by an OCI layer annotation io.crossplane.xpkg=base, or (as Upbound and the
// crossplane CLI actually emit) by an image-config label whose KEY is
// "io.crossplane.xpkg:<compressed layer digest>" and whose VALUE is "base".
const (
	xpkgAnnotation = "io.crossplane.xpkg"
	baseRole       = "base"
	streamFile     = "package.yaml"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: xpkgget <oci-ref> [out.yaml]")
		os.Exit(2)
	}
	out := "-"
	if len(os.Args) > 2 {
		out = os.Args[2]
	}
	data, meta, err := FetchPackageStream(context.Background(), os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "ref            = %s\n", meta.Ref)
	fmt.Fprintf(os.Stderr, "manifest type  = %s\n", meta.ManifestType)
	fmt.Fprintf(os.Stderr, "image digest   = %s\n", meta.ImageDigest)
	fmt.Fprintf(os.Stderr, "layers total   = %d\n", meta.LayerCount)
	fmt.Fprintf(os.Stderr, "base layer     = %s (%s)\n", meta.BaseDigest, meta.FoundVia)
	fmt.Fprintf(os.Stderr, "base layer B   = %d compressed\n", meta.BaseCompressed)
	fmt.Fprintf(os.Stderr, "stream bytes   = %d uncompressed\n", len(data))
	fmt.Fprintf(os.Stderr, "image bytes    = %d (all layers, NOT downloaded)\n", meta.AllLayersBytes)

	if out == "-" {
		os.Stdout.Write(data)
		return
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// Meta reports what the extractor observed about the image.
type Meta struct {
	Ref            string
	ManifestType   string
	ImageDigest    string
	LayerCount     int
	BaseDigest     string
	FoundVia       string
	BaseCompressed int64
	AllLayersBytes int64
}

// FetchPackageStream returns the raw multi-document YAML package stream.
func FetchPackageStream(ctx context.Context, ref string) ([]byte, Meta, error) {
	var m Meta
	m.Ref = ref

	r, err := name.ParseReference(ref)
	if err != nil {
		return nil, m, fmt.Errorf("parse ref: %w", err)
	}

	// authn.DefaultKeychain reads ~/.docker/config.json (and $DOCKER_CONFIG),
	// including credential helpers such as docker-credential-osxkeychain.
	// Anonymous pulls simply fall through when no entry matches the registry.
	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithUserAgent("xpkgget/0.1"),
	}

	desc, err := remote.Get(r, opts...)
	if err != nil {
		return nil, m, fmt.Errorf("get manifest: %w", err)
	}
	m.ManifestType = string(desc.MediaType)

	img, err := resolveImage(desc, opts)
	if err != nil {
		return nil, m, err
	}

	if d, err := img.Digest(); err == nil {
		m.ImageDigest = d.String()
	}

	mf, err := img.Manifest()
	if err != nil {
		return nil, m, fmt.Errorf("manifest: %w", err)
	}
	m.LayerCount = len(mf.Layers)
	for _, l := range mf.Layers {
		m.AllLayersBytes += l.Size
	}

	h, via, err := findBaseLayer(img, mf)
	if err != nil {
		return nil, m, err
	}
	m.BaseDigest, m.FoundVia = h.String(), via

	// LayerByDigest is lazy: only this one blob is fetched over the network.
	layer, err := img.LayerByDigest(h)
	if err != nil {
		return nil, m, fmt.Errorf("layer %s: %w", h, err)
	}
	if sz, err := layer.Size(); err == nil {
		m.BaseCompressed = sz
	}

	rc, err := layer.Uncompressed() // transparently gunzips
	if err != nil {
		return nil, m, fmt.Errorf("open layer: %w", err)
	}
	defer rc.Close()

	data, err := readStreamFromTar(rc)
	if err != nil {
		return nil, m, err
	}
	return data, m, nil
}

// resolveImage turns a descriptor into an image, handling multi-arch indexes.
// The xpkg base layer is byte-identical across architectures, so any child
// manifest yields the same package stream; we prefer linux/amd64 and otherwise
// take the first child.
func resolveImage(desc *remote.Descriptor, opts []remote.Option) (v1.Image, error) {
	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList:
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, fmt.Errorf("image index: %w", err)
		}
		im, err := idx.IndexManifest()
		if err != nil {
			return nil, fmt.Errorf("index manifest: %w", err)
		}
		var pick *v1.Descriptor
		for i := range im.Manifests {
			d := im.Manifests[i]
			if d.MediaType.IsIndex() || d.MediaType.IsImage() {
				if d.Platform != nil && d.Platform.OS == "linux" && d.Platform.Architecture == "amd64" {
					pick = &d
					break
				}
				if pick == nil {
					pick = &d
				}
			}
		}
		if pick == nil {
			return nil, errors.New("index has no image manifests")
		}
		return idx.Image(pick.Digest)
	default:
		return desc.Image()
	}
}

// findBaseLayer locates the package layer by config label first (what Upbound
// and the crossplane CLI emit), then by OCI layer annotation (the spec form).
func findBaseLayer(img v1.Image, mf *v1.Manifest) (v1.Hash, string, error) {
	cf, err := img.ConfigFile() // fetches only the small config blob
	if err == nil && cf != nil {
		for k, v := range cf.Config.Labels {
			if v != baseRole || !strings.HasPrefix(k, xpkgAnnotation+":") {
				continue
			}
			h, err := v1.NewHash(strings.TrimPrefix(k, xpkgAnnotation+":"))
			if err != nil {
				continue
			}
			return h, "config label io.crossplane.xpkg:<digest>=base", nil
		}
	}
	for _, l := range mf.Layers {
		if l.Annotations[xpkgAnnotation] == baseRole {
			return l.Digest, "OCI layer annotation io.crossplane.xpkg=base", nil
		}
	}
	return v1.Hash{}, "", errors.New("no layer marked io.crossplane.xpkg=base")
}

// readStreamFromTar pulls package.yaml out of the layer tarball.
func readStreamFromTar(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s not found in base layer", streamFile)
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.TrimPrefix(hdr.Name, "./") != streamFile {
			continue
		}
		return io.ReadAll(tr)
	}
}
```

**Real output, run during this synthesis:** [V-me]

```
$ ./xpkgget xpkg.upbound.io/upbound/provider-aws-sqs:v2 /tmp/verify-sqs.yaml
ref            = xpkg.upbound.io/upbound/provider-aws-sqs:v2
manifest type  = application/vnd.oci.image.index.v1+json
image digest   = sha256:1aff5a5aa39ec5c103782c098fe28a2774793e68c1419bc450a26c0a361e35f7
layers total   = 18
base layer     = sha256:04115f40bbaf016f4e530ef00fc2b7d2171061d71a1d4f243b1970985c44cc98 (config label io.crossplane.xpkg:<digest>=base)
base layer B   = 20071 compressed
stream bytes   = 182766 uncompressed
image bytes    = 271482755 (all layers, NOT downloaded)

real  0m1.841s

$ md5 /tmp/verify-sqs.yaml
MD5 (/tmp/verify-sqs.yaml) = 6023a8c7e51284baa6c9186abb791357   # identical to CLI extraction
```

**Two implementation details worth preserving.** The base layer is located by **config label first** (`io.crossplane.xpkg:<digest>=base`, what Upbound and the CLI actually emit) and only then by the spec's OCI **layer annotation** (`io.crossplane.xpkg=base`) — a generator that only implements the spec form will fail on real Upbound packages. And `LayerByDigest` is lazy, which is what turns a 271 MB image into a 20 KB fetch.

### 3.7 Exact CLI flags — `crossplane` v2.5.0

**Verified command tree** (banner: *"Beta features are enabled"* — beta is **on by default**): [V]

```
cluster       [BETA]   cluster top
composition            composition convert | generate | render
config                 config set | view
dependency    [BETA]   dependency add | update-cache | clean-cache
function      [BETA]   function generate
project       [BETA]   project init | build | push | run | stop
resource      [BETA]   resource trace | validate
version
xpkg                   xpkg batch|build|get-crds|init|install|push|update|extract
xrd           [BETA]   xrd convert | generate
completions
```

**Migration table:** [V]

| Pre-2.x | v2.5.0 |
|---|---|
| `crossplane beta render` | `crossplane composition render` |
| `crossplane render` | **still works — hidden top-level alias** |
| `crossplane beta validate` | `crossplane resource validate` |
| `crossplane validate` | **does not exist** → `error: unexpected argument validate`, **exit 80** |
| `crossplane beta trace` | `crossplane resource trace` |
| `crossplane beta <anything>` | **gone** → `error: unexpected argument beta`, exit 80 |

Alpha-gated behind `features.enableAlpha: true`: `operation render`, `xr generate`, `xr patch`. [V]

#### `crossplane composition render` — verbatim `--help` flag block [V]

```
Usage: crossplane composition render <composite-resource> <composition> [<functions>] [flags]

Flags:
  -h, --help                       Show context-sensitive help.
      --config=PATH                Path to the crossplane CLI configuration file ($CROSSPLANE_CONFIG).
      --verbose                    Print verbose logging statements.

      --crossplane-version=VERSION
                                   Version of the Crossplane image to use for rendering. Defaults to the latest stable version.
      --crossplane-image=IMAGE     Override the full Crossplane Docker image reference for rendering.
      --crossplane-binary=PATH     Path to a local crossplane binary to use instead of Docker.
      --crossplane-docker-network=STRING
                                   The docker network to start the crossplane container in
      --context-files=KEY=VALUE;...
                                   Comma-separated context key-value pairs to pass to the Function pipeline. Values must be files containing JSON/YAML.
      --context-values=KEY=VALUE;...
                                   Comma-separated context key-value pairs to pass to the Function pipeline. Values must be JSON/YAML. Keys take precedence over --context-files.
  -r, --include-function-results
                                   Include informational and warning messages from Functions in the rendered output as resources of kind: Result.
  -x, --include-full-xr            Include a direct copy of the input XR's spec and metadata fields in the rendered output.
  -o, --observed-resources=PATH    A YAML file or directory of YAML files specifying the observed state of composed resources.
      --extra-resources=PATH       A YAML file or directory of YAML files specifying required resources (deprecated, use --required-resources). Provide multiple files by repeating the argument.
  -e, --required-resources=PATH    A YAML file or directory of YAML files specifying required resources to pass to the Function pipeline. Provide multiple files by repeating the argument.
  -s, --required-schemas=DIR       A directory of JSON files specifying OpenAPI v3 schemas (from kubectl get --raw /openapi/v3/<group-version>).
  -c, --include-context            Include the context in the rendered output as a resource of kind: Context.
      --function-credentials=PATH
                                   A YAML file or directory of YAML files specifying credentials to use for Functions to render the XR.
  -a, --function-annotations=KEY=VALUE,...
                                   Override function annotations for all functions. Provide multiple annotations by repeating the argument.
      --cache-dir=STRING           Directory for cached xpkg package contents ($CROSSPLANE_XPKG_CACHE).
      --max-concurrency=8          Maximum concurrency for building embedded functions.
  -f, --project-file="crossplane-project.yaml"
                                   Path to the project file. Optional.
      --timeout=1m                 How long to run before timing out.
      --xrd=PATH                   A YAML file specifying the CompositeResourceDefinition (XRD) that defines the XR's schema and properties.
```

**Function runtime annotations** (`-a` overrides them globally): [D from help; pull-policy [V]]

| Annotation | Purpose |
|---|---|
| `render.crossplane.io/runtime: "Development"` | Connect to a function on `localhost:9443` running with `--insecure` instead of Docker |
| `render.crossplane.io/runtime-development-target: "dns:///example.org:7443"` | Non-default gRPC target |
| `render.crossplane.io/runtime-docker-cleanup: "Orphan"` | Don't stop the container after rendering |
| `render.crossplane.io/runtime-docker-name: "<name>"` | Create/reuse a named container |
| `render.crossplane.io/runtime-docker-pull-policy: "Always"` | Also `Never`, `IfNotPresent` |
| `render.crossplane.io/runtime-docker-publish-address: "0.0.0.0"` | Default `127.0.0.1` |
| `render.crossplane.io/runtime-docker-target: "docker-host"` | Address the CLI dials |

Also honours `DOCKER_HOST`, `DOCKER_API_VERSION`, `DOCKER_CERT_PATH`, `DOCKER_TLS_VERIFY`.

**Measured timings:** [V]

| Scenario | Wall time |
|---|---|
| Warm (all images cached) | **1.05 – 1.46 s** |
| Forced manifest re-check (`-a …pull-policy=Always`) | **3.55 s** |
| Cold engine image pull (`--crossplane-version=v2.3.0`, ~106 MB) | **7.35 s** |

Fully cold (engine 106 MB + go-templating 80.1 MB + auto-ready 78.9 MB ≈ 265 MB) not measured [U]; extrapolating, a first CI run plausibly **exceeds the `--timeout=1m` default**. **Generate `--timeout=5m` into shipped Makefiles.**

**Determinism** — three consecutive runs, identical MD5 `3eb474d6df52dd0e6b8a6d53536bb732`; independently reproduced in a second brief with sha256 `332ee29a41baca893d709d2b680c3bd2bb1252f8b133fdc4588fd7d19d435e80` ×3. [V] `status.conditions[].lastTransitionTime` is frozen at `"2024-01-01T00:00:00Z"`; composed names are content-derived hashes (`demo-queue-2d702055d0fb`); owner-ref UIDs are deterministic. **Golden files are safe.**

**Template errors are precise but offset:** [V]

```
crossplane: error: cannot render composite resource: crossplane internal render in Docker:
pipeline returned fatal: ... pipeline step "render-queue" returned a fatal result:
cannot execute template: template: manifests:9:15: executing "manifests" at
<index (dict "EU" "eu-north-1" "US" "us-east-2") $spec.location>: error calling index:
value is nil; should be of type string
```

Line numbers are relative to the **rendered template body**, not the Composition file. **Keep an offset map to translate `manifests:9` back to a source line.**

Also: functions must be a **multi-document YAML stream**, not a `List` — `cannot load functions from "…": not a function: List/`. [V]

#### `crossplane resource validate` — verbatim `--help` flag block [V]

```
Usage: crossplane resource validate <extensions> <resources> [flags]

Flags:
  -h, --help                    Show context-sensitive help.
      --config=PATH             Path to the crossplane CLI configuration file ($CROSSPLANE_CONFIG).
      --verbose                 Print verbose logging statements.

      --cache-dir="~/.crossplane/cache"
                                Absolute path to the cache directory for downloaded schemas.
      --clean-cache             Clean the cache directory before downloading package schemas.
      --crossplane-image="xpkg.crossplane.io/crossplane/crossplane:stable"
                                Specify the Crossplane image for validating built-in schemas.
      --error-on-missing-schemas
                                Return non zero exit code if missing schemas.
  -o, --output=text             Output format for validation results (text, json, or yaml).
      --skip-success-results    Skip printing success results.
      --update-cache            Update cached schemas by downloading the latest version that satisfies a constraint. May be useful if you are using semantic version constraints and want to get the latest version, but this slows down the cache lookup due to the required network calls.
```

Both positional args accept **comma-separated lists of files, directories, or `-` (stdin)**. Help text states: *"All validation happens offline using the Kubernetes API server's validation library, without requiring a Crossplane instance or control plane."* — confirmed, **including no Docker** (works with `DOCKER_HOST=tcp://127.0.0.1:1`). [V]

**What it catches** [V] — against provider CRDs:

```
[x] schema validation error sqs.aws.m.upbound.io/v1beta1, Kind=Queue, demo-queue-2d702055d0fb :
    spec.forProvider.maxMessageSize: Invalid value: "string": ... must be of type number: "string"
[x] schema validation error ... spec.forProvider.totallyBogusField: Invalid value:
    "totallyBogusField": unknown field: "totallyBogusField"
Total 2 resources: 0 missing schemas, 1 success cases, 1 failure cases
crossplane: error: could not validate all resources
```

Against the XRD (no render, no cluster):

```
[x] spec.location: Unsupported value: "ASIA": supported values: "EU", "US"
[x] spec.maxMessageSize: Invalid value: 10: ... should be greater than or equal to 1024
[x] spec.providerName: Required value
[x] spec.bogusField: Invalid value: "bogusField": unknown field: "bogusField"
```

CEL rules fire too:

```
[x] CEL validation error platform.hooli.tech/v1alpha1, Kind=XQueue, demo-queue :
    spec: Invalid value: EU queues must use an eu- prefixed providerConfig
```

**Function input schemas** — it descends into `spec.pipeline[].input` and checks against the function package's own input CRD: [V]

```
schemas does not exist, downloading:  xpkg.upbound.io/crossplane-contrib/function-go-templating:v0.12.0
[✓] gotemplating.fn.crossplane.io/v1beta1, Kind=GoTemplate,  validated successfully
[✓] apiextensions.crossplane.io/v1, Kind=Composition, xqueues.aws.platform.hooli.tech validated successfully
```

⚠️ Not exhaustive: changing `source: Inline` → `Inlin3` was **not** caught (the GoTemplate CRD has no enum on `source`).

**JSON output** — ideal for a generator's test harness: [V]

```json
{
  "summary": {"total": 1, "valid": 0, "invalid": 1, "missingSchemas": 0},
  "resources": [{
    "apiVersion": "platform.hooli.tech/v1alpha1", "kind": "XQueue",
    "name": "bad-queue", "namespace": "team-a", "status": "invalid",
    "errors": [
      {"type": "schema", "field": "spec.location",
       "message": "spec.location: Unsupported value: \"ASIA\": supported values: \"EU\", \"US\"",
       "value": "ASIA"},
      {"type": "unknownField", "field": "spec.bogusField",
       "message": "spec.bogusField: Invalid value: \"bogusField\": unknown field: \"bogusField\"",
       "value": "bogusField"}
    ]
  }]
}
```

Error `type` values observed: `schema`, `unknownField`, and CEL failures.

**Trap 1 — missing schemas are silent by default:** [V]

```
[!] could not find CRD/XRD for: sqs.aws.m.upbound.io/v1beta1, Kind=Queue
Total 2 resources: 1 missing schemas, 1 success cases, 0 failure cases
EXIT=0                      ← !!
```

With `--error-on-missing-schemas`: `crossplane: error: could not validate all resources, schema(s) missing`, **exit 1**. **Always generate this flag.**

**Trap 2 — `-r` and `-c` break `--error-on-missing-schemas`:** [V]

| Render flags | Result |
|---|---|
| `-x` | exit **0** |
| `-x -r` | `[!] could not find CRD/XRD for: render.crossplane.io/v1beta1, Kind=Result` ×2 → exit **1** |
| `-x -c` | `[!] … Kind=Context` → exit **1** |

**Use `-x` only in the validated pipe.** Keep `-r -c` in a separate human-facing target.

**Trap 3 — `--xrd` on render does defaulting, not validation.** An XR with `location: ASIA` (enum violation), `maxMessageSize: 10` (below `minimum: 1024`), a missing required field, and an unknown field rendered **byte-identically with and without `--xrd`, exit 0**. [V] Defaulting *does* work (`location.default: US` → `region: us-east-2`). **The generator must emit a separate `resource validate` gate.**

**Validating an XRD or Composition offline** — pass an **empty directory** as extensions and it falls back to built-in schemas from `--crossplane-image`: [V]

```
$ mkdir -p empty-ext && crossplane resource validate empty-ext/ xrd.yaml
[✓] apiextensions.crossplane.io/v2, Kind=CompositeResourceDefinition, xqueues.platform.hooli.tech validated successfully
EXIT=0
```

⚠️ **Limit [U]:** this checks the XRD against the *CompositeResourceDefinition CRD* — field legality, types, required keys. It was **not** confirmed to enforce full **structural-schema** legality (e.g. a `type`-less property node). For that guarantee, `kubectl apply --dry-run=server` on the *derived CRD* is the authoritative check.

**The cache** is `~/.crossplane/cache`, **1.2 MB total** — it stores extracted `package.yaml` per package, not images. The 1.21 GB `provider-aws-sqs` image is *not* pulled. Cache this directory in CI. [V]

#### `crossplane xpkg get-crds` — verbatim `--help` flag block [V-me]

```
Usage: crossplane xpkg get-crds <extensions> [flags]

Arguments:
  <extensions>    Extension sources as a comma-separated list of files, directories, or '-' for standard input.

Flags:
  -h, --help                       Show context-sensitive help.
      --config=PATH                Path to the crossplane CLI configuration file ($CROSSPLANE_CONFIG).
      --verbose                    Print verbose logging statements.

      --cache-dir="~/.crossplane/cache"
                                   Absolute path to the cache directory holding downloaded schemas.
      --clean-cache                Clean the cache directory before downloading package schemas.
      --crossplane-image=STRING    Specify the Crossplane image for fetching the built-in schemas.
      --flat                       Write files to a flat directory instead of organizing by group and version.
      --json-schema                Write JSON Schema files instead of CRDs. Useful for YAML language server integration.
      --no-cache                   Disable caching entirely. The command downloads schemas every time without storing them.
  -o, --output-dir="."             Directory that receives the CRD or JSON Schema files. Defaults to current directory.
      --update-cache               Update cached schemas by downloading the latest version that satisfies a constraint.
```

Default layout is `<group>/<version>/<kind>.{yaml|json}`. Accepts `crossplane.yaml`, a directory, `-` for stdin, or a `Provider`/`Function`/`Configuration` manifest.

**Reproduction:** [V]

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

Pointed at **Function** manifests it also vendors function input CRDs (`gotemplating.fn.crossplane.io/v1beta1/gotemplate.yaml`) — **one command vendors provider MR schemas *and* function input schemas.** [V] Cache after 8 providers: 18 MB. [V]

**⚠️ Schema-source disagreement between briefs — resolved, not silently.** `prior-art.md` recommends *"shell out to `crossplane dependency add` + the CLI's schema generator"* (13.6 s cold for provider-aws-sqs, 39 JSON Schema files, digest-pinned lock) but flags in the same paragraph that **it strips all `x-kubernetes-*` extensions** (`grep -o 'x-kubernetes-[a-z-]*'` on the generated `Queue.schema.json` returns nothing; the live CRD has 4 `x-kubernetes-map-type`). `crd-schema-shape.md` and `validation-tooling.md` both recommend `crossplane xpkg get-crds`, which emits raw CRDs and preserves them. **These are different commands, not a contradiction — but the recommendations do conflict in practice.** Since `x-kubernetes-validations` **is** the requiredness mechanism (§3.3), stripping it is disqualifying. **Use `get-crds` for schemas; borrow `dependency add`'s digest-pinned lockfile discipline separately.**

#### Other v2.5.0 commands relevant to the generator

- **`crossplane xrd convert <file>`** [BETA] — renders the derived CRD(s) offline using the same `xcrd` code the controller runs. Flags `-o/--output-file`, `--output-dir`, `--format=crd|jsonschema`. **`jsonschema` emits a `$id`-tagged JSON Schema with `additionalProperties: false` injected** — directly usable to drive a form builder or YAML language server. [V] Three caveats: it parses through **v1** Go types (omitted `scope` → LegacyCluster); its output carries `ownerReferences[0].uid: ""` which the apiserver rejects (**strip `ownerReferences` before dry-running**); and it does **not** enforce v2 CEL rules.
- **`crossplane xrd generate <xr-or-simpleschema.yaml> [--from xr|simpleschema] [--plural] [--path] [--replace]`** — requires a project file. **Lossy** (see §4).
- **`crossplane composition generate <xrd> [--name] [--plural] [--path]`** — emits a Composition with a single `function-auto-ready` step. **The direct prior art, and a stub** (see §4).
- **`crossplane resource trace <resource> [<name>]`** — post-apply, needs a cluster. Flags `-o default|wide|json|dot|yaml`, `-n/--namespace`, `-c/--context`, `-w/--watch`, `-s/--show-connection-secrets` (names only, never values), `--show-package-dependencies=unique|all|none`, `--show-package-revisions=active|all|none`, `--concurrency=5`, `--as/--as-group/--as-uid`. [V]

```
$ crossplane resource trace xqueue cncf-pre-talk -n team-a
NAME                                           SYNCED   READY   STATUS
XQueue/cncf-pre-talk (team-a)                  True     True    Available
└─ Queue/cncf-pre-talk-e28dacd7ec77 (team-a)   True     True    Available
```

**No Crossplane validating webhook exists for XRDs.** `kubectl get validatingwebhookconfigurations` shows only `crossplane-no-usages`. All XRD validation is CRD-level CEL + strict decoding + downstream CRD rejection. [V]

### 3.8 The recommended `make test` for generated projects

Built and executed end-to-end; **full `make test` = 1.38 s warm, idempotent across runs.** [V]

```makefile
CROSSPLANE ?= crossplane
APIS       := apis
XRD        := $(APIS)/definition.yaml
COMP       := $(APIS)/composition.yaml
FUNCS      := $(APIS)/functions.yaml
DEPS       := $(APIS)/providers.yaml,$(APIS)/functions.yaml
SCHEMAS    := schemas
VALIDATE   := $(CROSSPLANE) resource validate $(SCHEMAS)
VFLAGS     := --error-on-missing-schemas --skip-success-results
CASES      := $(wildcard tests/*/)

.PHONY: test schemas lint render golden explain guard clean

# 0. Vendor provider + function CRDs once. The ONLY step that touches the network.
schemas:
	$(CROSSPLANE) xpkg get-crds $(DEPS) --output-dir $(SCHEMAS)
	cp $(XRD) $(SCHEMAS)/_xrd.yaml

# 1. Are the generated XRD + Composition legal? Also checks the embedded
#    function input against function-go-templating's own GoTemplate CRD.
lint:
	$(VALIDATE) $(XRD),$(COMP) $(VFLAGS)

# 2. Per case: gate the input XR on the XRD, render, then schema-check every
#    rendered managed resource against the real provider CRDs.
#    No -r/-c here: they emit schema-less Result/Context docs that
#    --error-on-missing-schemas rejects.
render:
	@set -e; for c in $(CASES); do \
	  echo "==> $$c"; \
	  $(VALIDATE) $$c/xr.yaml $(VFLAGS); \
	  $(CROSSPLANE) composition render $$c/xr.yaml $(COMP) $(FUNCS) \
	    --xrd=$(XRD) --include-full-xr --timeout=5m > $$c/rendered.yaml; \
	  $(VALIDATE) - $(VFLAGS) < $$c/rendered.yaml; \
	done

# 3. Catches silent behaviour changes schemas can't see. render is byte-deterministic.
golden:
	@set -e; for c in $(CASES); do \
	  if [ -f $$c/golden.yaml ]; then diff -u $$c/golden.yaml $$c/rendered.yaml && echo "==> $$c golden OK"; \
	  else cp $$c/rendered.yaml $$c/golden.yaml; echo "==> $$c golden CREATED"; fi; \
	done

# 4. Go templates emit the literal string "<no value>" on a missed key.
#    It is schema-VALID and passes every gate above. This is the only catch.
guard:
	@! grep -rn '<no value>\|<nil>' tests/*/rendered.yaml || \
	  { echo "FAIL: unresolved template expression in rendered output"; exit 1; }

explain:   # human debugging: function messages + pipeline context
	@for c in $(CASES); do $(CROSSPLANE) composition render $$c/xr.yaml $(COMP) $(FUNCS) --xrd=$(XRD) -x -r -c; done

test: lint render guard golden

clean:
	rm -rf $(SCHEMAS) tests/*/rendered.yaml
```

**Each layer proven to fire:** [V]

| Injected defect | Caught by | Observed |
|---|---|---|
| `bogusMrField: oops` in the template | `render` | `spec.forProvider.bogusMrField: … unknown field`, exit 2 |
| `notAField` in the GoTemplate input | `lint` | `Total 3 resources: … 1 failure cases`, exit 2 |
| `eu-north-1` → `eu-west-1` in the region map | `golden` | `-    region: eu-north-1` / `+    region: eu-west-1`, exit 2 |
| XR violating enum/min/required | `render` (XR gate) | 4 distinct errors, exit 1 |
| Missing required field → `<no value>` | **`guard` only** | everything else exits 0 |

**Why `--include-full-xr` is mandatory in the pipe:** without `-x` the XR's spec isn't in the stream, so XRD-level violations go unseen at the validate step. [V]

**CI lane split:** **Lane A** (`lint` only) — no Docker, no cluster, ~0.5 s, runs anywhere. **Lane B** (`test`) — needs Docker with network-create. **Lane C** (optional, pre-merge) — `kubectl apply --dry-run=server` against a real cluster, for the structural-schema gap. Cache `~/.crossplane/cache` (1.2 MB) and the Docker images (~265 MB).

**Golden-file testing in Go** — copy Helm's `internal/test/test.go` pattern verbatim: [V]

```go
var updateGolden = flag.Bool("update", false, "update golden files")

func AssertGoldenString(t TestingT, actual, filename string) {
	t.Helper()
	if err := compare([]byte(actual), path(filename)); err != nil {
		t.Fatalf("%v\n", err)
	}
}

func path(filename string) string {
	if filepath.IsAbs(filename) { return filename }
	return filepath.Join("testdata", filename)
}

func compare(actual []byte, filename string) error {
	actual = normalize(actual)
	if err := update(filename, actual); err != nil { return err }
	expected, err := os.ReadFile(filename)
	if err != nil { return fmt.Errorf("unable to read testdata %s: %w", filename, err) }
	expected = normalize(expected)
	if !bytes.Equal(expected, actual) {
		return fmt.Errorf("does not match golden file %s\n\nWANT:\n'%s'\n\nGOT:\n'%s'", filename, expected, actual)
	}
	return nil
}

func update(filename string, in []byte) error {
	if !*updateGolden { return nil }
	return os.WriteFile(filename, normalize(in), 0o666)
}

func normalize(in []byte) []byte { return bytes.ReplaceAll(in, []byte("\r\n"), []byte("\n")) }
```

Four details Helm gets right: `flag.Bool("update", …)` at package scope; the implicit `testdata/` join; **CRLF normalization on both sides**; and a `TestingT` interface rather than `*testing.T`.

**Corrections to common belief:** controller-tools does **not** use a `-update` flag (its CRD fixtures live in a separate nested Go module regenerated via `go generate`); kustomize compares against **inline expected strings** (93 uses of `AssertActualEqualsExpected`) — unmanageable for multi-hundred-line Compositions. [V]

For CLI end-to-end, `rogpeppe/go-internal/testscript` with txtar archives; `testscript.Params.UpdateScripts` regenerates both file goldens and embedded expectations [D]; set `RequireExplicitExec: true`.

### 3.9 Verified Go embed + SPA server

Built with go1.27.0 and exercised: [V]

```go
// web/embed.go
package web

import ("embed"; "io/fs"; "net/http"; "strings")

//go:embed all:dist
var distFS embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(distFS, "dist")
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" { p = "index.html" }
		if _, err := fs.Stat(sub, p); err != nil {
			r = r.Clone(r.Context())   // SPA history fallback
			r.URL.Path = "/"
		} else if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}
```

| Request | Result |
|---|---|
| `GET /` | `200`, `Content-Type: text/html; charset=utf-8` |
| `GET /assets/index-abc123.js` | `200`, `Cache-Control: public, max-age=31536000, immutable` |
| `GET /some/spa/route` | `200` + index.html (fallback works) |
| `GET /api/schemas` | `200` `{"ok":true}` (API route not shadowed) |

Binary: 8,411,298 bytes with a trivial frontend — the Go runtime floor.

Three details: **`//go:embed all:dist`, not `//go:embed dist`** (without `all:`, `embed` skips files beginning with `.` or `_`, and Vite emits `.vite/manifest.json`); **`embed` cannot reach outside its own directory** (set Vite's `build.outDir` into the Go package, `.gitignore` the output, add `dist/.gitkeep`); and **`http.FileServer` does not compress** (see §2.3).

PocketBase's `no_ui` build-tag pattern gives a second artifact from one tree: [D]

```go
// ui/embed_no_ui.go
//go:build no_ui
package ui
import "io/fs"
// DistDirFS is deliberately not set to prevent bundling the UI with the binary.
var DistDirFS fs.FS
```

Add Syncthing's runtime override (`--ui-dir ./web/dist`, cf. `STGUIASSETS`) — without it every frontend change during development requires a Go rebuild. **Commit `web/dist`** (PocketBase's choice, not Syncthing's) so `go install …@latest` works without a Node toolchain.

---

## 4. Prior art & positioning

### 4.1 The closest competitors — Crossplane's own generators

| Project | URL | Stars | License | Last activity | The gap it leaves |
|---|---|---|---|---|---|
| `crossplane composition generate` | <https://github.com/crossplane/cli> | — | Apache-2.0 | v2.5.0 | Emits a **12-line stub** with zero composed resources and never reads the XRD's schema fields. [V] |
| `crossplane function generate --language go-templating` | <https://github.com/crossplane/cli> | — | Apache-2.0 | v2.5.0 | Emits **two files that are 100% comments** — `00-prelude.yaml.gotmpl` is one line, `01-compose.yaml.gotmpl` is a commented-out NopResource. [V] |
| `crossplane xrd generate` | <https://github.com/crossplane/cli> | — | Apache-2.0 | v2.5.0 | Example-inference, not schema derivation: drops `enum`, `minimum`, `additionalProperties`, `required`, printer columns, and hardcodes **`scope: Cluster`**. [V] |
| `upbound/up` CLI | <https://github.com/upbound/up> → **HTTP 404** | — | closed | v0.53.2 (2026-08-20) | Same generators, now **closed source** — the open-source successor of that DevEx surface is the upstream CLI. [V] |
| Upbound Console | proprietary SaaS | — | proprietary | — | Observability and control-plane management; **no authoring canvas**. [D] |
| `upbound/vscode-up` | <https://github.com/upbound/vscode-up> | 16 | Apache-2.0 | 2024-10-10 | xpls-backed YAML diagnostics only; v0.0.6, 4,530 installs; **no visual authoring**. [V metrics] |

**The `composition generate` output, in full:** [V]

```yaml
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: xqueues.platform.hooli.tech
spec:
  compositeTypeRef:
    apiVersion: platform.hooli.tech/v1alpha1
    kind: XQueue
  mode: Pipeline
  pipeline:
  - functionRef:
      name: crossplane-contrib-function-auto-ready
    step: crossplane-contrib-function-auto-ready
```

The source XRD declared `location` (enum EU/US), `maxMessageSize` (minimum 1024), `providerName`, `tags`, `visibilityTimeoutSeconds` — **none of it influences the output.**

**`xrd generate` lossiness, measured against the hand-written XRD in git:** [V]

| Property | Hand-written (git) | `xrd generate` output |
|---|---|---|
| `scope` | `Namespaced` | **`Cluster`** (hardcoded) |
| `location` | `enum: [EU, US]` | `type: string` — enum lost |
| `maxMessageSize` | `minimum: 1024` | `type: integer` — bound lost |
| `visibilityTimeoutSeconds` | `minimum: 0` | dropped entirely |
| `required` | `[location, providerName]` | absent |
| `tags` | `additionalProperties: {type: string}` | `properties: {env: {type: string}}` — **map inferred as a struct from one sample** |
| `additionalPrinterColumns` | `LOCATION` | absent |

> **Docs contradiction flagged.** Upbound's go-templating docs page implies the generator emits a *"basic template structure with placeholder managed resources"* showing an `s3.aws.upbound.io/v1beta1 Bucket`. Verified against CLI v2.5.0, **that is not what is generated** — the S3 example illustrates what *you* write. **Treat any claim that these generators produce working resource bodies as false.** [V]

Also verified: a path bug — `--path apis/fromxr/definition.yaml` wrote to `apis/apis/fromxr/definition.yaml` (the flag is re-rooted under the project's APIs dir). [V]

### 4.2 Composition generators that read provider CRDs

| Project | URL | Stars | License | Last push | The gap it leaves |
|---|---|---|---|---|---|
| **x-generation** | <https://github.com/crossplane-contrib/x-generation> | 46 | Apache-2.0 | 2025-07-01 | The real competitor — but `provider.crd.file` is **singular**, so it produces thin 1:1 CRD wrappers, never a composed multi-resource abstraction; emits **patch-and-transform**, not go-templating; YAML-config CLI with no UI; 14 months stale. |
| benagricola/crossplane-composition-generator | <https://github.com/benagricola/crossplane-composition-generator> | 4 | MIT | 2023-02-17 | Dead. |
| moneyforward/crossplane-poc-x-generation | <https://github.com/moneyforward/crossplane-poc-x-generation> | 0 | Apache-2.0 | 2024-10-28 | **ARCHIVED.** |
| crossplane-cdk8s | <https://github.com/crossplane-contrib/crossplane-cdk8s> | 49 | Apache-2.0 | 2023-01-05 | Right idea (code → XRD+Composition) but dead 3.5 years, **predates composition functions entirely**, emits legacy patch-and-transform. |

**Upstream has explicitly declined this space:** [crossplane/crossplane#4989 "XRD and Claim Generation Tools"](https://github.com/crossplane/crossplane/issues/4989) was **closed as not planned**. [D] That is an opening, but it means no upstream blessing.

### 4.3 Output targets (runtimes, not competitors) — metrics 2026-08-27 [V]

| Repo | URL | Stars | License | Latest release | Last push |
|---|---|---|---|---|---|
| function-go-templating | <https://github.com/crossplane-contrib/function-go-templating> | 99 | Apache-2.0 | **v0.12.4** (2026-08-25) | 2026-08-27 |
| function-kcl | <https://github.com/crossplane-contrib/function-kcl> | 87 | Apache-2.0 | v0.12.2 (2026-07-19) | 2026-08-26 |
| function-patch-and-transform | <https://github.com/crossplane-contrib/function-patch-and-transform> | 44 | Apache-2.0 | v0.10.10 (2026-08-25) | 2026-08-26 |
| function-sequencer | <https://github.com/crossplane-contrib/function-sequencer> | 37 | Apache-2.0 | v0.6.0 (2026-06-23) | 2026-08-27 |
| function-auto-ready | <https://github.com/crossplane-contrib/function-auto-ready> | 35 | Apache-2.0 | v0.7.0 (2026-06-24) | 2026-08-26 |
| function-extra-resources | <https://github.com/crossplane-contrib/function-extra-resources> | 33 | Apache-2.0 | v0.3.0 (2026-01-10) | 2026-08-27 |
| function-environment-configs | <https://github.com/crossplane-contrib/function-environment-configs> | 27 | Apache-2.0 | v0.7.4 (2026-08-25) | 2026-08-25 |
| **function-cue** | <https://github.com/crossplane-contrib/function-cue> | 25 | Apache-2.0 | **no releases** | **2026-01-08** — do not target |
| function-python | <https://github.com/crossplane-contrib/function-python> | 20 | Apache-2.0 | v0.5.0 (2026-06-23) | 2026-08-26 |

**`function-go-templating` at 99 stars is the most-starred**, which validates it as the default output format. **Gap they all leave:** every one assumes a human already wrote the template/KCL/CUE/Python.

### 4.4 Visualizers, editors, and adjacent platforms

| Project | URL | Stars | License | Last push | The gap it leaves |
|---|---|---|---|---|---|
| komoplane | <https://github.com/komodorio/komoplane> | 386 | Apache-2.0 | 2026-08-25 | Read-only resource graph of objects that **already exist**; no authoring. |
| crossview | <https://github.com/crossplane-contrib/crossview> | 265 | Apache-2.0 | 2026-06-29 | Read-only dashboard — but **the reference implementation for the stack** (see §5). |
| xgql | <https://github.com/upbound/xgql> | 47 | Apache-2.0 | 2026-01-27 | GraphQL over Crossplane; latest release **v0.1.5, 2021-11-19** — effectively dead. (`crossplane-contrib/xgql` is a 404.) |
| cyclops | <https://github.com/cyclops-ui/cyclops> | 3,321 | Apache-2.0 | 2026-01-22 | Forms from Helm `values.schema.json`; **no Crossplane, no CRD path, no canvas.** |
| headlamp | <https://github.com/kubernetes-sigs/headlamp> | 7,160 | Apache-2.0 | 2026-08-27 | Extensible K8s UI; the most relevant **plugin host**, not a generator. |
| orange-cloudfoundry/Headlamp-plugin | <https://github.com/orange-cloudfoundry/Headlamp-plugin> | — | — | — | RJSF forms to **instantiate one existing CR** (consumer side); nobody is doing producer-side. |
| monokle | <https://github.com/kubeshop/monokle> | 2,140 | MIT | 2026-02-26 | Manifest validation IDE; 409 open issues, 6 months stale; no generation. |
| kubevious | <https://github.com/kubevious/kubevious> | 1,706 | Apache-2.0 | 2026-06-13 | Read-only config graph + rule engine. |
| kube-composer | <https://github.com/same7ammar/kube-composer> | 480 | **NONE** | 2025-08-16 | Drag-drop for **core workloads only**; **no license file** ⇒ unusable as a base. |
| weave-gitops | <https://github.com/weaveworks/weave-gitops> | 1,128 | Apache-2.0 | 2026-08-27 | Flux GitOps UI; Weaveworks shut down in 2024. |
| datree | <https://github.com/datreeio/datree> | 6,333 | Apache-2.0 | 2024-04-23 | **ARCHIVED**; policy linting only. |
| kalm | <https://github.com/kalmhq/kalm> | 431 | Apache-2.0 | 2022-05-13 | **Dead 4 years** despite live-looking "Closed Beta" docs. |
| kubevela | <https://github.com/kubevela/kubevela> | 7,888 | Apache-2.0 | 2026-08-27 | CNCF Incubating; a **competing abstraction model** (OAM + CUE) that replaces XRDs rather than authoring them. |
| radius | <https://github.com/radius-project/radius> | 1,665 | Apache-2.0 | 2026-08-27 | Bicep-based recipes; same — replaces, doesn't author. |
| kratix | <https://github.com/syntasso/kratix> | 770 | Apache-2.0 | 2026-08-27 | Promises; ships `kratix init crossplane-promise`, i.e. it **wraps** Crossplane rather than helping author it. |
| score-spec | <https://github.com/score-spec/spec> | 8,088 | Apache-2.0 | 2026-07-27 | Workload spec only; no XRD/Composition path. |
| react-jsonschema-form | <https://github.com/rjsf-team/react-jsonschema-form> | 15,877 | Apache-2.0 | 2026-08-27 | A form library — an **input**, not a solution; knows nothing about Crossplane. |
| jsonforms | <https://github.com/eclipsesource/jsonforms> | 2,736 | MIT | 2026-08-27 | Needs a hand-authored UISchema per form — **structurally incompatible** with thousands of auto-discovered CRDs. |
| uniforms | <https://github.com/vazco/uniforms> | 2,104 | MIT | 2026-01-12 | ~18 months stale; weaker `oneOf`/`additionalProperties` story. |
| kubernetes-models-ts | <https://github.com/tommy351/kubernetes-models-ts> | 163 | MIT | 2026-08-12 | CRD → TS **types**, not UI or compositions. |

Also useful as CRD-normalization reference implementations, all type generators: [pulumi/crd2pulumi](https://github.com/pulumi/crd2pulumi), [IvanJosipovic/KubernetesCRDModelGen](https://github.com/IvanJosipovic/KubernetesCRDModelGen), [yaacov/crdtoapi](https://github.com/yaacov/crdtoapi).

### 4.5 Positioning, in one paragraph

**The entire Upbound/Crossplane DevEx stack gives you an empty pipeline skeleton plus IDE autocomplete, then expects a human to hand-write every managed resource body in YAML.** The one tool that reads provider CRDs and writes real resource bodies (`x-generation`) is strictly 1:1, targets patch-and-transform, and is 14 months stale. Every visual tool is read-only or consumer-side. **Producer-side, multi-resource, go-templating-emitting, provider-agnostic, with a canvas — is uncontested ground.** The differentiator is not "generate an XRD" but **"generate a correct, constrained, namespaced XRD plus a real go-templating Composition, from provider schemas."**

---

## 5. Recommended stack

### 5.1 Frontend

```jsonc
{
  "@xyflow/react":            "12.11.5",  // MIT — canvas; ONLY candidate rendering nodes as real DOM
  "react":                    "19.2.8",
  "react-dom":                "19.2.8",
  "@rjsf/core":               "6.8.0",    // Apache-2.0 — schema forms
  "@rjsf/utils":              "6.8.0",
  "@rjsf/validator-ajv8":     "6.8.0",    // MUST run ajv strict:false
  "codemirror":               "6.0.2",    // MIT — editor
  "@codemirror/lang-yaml":    "6.1.3",
  "@codemirror/lint":         "6.9.7",
  "@codemirror/autocomplete": "6.20.3",
  "yaml-language-server":     "1.24.0",   // MIT — in a Web Worker
  "yaml":                     "2.x",      // eemeli/yaml — AST w/ source positions
  "zustand":                  "5.0.15",   // MIT — document state
  "immer":                    "11.1.18",  // MIT — immutable updates + snapshot undo
  "@dagrejs/dagre":           "3.1.1",    // MIT — auto-layout
  "vite":                     "8.2.2"     // MIT — build
}
```

**Rationale, each load-bearing:**

- **`@xyflow/react` 12.11.5 (MIT)** — the decisive criterion is custom node bodies containing forms, which requires real DOM. Cytoscape and LiteGraph draw to `<canvas>` and are **disqualified**; `litegraph.js` proper is abandoned (last publish 2024-01-08). Rete 2.0.6 is viable but needs ~6 assembled packages and a data model you must map onto Crossplane concepts. **The license question is settled: `node_modules/@xyflow/react/LICENSE` reads MIT, `package.json` declares MIT** [V]; attribution removal via `proOptions.hideAttribution` is a plain runtime code path, not a license gate. Corroborated by prior art: crossview ships `@xyflow/react` ^12.9.3. Cost: **178,470 raw / 56,233 gzip JS + 15,413 / 2,555 CSS.** ⚠️ It pins `zustand@^4.4.0` internally, so with zustand 5 for app state you ship two copies (a few KB; you cannot share the store instance).
- **`@rjsf/core` 6.8.0, not JSONForms** — against the same real EC2 `LaunchTemplate` `forProvider` schema, **rjsf rendered 88 inputs; JSONForms rendered 15** — and JSONForms rendered **literally nothing (75 bytes of HTML, 0 inputs)** for `additionalProperties` maps, `oneOf`, and any object nested deeper than one level, even with `Generate.uiSchema()`. [V] It does not crash; it silently renders an empty div — a worse failure mode, because the tool would appear to work on the one provider you tested and quietly drop 80% of fields on every other. **Disqualifying.**
- **CodeMirror 6, not Monaco** — same feature set, same build: **CM6 = 251 KB gzip total bundle; minimal tree-shaken Monaco = 899 KB gzip + 137 KB CSS; naive Monaco = 3.30 MB gzip across 92 files** including a 6.9 MB TypeScript worker you'll never use. **~5.8×.** [V] crossview independently chose CodeMirror.
- **`yaml-language-server` 1.24.0 in a Web Worker** for diagnostics, over `codemirror-json-schema` — the latter is ~16 months stale and drags in **shiki + markdown-it**, pushing the editor chunk from 135,648 to 291,367 gzip and emitting a **TextMate grammar for JavaScript in a YAML editor**. [V] The LSP path bundles cleanly via `yaml-language-server/lib/esm/languageservice/yamlLanguageService.js` at 325,596 gzip **off the main thread**.
- **dagre, not elk** — `@dagrejs/dagre` 3.1.1 is 106,501 bytes raw, MIT, one dependency. `elkjs` 0.12.0 is **1,609,707 raw / 466,718 gzip** and dual-licensed **EPL-2.0 OR GPL-3.0-or-later** — the one real license trap in the list. Paying 466 KB gzip and a copyleft dependency to lay out 15 nodes is not a trade worth making.
- **`zustand` + `immer` + a bounded snapshot stack; no y.js.** The argument against y.js is architectural, not size: CRDTs and undo stacks are different architectures, and retrofitting is easy while pre-fitting is expensive. **This tool authors a file that goes into Git — Git is the collaboration model.** Two rules that prevent the classic bugs: **coalesce** commits (~300–500 ms) or Ctrl+Z undoes one character; and **mirror node positions into the document on drag *end* only**, never on drag move.

**Bundle budget, measured:** [V] full eager bundle **425,988 gzip / 359,534 brotli**. With three `React.lazy` calls — canvas eager, inspector and editor lazy:

| Chunk | raw | gzip |
|---|---|---|
| **eager (React + React Flow + zustand + immer)** | 380,708 | **120,092** |
| `Inspector` (rjsf + ajv8) lazy | 390,938 | 125,795 |
| `Editor` (CM6 + lang-yaml + lint) lazy | 423,787 | 135,648 |

**First paint at 120 KB gzip.** Take this split.

**Three custom rjsf widgets are non-negotiable** — registered via rjsf's `widgets`/`fields`/`templates` props with a `uiSchema` you *generate* by walking the CRD:
1. a **reference picker** for `*Ref`/`*Selector` fields offering the other canvas nodes instead of free text — this is what turns the graph into more than decoration;
2. a **tags/map editor** for `additionalProperties`;
3. a **Go-template escape hatch** as a **first-class per-field mode toggle** — non-negotiable for a go-templating generator, and retrofitting it after the form layer is built means touching every widget.

### 5.2 Backend and CLI

- **Go 1.25+** (1.27.0 verified for the embed harness and the xpkg extractor), `net/http` + `embed.FS` — stdlib is enough; `http.ServeMux` in Go 1.22+ has method/pattern routing. Take crossview's *wiring*, not its dependency list (gin + cobra + viper + uber/fx + gorm with Postgres **and** SQLite drivers, deployed alongside `postgres:16-alpine` for a read-only dashboard). **Your tool should need no database: state lives in blueprint files in Git.**
- **`github.com/alecthomas/kong` v1.16.1 + `github.com/willabides/kongplete` v0.4.0**, not cobra. Rationale in priority order: (1) **ecosystem match** — `github.com/crossplane/cli/v2` requires exactly these versions (cobra appears only as an indirect dep), so contributors read `cmd/` without a context switch; (2) **the struct grammar is your config schema** — `enum:"argocd,flat,project"`, `env:`, `required:`, `embed:"" prefix:""` give validation, env fallback and `--help` from one declaration; (3) **`default:"1"` gives the two-front-doors ergonomics for free** (bare invocation → `serve`); (4) **`BindTo` shares one core between HTTP handlers and CLI** without globals. Honest counter-argument: cobra's `GenMarkdownTree`/completion ecosystem is richer and more contributors have seen it — kong's answers are `kong.Model` reflection (which crossplane itself uses) and kongplete. [V]
- **`github.com/google/go-containerregistry` v0.22.0** for xpkg extraction (§3.6).
- **`k8s.io/client-go`** for live CRD/MRD discovery, informer-backed.
- **gzip/brotli middleware** — mandatory, see §2.3.

**The load-bearing architectural rule:** the HTTP API must be a **thin adapter over `internal/emit`, never a parallel implementation**. `POST /api/generate` unmarshals a blueprint and calls the exact function `generate.go` calls. **If a code path exists only in the UI, the CLI cannot reproduce a UI-authored artifact, and the whole GitOps story collapses.**

Mirror the Crossplane CLI's `internal/` vs `pkg/` discipline — it exposes only `pkg/validate` and `pkg/xr`. Expose a narrow `pkg/blueprint` and nothing else.

### 5.3 The intermediate representation

A Kubernetes-shaped `Blueprint` YAML, `apiVersion: factory.crossplane.io/v1alpha1`. Reverse-DNS `apiVersion` buys migration for free (v1alpha2 + converter is a solved pattern); the audience already reads `apiVersion/kind/metadata/spec` and gets editor support via `yaml.schemas` + `# yaml-language-server: $schema=…`.

Three mandatory properties:

- **Digest-pinned providers.** `crossplane dependency add` resolves the floating tag `:v2` → `v2.7.1` and records `sha256:dcce6930dfebf29dda07946babebca57fa6df4f6034e8a52501dca5eb85b97c1` in `schemas/.lock.json`. [V] **Reproducible generation requires pinning the schema source**, or the same blueprint emits different Compositions next month. Adopt the lockfile discipline (without adopting the Project directory contract).
- **`plural` is explicit.** Never infer.
- **`resources[].rawTemplate: |` escape hatch**, emitted verbatim. Without it the tool becomes a ceiling the first time someone needs logic like `dig "resource" "status" "availableReplicas" 0 $deployed`. **Plan for the DSL covering ~80% and `rawTemplate` the rest.**

### 5.4 On-disk layout and distribution

**`--layout argocd` (default):**

```
crossplane/xrds/<kind-lower>.yaml
crossplane/compositions/<variant>/<kind-lower>.yaml
```

Make the prefixes flags (`--xrd-dir`, `--composition-dir`) — the convention generalizes, the literal paths don't. **One Kubernetes object per file, not multi-doc** (with `prune: true` + `recurse: true`, deleting a file is how you delete a live object). **Blueprints live outside the synced paths** (e.g. `blueprints/` at repo root) or `recurse: true` will try to apply `kind: Blueprint` to the cluster.

Naming, cross-checked between git and cluster: [V] XRD `metadata.name` = `<plural>.<group>` (a hard invariant); Composition `metadata.name` = `<plural>.<variant>.<group>` (a **convention** — the `<variant>` segment is a human choice, not derivable from the MR group); filenames = `<singular-kind-lowercase>.yaml`.

**CI wiring:** `crossplane-factory generate blueprints/ --out-dir . --check` with **distinct exit codes** (`terraform plan -detailed-exitcode` precedent): `0` = in sync, `1` = tool error, `2` = drift — so CI distinguishes "your generator crashed" from "someone hand-edited generated YAML".

**Distribution, Tier 1 at v0.1.0:** GoReleaser **v2.18.0** (2026-08-24) with two build IDs (default + `-tags no_ui`); **Homebrew via `homebrew_casks:`, not `brews:`** (casks landed in v2.10; `brews` is deprecated and removed in v3 — their docs call the old section *"kind of a hack"*; start on casks so you never migrate); an **OCI image to ghcr.io** (non-negotiable — the UI wants to run in-cluster); an **`install.sh`** at repo root (the Crossplane CLI ships exactly this); and `go install …@latest` (free, provided `web/dist` is committed).

**Defer Krew.** The mechanics work — a binary named `kubectl-crossplane_factory` (**underscore**) is invoked as `kubectl crossplane-factory` [V] — but (a) Crossplane retired the `kubectl-crossplane` plugin in favour of a standalone binary, so re-entering that namespace re-litigates a settled decision; (b) a plugin that starts a web server is an odd fit for the kubectl contract; (c) `generate` needs **no cluster at all**, and the kubectl entry point implies a dependency that doesn't exist. **Skip** apt/rpm/AUR/Snap/Flatpak.

### 5.5 Top 5 named risks

**Risk 1 — "The Upbound Convention Trap": the reference-wiring layer is the actual product, and it rests on a naming convention rather than a spec.**
Everything else in the stack is commodity. The hard part is semantic — and **344 of the 353 CRDs analysed are upjet-generated**, so the `*Ref`/`*Selector`/`*IdRef` convention and the `"Reference to a X in y to populate z."` grammar are an upjet/crossplane-runtime property, not a Crossplane guarantee. Worse, **what the reference resolves to is not in the schema at all** (upjet's `Extractor` lives in provider Go config), so you can draw a correct edge and still not know what value flows across it. Both `canvas-ux.md` and `crd-schema-shape.md` independently name this the central risk.
**Mitigation:** build reference inference as an explicit, testable, **data-driven layer in Go** (not scattered through React), seeded from the convention, with **per-provider override files** and a hand-override on every edge. **Prototype against a second, non-upjet provider (`provider-kubernetes` is installed and already known to differ) before the convention hardens into the core.** Ship the description-parser regexes as data, not code.

**Risk 2 — "The `<no value>` Silent Corruption": the generator's worst output passes every automated gate.**
A missing XR field renders the literal string `<no value>` into a real managed resource, at any depth, without erroring — and because it is a legal string, **the full validate → render → validate pipeline exits 0**. [V] This is the one defect class that ships to production.
**Mitigation:** defence in depth, all five layers, from day one: (a) always emit `options: ["missingkey=error"]` **top-level**; (b) wrap every optional field in `{{- with }}`; (c) mark every field the template dereferences as `required` in the generated XRD so the XR gate catches it upstream; (d) ship the `grep -rn '<no value>\|<nil>'` guard in every generated Makefile; (e) run the guard in the generator's own golden tests. **Also close the known gap:** it was verified that `missingkey=error` fires on a bare deref, but **not** separately confirmed that `dig`/`default` remain safe *under* it [U] — a 30-second test before hard-coding the combination.

**Risk 3 — "The Round-Trip Expectation": users will hand-edit generated YAML and expect the graph to follow, and a generate-only tool that surprises them gets abandoned.**
The research is unambiguous that AST → graph is infeasible (§2.5), but the *user expectation* is real and does not care.
**Mitigation:** declare round-tripping a **non-goal in v1 and say so loudly** — in the README, in `--help`, and in a generated header comment on every emitted file. Ship **Tier 1 `adopt`** (structured fields mapped, template captured as opaque `rawTemplate`) on day one so the tool onboards existing Compositions losslessly; ship **Tier 2.5 render-based visualization** as read-only "show me what this makes". Use `eemeli/yaml`'s position-aware AST so comments survive and surgical edits stay possible later. **Add a provenance marker to emitted templates** so the tool can cheaply detect its own output — far cheaper than parsing.

**Risk 4 — "The GitOps Churn Cascade": nondeterministic output on a `selfHeal: true` + `prune: true` repo becomes live-cluster churn, and one wrong file becomes a deletion.**
Three verified mechanisms compound: a generated-at annotation creates a perpetual sync loop; trailing whitespace changes YAML block-scalar round-tripping and produces phantom diffs; and **a `kustomization.yaml` flips ArgoCD from Directory to Kustomize, after which any file absent from `resources:` is deleted under `prune: true`**.
**Mitigation:** treat determinism as a **correctness requirement** — sorted keys, stable field order, LF only, trailing newline, no version stamps, trailing whitespace stripped from every template line. **Provenance in YAML comments, never annotations.** Never emit `tracking-id`, `last-applied-configuration`, `sync-wave`, or a default `kustomization.yaml`. Enforce all of it with byte-exact goldens (Layer 1) — byte-exactness matters more here than in most generators. **And resolve the open `[U]`: test the Kustomize-detection-under-`recurse` interaction in a scratch repo before shipping `--layout kustomize`.**

**Risk 5 — "The Docker Cliff": the highest-value verification loop needs a Docker daemon with network-create privileges, which many CI runners cannot provide.**
`crossplane composition render` runs **the engine itself** as a container plus one per function, and creates a Docker network per render. `--crossplane-binary` wants the core **server** binary (linux-only in the shipped image; darwin viability **[U]**). Renders also **leak containers**. And a fully-cold first run plausibly exceeds the `--timeout=1m` default.
**Mitigation:** **split CI into three lanes from day one** — Lane A (`lint` via `resource validate`, no Docker, no cluster, ~0.5 s, runs anywhere and catches malformed XRD/Composition/function-input); Lane B (full `test` with Docker); Lane C (optional pre-merge `kubectl apply --dry-run=server`, which also covers the structural-schema gap `resource validate` may not). Generate `--timeout=5m` into shipped Makefiles, set `render.crossplane.io/runtime-docker-name` to reuse named containers, reap in `t.Cleanup`, gate Layer-4 render goldens behind `testing.Short()` + a docker probe, and cache `~/.crossplane/cache` (1.2 MB) plus the images (~265 MB).

---

## 6. Open questions for the human

These could not be settled by research and are product decisions or require access/testing the research could not perform.

**Q1 — Schema sourcing strategy beyond a single pinned tag. (Blocked by the missing brief.)**
`schema-sourcing.md` does not exist, and the recovered artifact (§3.6) answers *extraction* but not *discovery*. The only datapoint is negative: `xpkg.upbound.io/v2/<repo>/tags/list` returns an **empty tag list** [V], so version discovery needs the Upbound marketplace API or a user-supplied tag. **Decide:** does the GUI let a user browse *installable* providers (requiring a catalogue integration and a marketplace API dependency), or only work against providers already installed / explicitly named? Also unanswered: private-registry auth beyond the Docker keychain, air-gap/mirror strategy, and cache invalidation policy. **This is the largest research gap and should be re-run before implementation.**

**Q2 — Do you support the legacy cluster-scoped variant at all?**
Everything argues for `.m.` namespaced only (halves the catalogue on day one, matches the target XRD). But `LegacyCluster` XRDs still exist in the field, and a v1 emitter mode is a real feature with a real cost (a second envelope renderer, a second CRD variant, `claimNames`/`connectionSecretKeys` support). **Scope decision, not a research question.** Note the read-side wrinkle: a `LegacyCluster` XRD created via v1 may return `scope: LegacyCluster` on a **v2 read** (CEL runs on write, not read) — untested [U] — so the tool should probably **tolerate `LegacyCluster` on read while refusing to emit it under v2** regardless of the scope decision.

**Q3 — How much of the template does the DSL own, and how much is `rawTemplate`?**
The research establishes that a field-mapping DSL will cover roughly 80% and that `rawTemplate` is mandatory for the rest — but the boundary is a product judgement. Every field the DSL absorbs is one the canvas can render and validate; every field pushed to `rawTemplate` is a node that degrades to a text editor. **Where the line sits determines what the GUI actually feels like.**

**Q4 — Reference-target resolution when the target provider isn't installed.**
`provider-aws-ec2` refs reach into 6 other groups; a dragged EC2 `Instance` will suggest a `kms.Key` whose CRD may not be present. The research recommends marking these "requires provider-aws-kms" rather than dropping the edge — but **does the tool then offer to add the provider to the blueprint, fetch its schema on demand, or merely warn?** Each is a different scope.

**Q5 — Does the tool require a cluster, and if so for what?**
`generate` is verified to need **no cluster and no Docker**. But the cluster is the authoritative schema source, the only way to populate the palette from **Active MRDs**, and the only way to resolve `functionRef.name` against installed `Function` objects rather than guessing. **Decide the degraded-mode contract:** what works fully offline, what works better with a cluster, and what is cluster-only. This also determines whether `client-go` is a hard dependency of the core or an optional adapter.

**Q6 — Is the in-cluster web UI a v1 deliverable?**
The single-binary + `//go:embed` + `no_ui` build tag pattern is verified and cheap, and crossview proves in-cluster demand. But it implies an OCI image, a Helm chart, an auth story, and RBAC — none of which the research covered. **Ship CLI-first and add `serve` later, or both at v0.1.0?**

**Q7 — Which second provider anchors the portability test, and when?**
Risk 1 says prototype against a non-upjet provider **before** the convention hardens. `provider-kubernetes` is installed and already known to differ (no `forProvider` on `ObservedObjectCollection`, a real enum, a real CEL business rule, `x-kubernetes-preserve-unknown-fields` on `Object.spec.forProvider.manifest` requiring a raw-YAML editor). **Is it the right anchor, or does a second *cloud* provider (Azure/GCP, already sampled at 62 CRDs) matter more for the go-to-market story?** These pull in opposite directions: provider-kubernetes stresses the *schema shape*, a second cloud stresses the *reference grammar*.

**Q8 — Bump `function-go-templating` to v0.12.4?**
The cluster runs v0.12.0; latest is v0.12.4 (2026-08-25) and the intervening releases are **exclusively** Go-runtime and dependency CVE remediation with no functional, schema, or template-function changes [D]. Recommending the bump is free and a generator targeting v0.12.0 semantics is safe against it. **But does the generator pin a version, emit a floating range, or resolve against installed `Function` objects?** (Note the related verified trap: `crossplane composition generate` pulls from a *different registry and version* than what is installed.)

**Q9 — Two open verification gaps worth one test each before they become load-bearing.**
(a) Does an **Inactive** MRD carry the full `openAPIV3Schema`? Inferred, not tested — all 8 MRDs on the test cluster were Active. If it does not, the offline palette story changes for large providers. (b) Does `crossplane resource validate` enforce full **CRD structural-schema legality** on an XRD, or only field-level conformance to the CompositeResourceDefinition CRD? Only the latter was demonstrated — if it's the latter, Lane C (`kubectl --dry-run=server`) is not optional.
